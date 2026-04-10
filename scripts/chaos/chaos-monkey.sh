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
#     node removes all containers from that VM. Other nodes still serve.
#     Restoration: node availability set back to "active".
#
#   scale-service  (replaces pause-container)
#     Adds an impossible placement constraint to a service so the Swarm
#     scheduler finds zero valid nodes — effectively scaling to 0 tasks.
#     Safer than pause-container: containers stop cleanly, healthchecks
#     fail immediately, and nginx's DNS resolver stops routing within ~10s.
#     Supports a comma-separated list of services tested one at a time:
#       --service capacity-service,map-service
#     Each service is scaled to 0, observed for DURATION seconds, then
#     restored and allowed to recover before the next service is tested.
#     This lets you verify circuit breakers trip independently per service.
#     Restoration: impossible constraint removed; scheduler recreates tasks.
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
#   - Single-node limit: only one VM or service under chaos at any time.
#   - Dry-run mode: prints every action without executing anything.
#   - Auto-restore: cleanup runs automatically after --duration seconds.
#   - Early abort: if unexpected cell failures occur during chaos (non-target
#     infrastructure degrades), the script aborts and restores immediately.
#   - Report: full timeline written to debug-artifacts/chaos/<timestamp>/
#
# Usage:
#   ./scripts/chaos/chaos-monkey.sh [OPTIONS] <experiment> <target-vm>
#
# Options:
#   -c, --cell     eu|us|apac       Cell to observe health from (default: eu)
#   -d, --duration SECONDS          Chaos window per service (default: 60)
#   -p, --poll     SECONDS          Health poll interval (default: 10)
#   -r, --recover  SECONDS          Max recovery wait after restore (default: 180)
#   -s, --service  SVC[,SVC...]     Service(s) to test — comma-separated list for
#                                   scale-service (default: capacity-service).
#                                   Tested sequentially, one at a time.
#   -n, --dry-run                   Print actions, execute nothing
#   -v, --verbose                   Extra debug output
#       --max-cell-fails  N         Abort if non-target cell fails N times (default: 3)
#   -h, --help                      Show this help
#
# Arguments:
#   experiment    drain-node | scale-service | block-firewall
#   target-vm     GCP VM name (e.g. vcs-vm-eu2)
#
# Examples:
#   # Drain the EU worker node for 60s, watch services redistribute
#   ./scripts/chaos/chaos-monkey.sh drain-node vcs-vm-eu2
#
#   # Scale capacity-service to 0 to trip journey-service's circuit breaker
#   ./scripts/chaos/chaos-monkey.sh --service capacity-service scale-service vcs-vm-eu2
#
#   # Scale capacity-service then map-service sequentially, 90s each
#   ./scripts/chaos/chaos-monkey.sh --service capacity-service,map-service \
#     --duration 90 scale-service vcs-vm-eu2
#
#   # Scale db (CockroachDB local node) to 0 — services on VM lose DB access
#   ./scripts/chaos/chaos-monkey.sh --service db scale-service vcs-vm-eu2
#
#   # Scale Redis to 0 — tests graceful degradation and Stream consumer stoppage
#   ./scripts/chaos/chaos-monkey.sh --service redis scale-service vcs-vm-eu2
#
#   # Full infra chain: capacity-service → db → redis (sequential, 60s each)
#   ./scripts/chaos/chaos-monkey.sh --service capacity-service,db,redis \
#     --duration 60 scale-service vcs-vm-eu2
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

# Map each VM back to the cell it belongs to (used for abort-on-collateral-damage)
declare -A VM_CELL=(
  [vcs-vm-eu1]="eu"
  [vcs-vm-eu2]="eu"
  [vcs-vm-us1]="us"
  [vcs-vm-ap1]="apac"
)

# VCS Docker service names as they appear in "docker service ls"
declare -A VCS_SERVICES=(
  [iam-service]="${STACK_NAME}_iam-service"
  [capacity-service]="${STACK_NAME}_capacity-service"
  [journey-service]="${STACK_NAME}_journey-service"
  [map-service]="${STACK_NAME}_map-service"
  [notification-service]="${STACK_NAME}_notification-service"
  [db]="${STACK_NAME}_db"
  [redis]="${STACK_NAME}_redis"
)

