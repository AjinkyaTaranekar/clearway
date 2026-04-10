#!/usr/bin/env bash
# =============================================================================
# chaos-monkey.sh — GCP Swarm Chaos Engineering for VCS
# =============================================================================
#
# Run from GCP Cloud Shell against the live deployed stack.
# Tests fault-tolerance and circuit-breaker behaviour by injecting
# controlled failures into individual Swarm nodes or service containers.
#
# Experiment types:
#
#   drain-node
#     Marks a Swarm node "drain" so Docker stops scheduling tasks on it.
#     Since all VCS services are global-mode (1 task per node), draining a
#     node removes all containers from that VM. The other 3 nodes still serve.
#     Restoration: node availability set back to "active".
#
#   pause-container
#     SSH into the target VM and `docker pause` one service container.
#     The process is frozen — ports stay bound but no requests are processed.
#     This is the most surgical way to trip journey-service's circuit breakers
#     (CB opens after 5 consecutive failures to capacity-service or map-service).
#     Restoration: `docker unpause` the container.
#
#   block-firewall
#     Adds a temporary GCP network tag to the target VM, then creates a
#     high-priority INGRESS DENY firewall rule targeting that tag.
#     Blocks all inbound HTTP/HTTPS and VCS service ports from the internet.
#     Internal Docker overlay network (VPC) is unaffected.
#     Restoration: firewall rule deleted, temp tag removed.
#
# Safety guarantees:
#   - Pre-flight: verifies all cells are healthy before starting.
#   - EXIT trap: always runs cleanup regardless of how the script exits.
#   - CRDB quorum guard: blocks drain-node if it would leave fewer than 3
#     CockroachDB nodes running (Raft majority for a 4-node cluster).
#   - Manager guard: warns before draining a Swarm manager node.
#   - Single-node limit: only one VM under chaos at any time.
#   - Dry-run mode: prints every action without executing anything.
#   - Auto-restore: cleanup runs automatically after --duration seconds.
#
# Usage:
#   ./scripts/chaos/chaos-monkey.sh [OPTIONS] <experiment> <target-vm>
#
# Options:
#   -c, --cell     eu|us|apac       Cell to observe health from (default: eu)
#   -d, --duration SECONDS          Chaos window length (default: 60)
#   -p, --poll     SECONDS          Health poll interval (default: 10)
#   -r, --recover  SECONDS          Max recovery wait after restore (default: 180)
#   -s, --service  SERVICE_NAME     Service to pause (pause-container only,
#                                   default: capacity-service)
#   -n, --dry-run                   Print actions, execute nothing
#   -v, --verbose                   Extra debug output
#   -h, --help                      Show this help
#
# Arguments:
#   experiment    drain-node | pause-container | block-firewall
#   target-vm     GCP VM name (e.g. vcs-vm-eu2)
#
# Examples:
#   # Drain the EU worker node for 60s, watch services redistribute
#   ./scripts/chaos/chaos-monkey.sh drain-node vcs-vm-eu2
#
#   # Pause capacity-service on EU worker to trip journey-service's circuit breaker
#   ./scripts/chaos/chaos-monkey.sh --service capacity-service pause-container vcs-vm-eu2
#
#   # Block HTTP to the APAC node for 90s via GCP firewall
#   ./scripts/chaos/chaos-monkey.sh --cell apac --duration 90 block-firewall vcs-vm-ap1
#
#   # Preview the drain experiment without executing anything
#   ./scripts/chaos/chaos-monkey.sh --dry-run drain-node vcs-vm-eu2
#
# =============================================================================

set -euo pipefail

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
CYAN='\033[0;36m'; MAGENTA='\033[0;35m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'

# ── Cell topology (mirrors deploy-cell.sh exactly) ────────────────────────────
GCP_PROJECT="distributed-capacity-system"
STACK_NAME="vcs"

declare -A CELL_MANAGER=([eu]="vcs-vm-eu1"      [us]="vcs-vm-us1"   [apac]="vcs-vm-ap1")
declare -A CELL_ZONE=(   [eu]="europe-west1-b"   [us]="us-east1-d"   [apac]="asia-east1-b")
declare -A CELL_URL=(
  [eu]="https://35.244.162.92.nip.io"
  [us]="https://35.227.198.68.nip.io"
  [apac]="https://34.8.134.246.nip.io"
)

declare -A VM_ZONE=(
  [vcs-vm-eu1]="europe-west1-b"
  [vcs-vm-eu2]="europe-west1-b"
  [vcs-vm-us1]="us-east1-d"
  [vcs-vm-ap1]="asia-east1-b"
)

# VCS Docker service names as they appear in "docker service ls"
declare -A VCS_SERVICES=(
  [iam-service]="${STACK_NAME}_iam-service"
  [capacity-service]="${STACK_NAME}_capacity-service"
  [journey-service]="${STACK_NAME}_journey-service"
  [map-service]="${STACK_NAME}_map-service"
  [notification-service]="${STACK_NAME}_notification-service"
  [db]="${STACK_NAME}_db"
)

# ── Defaults ──────────────────────────────────────────────────────────────────
CELL="eu"
DURATION=60
POLL_INTERVAL=10
RECOVER_TIMEOUT=180
TARGET_SERVICE="capacity-service"
DRY_RUN=false
VERBOSE=false
EXPERIMENT=""
TARGET_VM=""

# ── Cleanup stack (LIFO) ──────────────────────────────────────────────────────
# Each entry is a literal shell command string, eval'd in reverse order on EXIT.
# We store raw gcloud/docker commands (not bash function calls) so they are
# self-contained and not sensitive to function availability in the trap context.
CLEANUP_CMDS=()
CHAOS_TAG=""        # temp GCP network tag added for block-firewall experiment
FW_RULE_NAME=""     # firewall rule name for block-firewall experiment
EXPERIMENT_START=""

# ── Logging helpers ───────────────────────────────────────────────────────────
ts()      { date '+%H:%M:%S'; }
log()     { echo -e "${BOLD}[$(ts)]${NC} $*"; }
info()    { echo -e "${GREEN}[$(ts)] INFO   ${NC} $*"; }
warn()    { echo -e "${YELLOW}[$(ts)] WARN   ${NC} $*"; }
error()   { echo -e "${RED}[$(ts)] ERROR  ${NC} $*" >&2; }
chaos()   { echo -e "${RED}[$(ts)] CHAOS  ${NC} $*"; }
restlog() { echo -e "${CYAN}[$(ts)] RESTORE${NC} $*"; }
debug()   { $VERBOSE && echo -e "${DIM}[$(ts)] DEBUG   $*${NC}" || true; }
banner()  { echo -e "${MAGENTA}[$(ts)] ──${NC} $*"; }

# ── Execute or dry-run ────────────────────────────────────────────────────────
# Pass the full command as a single string (will be eval'd).
run() {
  local cmd="$1"
  if $DRY_RUN; then
    echo -e "  ${DIM}[DRY-RUN] ${cmd}${NC}"
  else
    debug "exec: ${cmd}"
    eval "${cmd}"
  fi
}

# ── Cleanup registry ──────────────────────────────────────────────────────────
push_cleanup() { CLEANUP_CMDS+=("$1"); }