# Unauthenticated probe endpoints for each service (accessed via nginx gateway).
# 200 or 401 both mean the service is reachable; 502/000 means it is down.
#
#   capacity-service  → /api/v1/capacity/segments  (public GET, no JWT; hits CockroachDB)
#   map-service       → /api/v1/map/segments        (public GET, no JWT)
#   iam-service       → /.well-known/jwks.json       (public, no JWT; served from memory)
#   journey-service   → /api/v1/journeys             (401 when alive = UP)
#   notification-svc  → /api/v1/notifications        (401 when alive = UP)
#
#   db (CockroachDB)  → /api/v1/capacity/segments   (PROXY: 500 means local CRDB node
#                        is down; services can't reach db:26257 on that node)
#   redis             → /api/v1/capacity/segments   (PROXY: returns 200 even when Redis
#                        is down — cache miss degrades to DB, which is EXPECTED.
#                        Silent failure: the journey.events Stream consumer stops
#                        consuming, so completed journeys no longer release capacity.
#                        Stream backlog is checked separately after restore.)
declare -A SERVICE_ENDPOINT=(
  [capacity-service]="/api/v1/capacity/segments"
  [map-service]="/api/v1/map/segments"
  [iam-service]="/.well-known/jwks.json"
  [journey-service]="/api/v1/journeys"
  [notification-service]="/api/v1/notifications"
  [db]="/api/v1/capacity/segments"
  [redis]="/api/v1/capacity/segments"
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
TARGET_CELL=""            # derived from TARGET_VM after parse
SCALE_SERVICES_LIST=()    # populated from --service for scale-service
MAX_CELL_FAILS=3          # abort if non-target cell fails this many polls in a row

# ── Cleanup stack (LIFO) ──────────────────────────────────────────────────────
# Each entry is a literal shell command string, eval'd in reverse order on EXIT.
CLEANUP_CMDS=()
CHAOS_TAG=""        # temp GCP network tag for block-firewall
FW_RULE_NAME=""     # firewall rule for block-firewall
EXPERIMENT_START=""
ABORT_REASON=""

# ── Report / timeline tracking ────────────────────────────────────────────────
REPORT_DIR=""
REPORT_FILE=""
TIMELINE_FILE=""
TOTAL_POLLS=0
EP_UP_COUNT=0
EP_DOWN_COUNT=0
NON_TARGET_CELL_FAIL_STREAK=0   # consecutive polls with a non-target cell down
ALL_CELLS_DOWN_STREAK=0          # consecutive polls with every cell unreachable
declare -a REPORT_EVENTS=()      # key events appended during run

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

# ── Report helpers ─────────────────────────────────────────────────────────────
report_init() {
  local ts_dir; ts_dir=$(date '+%Y%m%dT%H%M%SZ')
  REPORT_DIR="debug-artifacts/chaos/${ts_dir}"
  REPORT_FILE="${REPORT_DIR}/report.md"
  TIMELINE_FILE="${REPORT_DIR}/timeline.csv"

  if $DRY_RUN; then
    info "[DRY-RUN] Would create report at ${REPORT_DIR}/"
    return
  fi

  mkdir -p "$REPORT_DIR"
  # Write CSV header
  echo "timestamp,elapsed_s,eu_health,us_health,apac_health,endpoint_status,notes" \
    > "$TIMELINE_FILE"
  info "Report directory: ${REPORT_DIR}/"
}

report_event() {
  local kind="$1" msg="$2"
  local entry; entry="$(date '+%H:%M:%S') [${kind}] ${msg}"
  REPORT_EVENTS+=("$entry")
  debug "report_event: $entry"
}

# timeline_row writes one poll snapshot to the CSV
timeline_row() {
  local elapsed="$1" eu_h="$2" us_h="$3" ap_h="$4" ep="$5" notes="${6:-}"
  $DRY_RUN && return
  [[ -z "$TIMELINE_FILE" ]] && return
  printf '%s,%s,%s,%s,%s,"%s","%s"\n' \
    "$(date '+%H:%M:%S')" "$elapsed" "$eu_h" "$us_h" "$ap_h" "$ep" "$notes" \
    >> "$TIMELINE_FILE"
}

# report_finalize writes the markdown summary report
report_finalize() {
  $DRY_RUN && return
  [[ -z "$REPORT_FILE" ]] && return

  local final_eu final_us final_ap
  final_eu=$(cell_health eu)
  final_us=$(cell_health us)
  final_ap=$(cell_health apac)

  local abort_section=""
  if [[ -n "$ABORT_REASON" ]]; then
    abort_section="
> **EARLY ABORT TRIGGERED**
> Reason: ${ABORT_REASON}
"
  fi

  local result="PASS"
  [[ -n "$ABORT_REASON" ]] && result="ABORTED"
  [[ "$final_eu" != "healthy" || "$final_us" != "healthy" || "$final_ap" != "healthy" ]] \
    && result="DEGRADED"

  cat > "$REPORT_FILE" <<MARKDOWN
# VCS Chaos Monkey Report

| Field | Value |
|-------|-------|
| **Experiment** | ${EXPERIMENT} |
| **Target VM** | ${TARGET_VM} (${VM_ZONE[${TARGET_VM}]:-unknown}) |
| **Cell observed** | ${CELL} |
| **Services tested** | $(IFS=', '; echo "${SCALE_SERVICES_LIST[*]:-${TARGET_SERVICE}}") |
| **Duration per service** | ${DURATION}s |
| **Poll interval** | ${POLL_INTERVAL}s |
| **Start time** | ${EXPERIMENT_START} |
| **End time** | $(date '+%Y-%m-%dT%H:%M:%S') |
| **Result** | **${result}** |

${abort_section}

## Pre-flight Summary

All cells verified healthy before chaos was injected.

## Key Events

$(printf '%s\n' "${REPORT_EVENTS[@]:-none}")

## Endpoint Health Summary

| Metric | Value |
|--------|-------|
| Total polls | ${TOTAL_POLLS} |
| Target endpoint UP | ${EP_UP_COUNT} |
| Target endpoint DOWN / ERROR | ${EP_DOWN_COUNT} |
| Down % | $(( TOTAL_POLLS > 0 ? EP_DOWN_COUNT * 100 / TOTAL_POLLS : 0 ))% |

## Final Cell Status

| Cell | Status |
|------|--------|
| EU | ${final_eu} |
| US | ${final_us} |
| APAC | ${final_ap} |

## Circuit Breaker Notes

Journey-service capacity CB: opens after **10 consecutive network failures**,
stays open for **30s**, probes with **3 requests** before closing.
Under load (K6 concurrent), the CB trips within ~10–15s of capacity-service going dark.
Without concurrent traffic the CB state is not directly observable via curl.

## Infrastructure Failure Behaviour

### CockroachDB (db) — scale to 0

| Behaviour | Details |
|-----------|---------|
| **Immediate effect** | Services on the affected node lose db:26257 — all DB reads/writes fail |
| **HTTP observable** | \`/api/v1/capacity/segments\` → 500 within ~5s |
| **Other cells** | Unaffected; each cell has its own local CRDB container |
| **CRDB cluster** | Quorum maintained (3 of 4 nodes still running) |
| **Cache buffer** | Redis segment cache serves reads for up to cacheTTL after DB loss |
| **Recovery** | Services auto-reconnect; no data loss (CRDB Raft log intact) |

### Redis Streams — scale to 0

| Behaviour | Details |
|-----------|---------|
| **HTTP observable** | All endpoints still return 200 — Redis failure is transparent |
| **★ Silent failure** | \`journey.events\` Stream consumer stops; completed/cancelled journeys NO LONGER release their capacity slots |
| **Capacity leak** | Slots appear permanently reserved; manifests as spurious "capacity full" errors over time |
| **Route cache** | Cache misses bypass Redis → direct OSRM calls (slower, risk of 429) |
| **Segment cache** | Falls back to CockroachDB SELECT — functionally correct, slightly slower |
| **Outbox relay** | journey-service logs warnings, retries XADD on next tick; events buffered in DB outbox |
| **Recovery** | Consumer reconnects; outbox relay re-publishes; capacity releases catch up |
| **Verify** | Check stream backlog via \`redis-cli XPENDING journey.events capacity-consumer-group - + 10\` |

## Full Timeline

See \`timeline.csv\` in this directory for per-poll snapshots.

---
*Generated by chaos-monkey.sh*
MARKDOWN

  info "Report written: ${REPORT_FILE}"
}

# ── EXIT trap — always runs, always restores ──────────────────────────────────
on_exit() {
  local code=$?
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

  report_finalize
  print_final_status

  return "$code"
}
trap on_exit EXIT

# =============================================================================
# ARGUMENT PARSING
# =============================================================================
usage() {
  sed -n '3,/^# ====/p' "$0" | head -72 | sed 's/^# \?//'
  exit 0
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -c|--cell)             CELL="$2";              shift 2 ;;
      -d|--duration)         DURATION="$2";          shift 2 ;;
      -p|--poll)             POLL_INTERVAL="$2";     shift 2 ;;
      -r|--recover)          RECOVER_TIMEOUT="$2";   shift 2 ;;
      -s|--service|--services) TARGET_SERVICE="$2"; shift 2 ;;
      -n|--dry-run)          DRY_RUN=true;           shift   ;;
      -v|--verbose)          VERBOSE=true;           shift   ;;
      --max-cell-fails)      MAX_CELL_FAILS="$2";    shift 2 ;;
      -h|--help)             usage ;;
      -*)                    error "Unknown flag: $1"; usage ;;
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

  # Derive the cell this VM belongs to
  TARGET_CELL="${VM_CELL[$TARGET_VM]:-}"

  # For scale-service, split TARGET_SERVICE on commas into SCALE_SERVICES_LIST
  IFS=',' read -ra SCALE_SERVICES_LIST <<< "$TARGET_SERVICE"
}

# =============================================================================
# GCP / SWARM HELPERS
# =============================================================================

# Run a command on a remote GCP VM.
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

# =============================================================================
# HEALTH CHECKS
# =============================================================================

# Returns "healthy" or "unreachable" for a given cell (checks nginx gateway).
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

# Probe the target service's public endpoint via the observed cell's gateway.
# Returns a human-readable status string for the monitor table.
#
# Endpoint mapping (all probed through nginx, no JWT required):
#   capacity-service   /api/v1/capacity/segments  → 200 when healthy
#   map-service        /api/v1/map/segments        → 200 when healthy
#   iam-service        /.well-known/jwks.json       → 200 when healthy
#   journey-service    /api/v1/journeys             → 401 when healthy (JWT required)
#   notification-svc   /api/v1/notifications        → 401 when healthy (JWT required)
#
# 200 and 401 are both treated as UP (service is reachable).
# 502 means nginx cannot reach the upstream (service is down or has 0 tasks).
# 000 means the cell's nginx is itself unreachable.
check_target_endpoint() {
  local svc="${1:-$TARGET_SERVICE}"
  local cell_url="${CELL_URL[$CELL]}"
  local endpoint="${SERVICE_ENDPOINT[$svc]:-/nginx-health}"

  local http_code
  http_code=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 \
    "${cell_url}${endpoint}" 2>/dev/null || echo "000")

  if [[ "$http_code" == "200" || "$http_code" == "401" ]]; then
    echo "UP ($http_code)"
  elif [[ "$http_code" == "000" ]]; then
    echo "UNREACHABLE"
  else
    echo "ERROR ($http_code)"
  fi
}

# =============================================================================
# INFRASTRUCTURE SERVICE GUARDS
# =============================================================================

# CockroachDB quorum guard for scale-service.
# 4-node CRDB cluster: Raft majority = 3.  Scaling db to 0 on ONE node leaves
# 3 nodes still running across the other VMs → quorum is maintained.
# We block only if the cluster is already degraded before we start.
scale_db_quorum_guard() {
  info "CockroachDB quorum guard (scale-service)..."
  local live
  live=$(count_live_crdb_nodes)
  info "  Live CRDB nodes (running tasks): $live"

  if [[ "$live" -le 2 ]]; then
    error "QUORUM GUARD BLOCKED: Only $live CRDB nodes running."
    error "Scaling db to 0 on one more node would break Raft consensus."
    error "Repair the cluster before running db chaos experiments."
    exit 1
  elif [[ "$live" -eq 3 ]]; then
    warn "  Only 3 CRDB nodes live. Scaling db to 0 on $TARGET_VM will leave exactly"
    warn "  the minimum quorum of 3. Monitor CockroachDB admin UI closely."
    warn "  Sleeping 5s — Ctrl-C now to abort."
    $DRY_RUN || sleep 5
  else
    info "  Quorum safe: $live nodes running; will have $((live-1)) during chaos."
  fi
}

# Print what to observe for the given service failure and what silently breaks.
# Called immediately before chaos is injected for each service.
announce_service_impact() {
  local svc="$1"
  echo ""
  case "$svc" in
    db)
      chaos "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
      chaos "WHAT BREAKS when CockroachDB local node goes to 0 tasks:"
      chaos "  • All services on this VM lose db:26257 connectivity"
      chaos "  • capacity-service: SELECT/INSERT fail → 500 on all write endpoints"
      chaos "  • journey-service:  journey creates fail → 500"
      chaos "  • iam-service:      login/registration fail → 500"
      chaos "  • Other cells: UNAFFECTED (each has its own local CRDB node)"
      chaos "  • CRDB cluster: quorum maintained (3 nodes still up)"
      chaos "WHAT STAYS WORKING:"
      chaos "  • Reads that are already cached in Redis (segment GET, route cache)"
      chaos "  • IAM JWKS endpoint (served from memory, no DB read)"
      chaos "  • nginx health check"
      chaos "PROBE: /api/v1/capacity/segments → expect ERROR (500) within ~5s"
      chaos "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
      ;;
    redis)
      chaos "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
      chaos "WHAT BREAKS when Redis goes to 0 tasks:"
      chaos "  ★ SILENT FAILURE: journey.events Stream consumer stops."
      chaos "    Completed/cancelled journeys NO LONGER release capacity."
      chaos "    Slots appear permanently occupied — capacity leak over time."
      chaos "    Events pile up in journey-service DB outbox (outbox_relay retries)."
      chaos "  • Route cache: bypassed (cache miss → direct OSRM call)."
      chaos "    Increases OSRM latency + risk of Nominatim 429 rate-limiting."
      chaos "  • Capacity segment cache: falls back to CockroachDB reads."
      chaos "    Slightly slower but functionally correct."
      chaos "WHAT STAYS WORKING:"
      chaos "  • All HTTP endpoints return 200 (Redis failure is transparent to callers)"
      chaos "  • Reservations still succeed (locking uses CockroachDB SELECT FOR UPDATE)"
      chaos "  • IAM JWKS endpoint (no Redis dependency)"
      chaos "PROBE: /api/v1/capacity/segments → expect UP (200) — degraded, not broken."
      chaos "Stream backlog will be checked via redis-cli after restore."
      chaos "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
      ;;
    capacity-service)
      chaos "WHAT BREAKS: journey creates fail (capacity CB opens after 10 network failures)."
      chaos "PROBE: /api/v1/capacity/segments → expect ERROR (502) within ~10s."
      ;;
    map-service)
      chaos "WHAT BREAKS: journey creates fail (map CB opens after 10 network failures)."
      chaos "PROBE: /api/v1/map/segments → expect ERROR (502) within ~10s."
      ;;
    *)
      chaos "WHAT BREAKS: ${svc} unavailable (502 from nginx)."
      ;;
  esac
  echo ""
}

# After restoring Redis, check the journey.events Stream for pending (un-ACKed)
# messages. A non-zero pending count means events backed up while Redis was down
# and the capacity-service consumer group has not yet caught up.
check_redis_stream_backlog() {
  local mgr="${CELL_MANAGER[$CELL]}" zone="${CELL_ZONE[$CELL]}"
  if $DRY_RUN; then
    info "[DRY-RUN] Would check Redis stream backlog via redis-cli"
    return
  fi

  info "Checking journey.events stream backlog (capacity-service consumer group)..."
  local redis_cid
  redis_cid=$(gcp_ssh_safe "$mgr" "$zone" \
    "sudo docker ps --filter 'name=${STACK_NAME}_redis' --format '{{.ID}}' | head -1")

  if [[ -z "$redis_cid" ]]; then
    warn "  Redis container not found on manager — cannot check stream backlog."
    return
  fi

  local stream_len pending_info
  stream_len=$(gcp_ssh_safe "$mgr" "$zone" \
    "sudo docker exec $redis_cid redis-cli XLEN journey.events 2>/dev/null || echo 'N/A'")
  pending_info=$(gcp_ssh_safe "$mgr" "$zone" \
    "sudo docker exec $redis_cid redis-cli XPENDING journey.events capacity-consumer-group - + 10 2>/dev/null || echo 'N/A'")

  info "  journey.events stream length : ${stream_len:-N/A}"
  if [[ -n "$pending_info" && "$pending_info" != "N/A" && "$pending_info" != "(empty list or set)" ]]; then
    warn "  Pending (un-ACKed) messages  : YES — capacity consumer group has not caught up."
    warn "  Affected: completed/cancelled journeys have NOT yet released their capacity slots."
    warn "  The outbox relay will re-publish and the consumer will process them on next poll."
    report_event "stream_backlog_detected" "journey.events has un-ACKed messages after Redis restore"
  else
    info "  Pending (un-ACKed) messages  : none — consumer group is caught up."
    report_event "stream_backlog_ok" "journey.events consumer group caught up after Redis restore"
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
    drain-node|scale-service|block-firewall) ;;
    *)
      error "Unknown experiment '$EXPERIMENT'. Valid: drain-node, scale-service, block-firewall"
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

  # 6. Validate service names (scale-service only)
  if [[ "$EXPERIMENT" == "scale-service" ]]; then
    for svc in "${SCALE_SERVICES_LIST[@]}"; do
      if [[ -z "${VCS_SERVICES[$svc]+_}" ]]; then
        error "Unknown service '$svc'. Known: ${!VCS_SERVICES[*]}"
        exit 1
      fi
    done
    info "Services to test (sequential): ${SCALE_SERVICES_LIST[*]}"
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

  # 9. Single-node cell warning (drain/block only)
  if [[ "$EXPERIMENT" != "scale-service" ]]; then
    local node_cell="${TARGET_CELL:-}"
    if [[ "$node_cell" == "us" || "$node_cell" == "apac" ]]; then
      warn "WARNING: '$TARGET_VM' is the ONLY node in the '$node_cell' cell."
      warn "Draining or blocking it means COMPLETE OUTAGE for that cell — no fallback node exists."
      warn "Other cells (EU, APAC/US) are unaffected."
      warn "Sleeping 5s — Ctrl-C now to abort."
      $DRY_RUN || sleep 5
    fi
  fi

  # 10. Health gate — all cells must be healthy before we proceed
  info "Checking all cells are healthy..."
  local retries=3 attempt=0
  while [[ $attempt -lt $retries ]]; do
    if all_cells_healthy; then
      info "All cells healthy — ready to proceed."
      report_event "preflight_ok" "All cells healthy, CRDB quorum verified"
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
  # Show which cells are bad so engineers can investigate
  for c in eu us apac; do
    local s; s=$(cell_health "$c")
    info "  ${c}: ${s}  →  ${CELL_URL[$c]}"
  done
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

# monitor_loop <duration_seconds> [display_service_name]
# Polls all cell health + target endpoint every POLL_INTERVAL seconds.
# Writes each snapshot to the timeline CSV.
# Aborts early if unexpected non-target cell failures accumulate.
monitor_loop() {
  local duration="$1"
  local display_svc="${2:-$TARGET_SERVICE}"
  local elapsed=0

  info "Monitoring for ${duration}s (poll every ${POLL_INTERVAL}s)..."
  info "Observing endpoint: ${display_svc} → ${SERVICE_ENDPOINT[$display_svc]:-/nginx-health}"
  info "Tip: run K6 load tests concurrently to generate traffic and observe circuit breakers."
  echo ""
  printf "  ${BOLD}%-8s %-13s %-13s %-13s %-26s${NC}\n" \
    "TIME" "EU-Cell" "US-Cell" "APAC-Cell" "${display_svc} endpoint"
  echo "  $(printf '%.0s─' {1..72})"

  while [[ $elapsed -lt $duration ]]; do
    local eu_h us_h ap_h ep_state
    eu_h=$(cell_health eu)
    us_h=$(cell_health us)
    ap_h=$(cell_health apac)
    ep_state=$(check_target_endpoint "$display_svc")

    TOTAL_POLLS=$(( TOTAL_POLLS + 1 ))
    if [[ "$ep_state" == UP* ]]; then
      EP_UP_COUNT=$(( EP_UP_COUNT + 1 ))
    else
      EP_DOWN_COUNT=$(( EP_DOWN_COUNT + 1 ))
    fi

    # Colour helpers (inline so they work inside the loop)
    _color_cell() {
      case "$1" in
        healthy)     printf "${GREEN}%-13s${NC}" "$1" ;;
        unreachable) printf "${RED}%-13s${NC}" "$1" ;;
        *)           printf "${YELLOW}%-13s${NC}" "$1" ;;
      esac
    }
    _color_ep() {
      case "$1" in
        UP*)          printf "${GREEN}%-26s${NC}" "$1" ;;
        UNREACHABLE*) printf "${RED}%-26s${NC}" "$1" ;;
        *)            printf "${YELLOW}%-26s${NC}" "$1" ;;
      esac
    }

    printf "  %-8s" "${elapsed}s"
    _color_cell "$eu_h"
    _color_cell "$us_h"
    _color_cell "$ap_h"
    _color_ep "$ep_state"
    echo ""

    # Write to timeline CSV
    timeline_row "$elapsed" "$eu_h" "$us_h" "$ap_h" "$ep_state"

    # ── Early abort logic ────────────────────────────────────────────────────
    # ALL cells down: catastrophic, abort immediately
    if [[ "$eu_h" == "unreachable" && "$us_h" == "unreachable" && "$ap_h" == "unreachable" ]]; then
      ALL_CELLS_DOWN_STREAK=$(( ALL_CELLS_DOWN_STREAK + 1 ))
      if [[ $ALL_CELLS_DOWN_STREAK -ge 1 ]]; then
        ABORT_REASON="ALL 3 cells unreachable at ${elapsed}s — catastrophic failure, aborting immediately"
        error "$ABORT_REASON"
        report_event "ABORT" "$ABORT_REASON"
        exit 2
      fi
    else
      ALL_CELLS_DOWN_STREAK=0
    fi

    # Non-target cell failure: unexpected collateral damage
    # For drain-node / block-firewall, the target cell is expected to degrade.
    # For scale-service, no cell should degrade at all (nginx stays up).
    local non_target_fail=false
    for check_cell in eu us apac; do
      # Skip the expected-to-fail cell (only for node-level experiments)
      [[ "$EXPERIMENT" != "scale-service" && "$check_cell" == "${TARGET_CELL:-none}" ]] && continue
      local ch_health; eval "ch_health=\${${check_cell}_h}"
      if [[ "$ch_health" == "unreachable" ]]; then
        non_target_fail=true
        break
      fi
    done

    if $non_target_fail; then
      NON_TARGET_CELL_FAIL_STREAK=$(( NON_TARGET_CELL_FAIL_STREAK + 1 ))
      warn "Non-target cell unreachable (streak: ${NON_TARGET_CELL_FAIL_STREAK}/${MAX_CELL_FAILS})"
      if [[ $NON_TARGET_CELL_FAIL_STREAK -ge $MAX_CELL_FAILS ]]; then
        ABORT_REASON="Non-target cell unreachable for ${NON_TARGET_CELL_FAIL_STREAK} consecutive polls at ${elapsed}s — aborting to prevent further damage"
        error "$ABORT_REASON"
        report_event "ABORT" "$ABORT_REASON"
        exit 2
      fi
    else
      NON_TARGET_CELL_FAIL_STREAK=0
    fi

    # Every 30s print Swarm task distribution for the current service
    if (( elapsed > 0 && elapsed % 30 == 0 )); then
      echo ""
      banner "Swarm task distribution for ${STACK_NAME}_${display_svc} (cell: $CELL):"
      swarm_safe "service ps ${STACK_NAME}_${display_svc} \
        --format 'table {{.Name}}\t{{.Node}}\t{{.CurrentState}}\t{{.DesiredState}}'" \
        | sed 's/^/    /' || true
      echo ""
    fi

    sleep "$POLL_INTERVAL"
    elapsed=$(( elapsed + POLL_INTERVAL ))
  done

  echo ""
  info "Monitoring window complete for ${display_svc}."
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
      report_event "recovered" "All cells healthy after ${elapsed}s"
      return 0
    fi
    sleep "$POLL_INTERVAL"
    elapsed=$(( elapsed + POLL_INTERVAL ))
    restlog "  Waiting... ${elapsed}s / ${RECOVER_TIMEOUT}s"
  done

  warn "System did not fully recover within ${RECOVER_TIMEOUT}s."
  for c in eu us apac; do
    local s; s=$(cell_health "$c")
    [[ "$s" != "healthy" ]] && warn "  ${c}: $s  →  ${CELL_URL[$c]}"
  done
  report_event "recovery_timeout" "Not all cells healthy within ${RECOVER_TIMEOUT}s"
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
  banner "Services (replicas):"
  swarm_safe "service ls --format 'table {{.Name}}\t{{.Replicas}}\t{{.Image}}'" \
    | sed 's/^/  /' || true

  if [[ -n "$REPORT_FILE" && -f "$REPORT_FILE" ]]; then
    echo ""
    info "Full report: ${REPORT_FILE}"
    info "Timeline:    ${TIMELINE_FILE}"
  fi
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
  info "Node $TARGET_VM  (swarm ID: $node_id)  current availability: $original_avail"

  push_cleanup "gcloud compute ssh $mgr \
    --project=$GCP_PROJECT \
    --zone=$zone \
    --quiet \
    --command='sudo docker node update --availability $original_avail $node_id' \
    2>/dev/null"

  chaos "Draining node $TARGET_VM (ID: $node_id)"
  chaos "All containers on $TARGET_VM will stop. Other nodes absorb the load."
  report_event "chaos_start" "Drained node ${TARGET_VM} (${node_id})"

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
# EXPERIMENT: scale-service
# =============================================================================
# Adds an impossible placement constraint to a VCS global-mode service so the
# Swarm scheduler finds zero valid nodes — effectively scaling to 0 tasks.
# The constraint key is unique per run (includes timestamp + service name) so
# multiple experiments cannot collide.
#
# For each service in SCALE_SERVICES_LIST:
#   1. Apply impossible constraint → 0 tasks
#   2. Wait 20s for tasks to drain
#   3. monitor_loop for DURATION seconds
#   4. If more services remain: restore this one + wait for recovery
#   5. Repeat for next service
# on_exit() handles final restore for the last (or any interrupted) service.
#
# Circuit breaker config in journey-service (as of last update):
#   - Capacity CB: opens after 10 consecutive network failures, 30s timeout
#   - Map CB:      same settings
#   - HTTP client timeout: 15s
# Run K6 concurrently to generate the traffic that trips the breaker.
# =============================================================================
experiment_scale_service() {
  local mgr="${CELL_MANAGER[$CELL]}" zone="${CELL_ZONE[$CELL]}"
  local ts_suffix; ts_suffix=$(date '+%s')
  local total_svcs=${#SCALE_SERVICES_LIST[@]}

  info "Scale-service experiment: ${SCALE_SERVICES_LIST[*]}"
  info "Each service will be scaled to 0 for ${DURATION}s then restored before the next."
  echo ""

  # Infrastructure guards — check before starting any chaos
  for svc in "${SCALE_SERVICES_LIST[@]}"; do
    if [[ "$svc" == "db" ]]; then
      scale_db_quorum_guard
    fi
    if [[ "$svc" == "redis" ]]; then
      warn "Redis chaos: HTTP endpoints will remain UP (graceful degradation to DB)."
      warn "Silent failure: journey.events Stream consumer stops → capacity leak."
      warn "Stream backlog will be checked after restore."
    fi
  done

  local idx=0
  for svc in "${SCALE_SERVICES_LIST[@]}"; do
    idx=$(( idx + 1 ))
    local full_svc="${STACK_NAME}_${svc}"
    # Unique constraint — safe to remove even if run is interrupted
    local constraint="node.hostname==chaos-no-match-${ts_suffix}-${svc}"

    banner "[$idx/$total_svcs] Testing: ${svc}"

    # Validate service exists on this cell's swarm
    local existing
    existing=$(swarm_safe "service ls --filter name=${full_svc} --format '{{.Name}}'")
    if [[ -z "$existing" ]]; then
      error "Service '${full_svc}' not found in Swarm for cell '${CELL}'."
      error "Is the stack deployed? Check: gcloud compute ssh $mgr --zone=$zone -- sudo docker service ls"
      exit 1
    fi

    info "Baseline task state for ${svc}:"
    swarm_safe "service ps ${full_svc} \
      --filter desired-state=running \
      --format 'table {{.Name}}\t{{.Node}}\t{{.CurrentState}}'" \
      | sed 's/^/  /' || true
    echo ""

    info "Endpoint before chaos: $(check_target_endpoint "$svc")"

    # Push cleanup BEFORE applying chaos so on_exit always has a restore command.
    # constraint-rm on a service that doesn't have this constraint is harmless (we suppress errors).
    push_cleanup "gcloud compute ssh $mgr \
      --project=$GCP_PROJECT \
      --zone=$zone \
      --quiet \
      --command='sudo docker service update --constraint-rm ${constraint} ${full_svc} 2>/dev/null || true' \
      2>/dev/null || true"

    chaos "$(printf '%.0s━' {1..56})"
    chaos "Scaling ${svc} to 0 — adding impossible constraint"
    chaos "Constraint: ${constraint}"
    chaos "$(printf '%.0s━' {1..56})"
    report_event "chaos_start" "${svc} scaled to 0 (constraint: ${constraint})"

    run "gcloud compute ssh $mgr \
      --project=$GCP_PROJECT \
      --zone=$zone \
      --quiet \
      --command='sudo docker service update --constraint-add ${constraint} ${full_svc}' \
      2>/dev/null"

    if ! $DRY_RUN; then
      info "Waiting 20s for tasks to drain from all nodes..."
      sleep 20

      banner "Task state after scale-to-0 (all should be Pending/Shutdown):"
      swarm_safe "service ps ${full_svc} \
        --format 'table {{.Name}}\t{{.Node}}\t{{.CurrentState}}\t{{.DesiredState}}\t{{.Error}}'" \
        | sed 's/^/  /' || true
      echo ""

      info "Endpoint after chaos: $(check_target_endpoint "$svc")"
    fi

    # Print what to observe for this specific service failure
    announce_service_impact "$svc"

    # Monitor this service for the full DURATION
    monitor_loop "$DURATION" "$svc"

    # Restore this service (whether or not there are more to test).
    # The constraint-rm on the last service is also run by on_exit(), but
    # doing it here for intermediate services gives them time to recover
    # before the next service is scaled to 0.
    echo ""
    restlog "Restoring ${svc}..."
    report_event "restore_intermediate" "Restoring ${svc}"

    run "gcloud compute ssh $mgr \
      --project=$GCP_PROJECT \
      --zone=$zone \
      --quiet \
      --command='sudo docker service update --constraint-rm ${constraint} ${full_svc} 2>/dev/null || true' \
      2>/dev/null"

    if ! $DRY_RUN; then
      restlog "Waiting for ${svc} to become healthy again..."
      local recover_elapsed=0
      while [[ $recover_elapsed -lt $RECOVER_TIMEOUT ]]; do
        local ep; ep=$(check_target_endpoint "$svc")
        restlog "  ${svc}: ${ep} (${recover_elapsed}s)"
        if [[ "$ep" == UP* ]]; then
          restlog "${svc} endpoint healthy after ${recover_elapsed}s."
          report_event "recovered_intermediate" "${svc} recovered after ${recover_elapsed}s"
          break
        fi
        sleep "$POLL_INTERVAL"
        recover_elapsed=$(( recover_elapsed + POLL_INTERVAL ))
      done
      if [[ $recover_elapsed -ge $RECOVER_TIMEOUT ]]; then
        warn "${svc} did not recover within ${RECOVER_TIMEOUT}s."
        report_event "recovery_timeout_intermediate" "${svc} did not recover within ${RECOVER_TIMEOUT}s"
      fi

      # Redis-specific: after Redis is back, check the Stream consumer backlog
      if [[ "$svc" == "redis" ]]; then
        sleep 10  # give consumer group time to reconnect and start processing
        check_redis_stream_backlog
      fi

      # Give the system a brief settling period before the next service test
      if [[ $idx -lt $total_svcs ]]; then
        info "Settling for 10s before next service test..."
        sleep 10
      fi
    fi
    echo ""
  done
}

# =============================================================================
# EXPERIMENT: block-firewall
# =============================================================================
# Adds a temporary GCP network tag ("chaos-block-<timestamp>") to the target
# VM, then creates a high-priority INGRESS DENY firewall rule targeting that
# tag. Blocks all inbound HTTP/HTTPS and VCS service ports from 0.0.0.0/0.
# Internal Docker overlay network (VPC 10.x.x.x) is NOT affected.
# Restoration: delete firewall rule → remove temp tag (LIFO order).
# =============================================================================
experiment_block_firewall() {
  local target_zone="${VM_ZONE[$TARGET_VM]}"
  local ts_suffix; ts_suffix=$(date '+%s')

  CHAOS_TAG="chaos-block-${ts_suffix}"
  FW_RULE_NAME="chaos-deny-${TARGET_VM}-${ts_suffix}"

  info "Target VM : $TARGET_VM (zone: $target_zone)"
  info "Chaos tag : $CHAOS_TAG"
  info "FW rule   : $FW_RULE_NAME"

  # LIFO cleanup: last pushed = first popped = runs first.
  # We want: (1) delete FW rule, (2) remove tag.
  # Push tag-removal first, rule-deletion second → pops rule first.
  push_cleanup "gcloud compute instances remove-tags $TARGET_VM \
    --tags=$CHAOS_TAG \
    --zone=$target_zone \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null || true"

  push_cleanup "gcloud compute firewall-rules delete $FW_RULE_NAME \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null || true"

  chaos "Adding temporary network tag '$CHAOS_TAG' to $TARGET_VM"
  run "gcloud compute instances add-tags $TARGET_VM \
    --tags=$CHAOS_TAG \
    --zone=$target_zone \
    --project=$GCP_PROJECT \
    --quiet \
    2>/dev/null"

  chaos "Creating INGRESS DENY rule: $FW_RULE_NAME"
  chaos "Blocking tcp:80,443,8081-8085 from 0.0.0.0/0 to tag '$CHAOS_TAG'"
  chaos "Internal Docker overlay traffic (10.x.x.x) is NOT blocked."
  report_event "chaos_start" "Firewall rule ${FW_RULE_NAME} blocking ${TARGET_VM}"

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
  echo -e "║      VCS CHAOS MONKEY — GCP SWARM EDITION (v3)          ║"
  echo -e "╚══════════════════════════════════════════════════════════╝${NC}"
  echo ""

  log "$(printf '%.0s━' {1..56})"
  log "Experiment    : ${BOLD}${EXPERIMENT}${NC}"
  log "Target VM     : ${BOLD}${TARGET_VM}${NC}  (${VM_ZONE[${TARGET_VM}]:-unknown})"
  log "Target cell   : ${BOLD}${TARGET_CELL:-derived from VM}${NC}"
  log "Cell (health) : ${BOLD}${CELL}${NC}  →  ${CELL_URL[$CELL]}"
  log "Duration      : ${BOLD}${DURATION}s per service${NC}"
  log "Poll interval : ${BOLD}${POLL_INTERVAL}s${NC}"
  if [[ "$EXPERIMENT" == "scale-service" ]]; then
    log "Services      : ${BOLD}$(IFS=', '; echo "${SCALE_SERVICES_LIST[*]}")${NC}  (sequential)"
  fi
  log "Max cell fails: ${BOLD}${MAX_CELL_FAILS}${NC} before abort"
  log "Dry-run       : ${BOLD}${DRY_RUN}${NC}"
  $DRY_RUN && warn "DRY-RUN MODE — no real changes will be made"
  log "$(printf '%.0s━' {1..56})"
  echo ""

  report_init
  preflight

  # ── Baseline snapshot ───────────────────────────────────────────────────────
  banner "Swarm nodes BEFORE chaos (cell: $CELL):"
  swarm_safe "node ls --format 'table {{.Hostname}}\t{{.Status}}\t{{.Availability}}\t{{.ManagerStatus}}'" \
    | sed 's/^/  /' || true
  echo ""

  if [[ "$EXPERIMENT" == "scale-service" ]]; then
    for svc in "${SCALE_SERVICES_LIST[@]}"; do
      info "  ${svc} endpoint BEFORE chaos: $(check_target_endpoint "$svc")"
    done
  else
    banner "Target endpoint BEFORE chaos (${TARGET_SERVICE}):"
    info "  $(check_target_endpoint "$TARGET_SERVICE")"
  fi
  echo ""

  # ── Inject failure ──────────────────────────────────────────────────────────
  chaos "$(printf '%.0s━' {1..56})"
  chaos "INJECTING FAILURE — $(date '+%H:%M:%S')"
  chaos "$(printf '%.0s━' {1..56})"
  echo ""

  case "$EXPERIMENT" in
    drain-node)      experiment_drain_node ;;
    scale-service)   experiment_scale_service ;;
    block-firewall)  experiment_block_firewall ;;
  esac

  # For drain-node and block-firewall, run the monitor loop after injecting
  # (scale-service runs its own per-service monitor loops internally)
  if [[ "$EXPERIMENT" != "scale-service" ]]; then
    echo ""
    monitor_loop "$DURATION" "$TARGET_SERVICE"

    echo ""
    banner "Target endpoint AFTER chaos window (${TARGET_SERVICE}):"
    info "  $(check_target_endpoint "$TARGET_SERVICE")"
  fi

  echo ""
  info "Chaos window done. EXIT trap will now restore the system."
  # on_exit() runs automatically here via the trap
}

main "$@"