# ── EXIT trap — always runs, always restores ──────────────────────────────────
on_exit() {
  local code=$?

  # Prevent recursive calls (bash does not re-fire EXIT from within EXIT trap)
  echo ""
  restlog "$(printf '%.0s━' {1..56})"
  restlog "RESTORE PHASE — returning system to original state"
  restlog "$(printf '%.0s━' {1..56})"

  if [[ ${#CLEANUP_CMDS[@]} -eq 0 ]]; then
    restlog "No cleanup actions registered."
  else
    local i
    for (( i=${#CLEANUP_CMDS[@]}-1; i>=0; i-- )); do
      restlog "Running: ${CLEANUP_CMDS[$i]}"
      run "${CLEANUP_CMDS[$i]}" || warn "Restore step failed (continuing): ${CLEANUP_CMDS[$i]}"
      sleep 2
    done
  fi

  echo ""
  restlog "Waiting up to ${RECOVER_TIMEOUT}s for all cells to come back healthy..."
  wait_for_recovery || true

  print_final_status
  # Return with the original exit code — bash EXIT trap respects this.
  return "$code"
}
trap on_exit EXIT

# =============================================================================
# ARGUMENT PARSING
# =============================================================================
usage() {
  # Print the header comment block
  sed -n '3,/^# ====/p' "$0" | head -70 | sed 's/^# \?//'
  exit 0
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -c|--cell)      CELL="$2";              shift 2 ;;
      -d|--duration)  DURATION="$2";          shift 2 ;;
      -p|--poll)      POLL_INTERVAL="$2";     shift 2 ;;
      -r|--recover)   RECOVER_TIMEOUT="$2";   shift 2 ;;
      -s|--service)   TARGET_SERVICE="$2";    shift 2 ;;
      -n|--dry-run)   DRY_RUN=true;           shift   ;;
      -v|--verbose)   VERBOSE=true;           shift   ;;
      -h|--help)      usage ;;
      -*)             error "Unknown flag: $1"; usage ;;
      *)
        if   [[ -z "$EXPERIMENT" ]]; then EXPERIMENT="$1"
        elif [[ -z "$TARGET_VM"  ]]; then TARGET_VM="$1"
        else error "Unexpected argument: $1"; exit 1
        fi
        shift ;;
    esac
  done

  [[ -n "$EXPERIMENT" ]] || { error "Missing <experiment>"; usage; }
  [[ -n "$TARGET_VM"  ]] || { error "Missing <target-vm>";  usage; }
}

# =============================================================================
# GCP / SWARM HELPERS
# =============================================================================

# Run a command on a remote GCP VM.
# Usage: gcp_ssh <vm> <zone> <command>
gcp_ssh() {
  local vm="$1" zone="$2"; shift 2
  gcloud compute ssh "$vm" \
    --project="$GCP_PROJECT" \
    --zone="$zone" \
    --quiet \
    --command="$*" \
    2>/dev/null
}

# Same but never fails — returns empty string on error.
gcp_ssh_safe() {
  gcp_ssh "$@" 2>/dev/null || true
}

# Run a docker command on the Swarm manager of the current cell.
swarm() {
  local mgr="${CELL_MANAGER[$CELL]}" zone="${CELL_ZONE[$CELL]}"
  gcp_ssh "$mgr" "$zone" "sudo docker $*"
}

swarm_safe() {
  swarm "$@" 2>/dev/null || true
}

# Get the Swarm node ID for a VM hostname (queried from manager).
get_node_id() {
  local vm="$1"
  swarm_safe "node ls --format '{{.ID}} {{.Hostname}}'" \
    | awk -v h="$vm" '$2 == h { print $1; exit }'
}

# Get a node's current availability (active/drain/pause).
get_node_availability() {
  local node_id="$1"
  swarm_safe "node inspect $node_id --format '{{.Spec.Availability}}'" || echo "unknown"
}

# Count how many CRDB (db) tasks are in running state across the swarm.
count_live_crdb_nodes() {
  swarm_safe "service ps ${STACK_NAME}_db \
    --filter desired-state=running \
    --format '{{.CurrentState}}'" \
    | grep -c "Running" 2>/dev/null || echo "0"
}

# Get the short container ID for a VCS service running on a specific VM.
get_container_id() {
  local vm="$1" zone="$2" svc="$3"
  # Swarm task container name pattern: vcs_<service>.<node-id>.<task-id>
  gcp_ssh_safe "$vm" "$zone" \
    "sudo docker ps --filter 'name=${STACK_NAME}_${svc}' --format '{{.ID}}' | head -1"
}

# =============================================================================
# HEALTH CHECKS
# =============================================================================

# Returns "healthy" or "unreachable" for a given cell.
cell_health() {
  local url="${CELL_URL[$1]}"
  curl -sf --max-time 5 "${url}/nginx-health" &>/dev/null && echo "healthy" || echo "unreachable"
}

# Returns true (0) if ALL three cells are healthy.
all_cells_healthy() {
  local ok=true
  for c in eu us apac; do
    [[ "$(cell_health "$c")" != "healthy" ]] && ok=false
  done
  $ok
}

# Check whether the target service endpoint is reachable via the cell's gateway.
# For pause-container experiments, this goes DOWN when the service is paused.
# (Note: this is gateway-level reachability, not the journey-service CB state.
#  To observe the CB, run K6 load tests concurrently.)
check_target_endpoint() {
  local cell_url="${CELL_URL[$CELL]}"
  local endpoint

  case "$TARGET_SERVICE" in
    capacity-service)    endpoint="/api/v1/capacity/segments" ;;
    map-service)         endpoint="/api/v1/map/segments" ;;
    iam-service)         endpoint="/.well-known/jwks.json" ;;
    notification-service|journey-service) endpoint="/nginx-health" ;;
    *) endpoint="/nginx-health" ;;
  esac

  local http_code
  http_code=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 \
    "${cell_url}${endpoint}" 2>/dev/null || echo "000")

  if [[ "$http_code" == "200" || "$http_code" == "401" ]]; then
    # 401 means service is up but needs auth — still "reachable"
    echo "UP ($http_code)"
  elif [[ "$http_code" == "000" ]]; then
    echo "UNREACHABLE"
  else
    echo "ERROR ($http_code)"
  fi
}

# =============================================================================
# PRE-FLIGHT CHECKS
# =============================================================================

preflight() {
  log "Pre-flight checks..."

  # 1. gcloud auth
  if ! gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q '@'; then
    error "Not authenticated to GCP. Run: gcloud auth login"
    exit 1
  fi

  # 2. Project
  local current_proj
  current_proj=$(gcloud config get-value project 2>/dev/null || echo "")
  if [[ "$current_proj" != "$GCP_PROJECT" ]]; then
    warn "Active project is '$current_proj'. Switching to '$GCP_PROJECT'..."
    run "gcloud config set project $GCP_PROJECT"
  fi
  info "GCP project: $GCP_PROJECT"

  # 3. Validate experiment
  case "$EXPERIMENT" in
    drain-node|pause-container|block-firewall) ;;
    *)
      error "Unknown experiment '$EXPERIMENT'. Valid: drain-node, pause-container, block-firewall"
      exit 1 ;;
  esac

  # 4. Validate cell
  if [[ -z "${CELL_MANAGER[$CELL]+_}" ]]; then
    error "Unknown cell '$CELL'. Valid: eu, us, apac"
    exit 1
  fi

  # 5. Validate target VM
  if [[ -z "${VM_ZONE[$TARGET_VM]+_}" ]]; then
    error "Unknown VM '$TARGET_VM'. Known VMs: ${!VM_ZONE[*]}"
    exit 1
  fi

  # 6. Validate service name (pause-container only)
  if [[ "$EXPERIMENT" == "pause-container" ]]; then
    if [[ -z "${VCS_SERVICES[$TARGET_SERVICE]+_}" ]]; then
      error "Unknown service '$TARGET_SERVICE'. Known: ${!VCS_SERVICES[*]}"
      exit 1
    fi
  fi

  # 7. Manager-drain warning
  if [[ "$EXPERIMENT" == "drain-node" && "$TARGET_VM" == "${CELL_MANAGER[$CELL]}" ]]; then
    warn "WARNING: '$TARGET_VM' is the Swarm manager of cell '$CELL'."
    warn "Draining a manager removes its containers but keeps its manager role."
    warn "Safe as long as the Swarm has at least one reachable manager (it does)."
    warn "Sleeping 5s — Ctrl-C now to abort."
    $DRY_RUN || sleep 5
  fi

  # 8. CRDB quorum guard (drain-node only)
  if [[ "$EXPERIMENT" == "drain-node" ]]; then
    crdb_quorum_guard
  fi

  # 9. Health gate
  info "Checking all cells are healthy..."
  local retries=3 attempt=0
  while [[ $attempt -lt $retries ]]; do
    if all_cells_healthy; then
      info "All cells healthy — ready to proceed."
      return 0
    fi
    attempt=$((attempt+1))
    if [[ $attempt -lt $retries ]]; then
      warn "Some cells unhealthy (attempt $attempt/$retries). Retrying in 10s..."
      sleep 10
    fi
  done

  error "Pre-flight failed: not all cells healthy after $retries attempts."
  error "Fix the cluster before running chaos experiments."
  exit 1
}

# CockroachDB Raft quorum guard.
# 4 nodes → majority = 3. We must never drop below 3 running CRDB nodes.
crdb_quorum_guard() {
  info "CockroachDB quorum guard..."
  local live
  live=$(count_live_crdb_nodes)
  info "  Live CRDB nodes (running tasks): $live"

  if [[ "$live" -le 2 ]]; then
    error "QUORUM GUARD BLOCKED: Only $live CRDB nodes running."
    error "Draining another node would break Raft consensus (need majority of 4 = 3)."
    error "Repair the cluster before running drain-node experiments."
    exit 1
  elif [[ "$live" -eq 3 ]]; then
    warn "  Only 3 CRDB nodes running — draining will leave exactly 3 (minimum safe)."
    warn "  If the target VM is already the failed node, this is fine."
    warn "  Continuing with extra caution. Monitor CRDB closely."
    $DRY_RUN || sleep 3
  else
    info "  Quorum safe: $live live nodes, will have $((live-1)) during chaos."
  fi
}

# =============================================================================
# MONITORING LOOP
# =============================================================================

monitor_loop() {
  local duration="$1"
  local elapsed=0

  info "Monitoring system for ${duration}s (poll every ${POLL_INTERVAL}s)..."
  info "Tip: run K6 load tests concurrently to generate traffic and observe circuit breakers."
  echo ""
  echo -e "  ${BOLD}TIME    EU-Cell       US-Cell       APAC-Cell     ${TARGET_SERVICE} endpoint${NC}"
  echo "  $(printf '%.0s─' {1..70})"

  while [[ $elapsed -lt $duration ]]; do
    local eu_h us_h ap_h ep_state
    eu_h=$(cell_health eu)
    us_h=$(cell_health us)
    ap_h=$(cell_health apac)
    ep_state=$(check_target_endpoint)

    # Colour coding (no printf alignment — ANSI codes break it)
    color_status() {
      case "$1" in
        healthy)     printf "${GREEN}%-12s${NC}" "$1" ;;
        unreachable) printf "${RED}%-12s${NC}" "$1" ;;
        *)           printf "${YELLOW}%-12s${NC}" "$1" ;;
      esac
    }
    color_ep() {
      case "$1" in
        UP*)          printf "${GREEN}%-22s${NC}" "$1" ;;
        UNREACHABLE*) printf "${RED}%-22s${NC}" "$1" ;;
        *)            printf "${YELLOW}%-22s${NC}" "$1" ;;
      esac
    }

    printf "  %-8s" "${elapsed}s"
    color_status "$eu_h"
    color_status "$us_h"
    color_status "$ap_h"
    color_ep "$ep_state"
    echo ""

    # Every 30s print Swarm task distribution
    if (( elapsed > 0 && elapsed % 30 == 0 )); then
      echo ""
      banner "Swarm task distribution (cell: $CELL):"
      swarm_safe "service ps ${STACK_NAME}_journey-service \
        --filter desired-state=running \
        --format 'table {{.Name}}\t{{.Node}}\t{{.CurrentState}}'" \
        | sed 's/^/    /' || true
      echo ""
    fi

    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
  done

  echo ""
  info "Chaos window complete."
}

# =============================================================================
# RECOVERY WAIT
# =============================================================================

wait_for_recovery() {
  if $DRY_RUN; then
    restlog "[DRY-RUN] Skipping recovery wait."
    return 0
  fi

  local elapsed=0
  while [[ $elapsed -lt $RECOVER_TIMEOUT ]]; do
    if all_cells_healthy; then
      restlog "All cells healthy after ${elapsed}s."
      return 0
    fi
    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
    restlog "  Waiting... ${elapsed}s / ${RECOVER_TIMEOUT}s"
  done

  warn "System did not fully recover within ${RECOVER_TIMEOUT}s."
  for c in eu us apac; do
    local s; s=$(cell_health "$c")
    [[ "$s" != "healthy" ]] && warn "  ${c}: $s  →  ${CELL_URL[$c]}"
  done
  return 1
}

# =============================================================================
# FINAL STATUS
# =============================================================================

print_final_status() {
  echo ""
  log "$(printf '%.0s━' {1..56})"
  log "FINAL SWARM STATUS (cell: $CELL)"
  log "$(printf '%.0s━' {1..56})"

  if $DRY_RUN; then
    log "[DRY-RUN] No real changes were made."
    return
  fi

  echo ""
  banner "Nodes:"
  swarm_safe "node ls --format 'table {{.Hostname}}\t{{.Status}}\t{{.Availability}}\t{{.ManagerStatus}}'" \
    | sed 's/^/  /' || true

  echo ""
  banner "Services:"
  swarm_safe "service ls --format 'table {{.Name}}\t{{.Replicas}}'" \
    | sed 's/^/  /' || true
  echo ""
}

# =============================================================================
# EXPERIMENT: drain-node
# =============================================================================
# Drains a Swarm node so Docker stops scheduling any tasks on it.
# With global-mode services, this means ALL VCS containers on that VM stop.
# Other nodes continue serving. Restoration: set availability back to "active".
# =============================================================================
experiment_drain_node() {
  local mgr="${CELL_MANAGER[$CELL]}" zone="${CELL_ZONE[$CELL]}"

  info "Resolving Swarm node ID for VM: $TARGET_VM"
  local node_id
  node_id=$(get_node_id "$TARGET_VM")

  if [[ -z "$node_id" ]]; then
    error "VM '$TARGET_VM' is not found in the Swarm for cell '$CELL'."
    error "Verify with: gcloud compute ssh $mgr --zone=$zone -- sudo docker node ls"
    exit 1
  fi

  local original_avail
  original_avail=$(get_node_availability "$node_id")
  info "Node $TARGET_VM  (swarm ID: $node_id)  availability: $original_avail"

  # Store a raw gcloud command for the cleanup (no bash function references)
  push_cleanup "gcloud compute ssh $mgr \
    --project=$GCP_PROJECT \
    --zone=$zone \
    --quiet \
    --command='sudo docker node update --availability $original_avail $node_id' \
    2>/dev/null"

  chaos "Draining node $TARGET_VM (ID: $node_id)"
  chaos "All containers on $TARGET_VM will stop. Other nodes absorb the load."

  run "gcloud compute ssh $mgr \
    --project=$GCP_PROJECT \
    --zone=$zone \
    --quiet \
    --command='sudo docker node update --availability drain $node_id' \
    2>/dev/null"

  if ! $DRY_RUN; then
    info "Waiting 15s for tasks to evacuate $TARGET_VM..."
    sleep 15

    banner "Node availability after drain:"
    swarm_safe "node ls --format 'table {{.Hostname}}\t{{.Status}}\t{{.Availability}}'" \
      | sed 's/^/  /' || true

    echo ""
    banner "Running journey-service tasks (should show only remaining nodes):"
    swarm_safe "service ps ${STACK_NAME}_journey-service \
      --filter desired-state=running \
      --format 'table {{.Name}}\t{{.Node}}\t{{.CurrentState}}'" \
      | sed 's/^/  /' || true
  fi
}

# =============================================================================
# EXPERIMENT: pause-container
# =============================================================================
# SSH into the target VM and `docker pause` one named service container.
# The container process is frozen via SIGSTOP — ports remain bound but no
# request is processed. This is the best way to trip journey-service circuit
# breakers without affecting the entire node.
#
# Circuit breaker config in journey-service:
#   - Capacity CB: trips after 5 consecutive failures, 10s timeout
#   - Map CB:      same settings
#
# Run K6 concurrently to generate the traffic that trips the breaker.
# =============================================================================
experiment_pause_container() {
  local target_zone="${VM_ZONE[$TARGET_VM]}"

  info "Locating '$TARGET_SERVICE' container on $TARGET_VM..."
  local cid
  cid=$(get_container_id "$TARGET_VM" "$target_zone" "$TARGET_SERVICE")

  if [[ -z "$cid" ]]; then
    error "No running container for '$TARGET_SERVICE' found on $TARGET_VM."
    error "Check with: gcloud compute ssh $TARGET_VM --zone=$target_zone -- sudo docker ps"
    exit 1
  fi
  info "Found container: $cid"

  # Store a raw gcloud unpause command
  push_cleanup "gcloud compute ssh $TARGET_VM \
    --project=$GCP_PROJECT \
    --zone=$target_zone \
    --quiet \
    --command='sudo docker unpause $cid' \
    2>/dev/null"

  chaos "Pausing container $cid ($TARGET_SERVICE) on $TARGET_VM"
  chaos "Container is frozen — ports bound, no requests processed."
  chaos "Journey-service CB will open after 5 consecutive failures."
  chaos "Run K6 load tests now to generate the failing requests!"

  run "gcloud compute ssh $TARGET_VM \
    --project=$GCP_PROJECT \
    --zone=$target_zone \
    --quiet \
    --command='sudo docker pause $cid' \
    2>/dev/null"

  if ! $DRY_RUN; then
    banner "Container state on $TARGET_VM:"
    gcp_ssh_safe "$TARGET_VM" "$target_zone" \
      "sudo docker inspect $cid --format 'Status={{.State.Status}} Paused={{.State.Paused}}'" \
      | sed 's/^/  /' || true
  fi
}

# =============================================================================
# EXPERIMENT: block-firewall
# =============================================================================
# Adds a temporary GCP network tag ("chaos-block-<timestamp>") to the target
# VM, then creates a high-priority INGRESS DENY firewall rule that targets
# that tag. This blocks all inbound HTTP/HTTPS and VCS service ports from
# 0.0.0.0/0 (internet) to that VM.
#
# Why temp tag instead of matching existing tags:
#   - Existing tags may overlap with other VMs (blast radius risk).
#   - A unique temporary tag gives precise, single-VM targeting.
#   - Clean: tag is removed with the firewall rule on restore.
#
# Docker overlay network (internal VPC 10.x.x.x range) is NOT affected —
# service-to-service calls on the overlay continue working.
# This simulates the GCP load balancer's health check failing for this node,
# causing LB to stop routing external traffic to it.
#
# Restoration: delete firewall rule → remove temp tag (order matters).
# =============================================================================
experiment_block_firewall() {
  local target_zone="${VM_ZONE[$TARGET_VM]}"
  local ts_suffix; ts_suffix=$(date '+%s')

  CHAOS_TAG="chaos-block-${ts_suffix}"
  FW_RULE_NAME="chaos-deny-${TARGET_VM}-${ts_suffix}"

  info "Target VM : $TARGET_VM (zone: $target_zone)"
  info "Chaos tag : $CHAOS_TAG"
  info "FW rule   : $FW_RULE_NAME"

  # --- Register cleanup in LIFO order ---
  # Step 2 (runs first on exit): remove the temp tag from the VM
  push_cleanup "gcloud compute instances remove-tags $TARGET_VM \
    --tags=$CHAOS_TAG \
    --zone=$target_zone \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null || true"

  # Step 1 (runs second on exit, i.e. first to be popped from LIFO):
  # delete the firewall rule (must happen before tag removal to avoid orphan rules)
  # Note: LIFO means this push_cleanup runs BEFORE the one above — push order matters.
  # We push rule deletion AFTER tag removal so it appears EARLIER in the stack (higher index).
  # Actually: last pushed = first popped = runs first.
  # We want: (1) delete rule, (2) remove tag.
  # So push tag removal first, then rule deletion last.
  # Stack after both pushes: [remove-tag, delete-rule]  → pops: delete-rule first, remove-tag second
  push_cleanup "gcloud compute firewall-rules delete $FW_RULE_NAME \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null || true"

  # Step 1: Add temporary chaos tag to the VM
  chaos "Adding temporary network tag '$CHAOS_TAG' to $TARGET_VM"
  run "gcloud compute instances add-tags $TARGET_VM \
    --tags=$CHAOS_TAG \
    --zone=$target_zone \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null"

  # Step 2: Create the DENY firewall rule targeting that tag
  chaos "Creating INGRESS DENY rule: $FW_RULE_NAME"
  chaos "Blocking tcp:80,443,8081-8085 from 0.0.0.0/0 to VMs tagged '$CHAOS_TAG'"
  chaos "Internal Docker overlay traffic (10.x.x.x) is NOT blocked."

  run "gcloud compute firewall-rules create $FW_RULE_NAME \
    --project=$GCP_PROJECT \
    --direction=INGRESS \
    --priority=500 \
    --network=default \
    --action=DENY \
    --rules=tcp:80,tcp:443,tcp:8081,tcp:8082,tcp:8083,tcp:8084,tcp:8085 \
    --source-ranges=0.0.0.0/0 \
    --target-tags=$CHAOS_TAG \
    --description='VCS chaos-monkey - auto-deleted. Rule for ${TARGET_VM}' \
    2>/dev/null"

  if ! $DRY_RUN; then
    info "Waiting 10s for GCP firewall rule to propagate..."
    sleep 10

    banner "Active chaos firewall rules:"
    gcloud compute firewall-rules list \
      --project="$GCP_PROJECT" \
      --filter="name:chaos-deny" \
      --format="table(name,direction,priority,action,targetTags.list())" \
      2>/dev/null | sed 's/^/  /' || true
  fi
}

# =============================================================================
# MAIN
# =============================================================================
main() {
  parse_args "$@"
  EXPERIMENT_START=$(date '+%Y-%m-%dT%H:%M:%S')

  echo ""
  echo -e "${BOLD}${RED}╔══════════════════════════════════════════════════════════╗"
  echo -e "║      VCS CHAOS MONKEY — GCP SWARM EDITION (v2)          ║"
  echo -e "╚══════════════════════════════════════════════════════════╝${NC}"
  echo ""

  log "$(printf '%.0s━' {1..56})"
  log "Experiment    : ${BOLD}${EXPERIMENT}${NC}"
  log "Target VM     : ${BOLD}${TARGET_VM}${NC}  (${VM_ZONE[${TARGET_VM}]:-unknown})"
  log "Cell (health) : ${BOLD}${CELL}${NC}  →  ${CELL_URL[$CELL]}"
  log "Duration      : ${BOLD}${DURATION}s${NC}"
  log "Poll interval : ${BOLD}${POLL_INTERVAL}s${NC}"
  if [[ "$EXPERIMENT" == "pause-container" ]]; then
    log "Target svc    : ${BOLD}${TARGET_SERVICE}${NC}"
  fi
  log "Dry-run       : ${BOLD}${DRY_RUN}${NC}"
  $DRY_RUN && warn "DRY-RUN MODE — no real changes will be made"
  log "$(printf '%.0s━' {1..56})"
  echo ""

  preflight

  # ── Baseline snapshot ───────────────────────────────────────────────────────
  banner "Swarm nodes BEFORE chaos (cell: $CELL):"
  swarm_safe "node ls --format 'table {{.Hostname}}\t{{.Status}}\t{{.Availability}}\t{{.ManagerStatus}}'" \
    | sed 's/^/  /' || true

  echo ""
  banner "Target service endpoint BEFORE chaos:"
  info "  ${TARGET_SERVICE:-nginx-health}: $(check_target_endpoint)"
  echo ""

  # ── Inject failure ──────────────────────────────────────────────────────────
  chaos "$(printf '%.0s━' {1..56})"
  chaos "INJECTING FAILURE — $(date '+%H:%M:%S')"
  chaos "$(printf '%.0s━' {1..56})"
  echo ""

  case "$EXPERIMENT" in
    drain-node)       experiment_drain_node ;;
    pause-container)  experiment_pause_container ;;
    block-firewall)   experiment_block_firewall ;;
  esac

  echo ""

  # ── Monitor during chaos window ─────────────────────────────────────────────
  monitor_loop "$DURATION"

  # ── Final endpoint check ────────────────────────────────────────────────────
  echo ""
  banner "Target service endpoint AFTER chaos window:"
  info "  ${TARGET_SERVICE}: $(check_target_endpoint)"

  echo ""
  info "Chaos window done. EXIT trap will now restore the system."
  # on_exit() runs automatically here via the trap
}

main "$@"
