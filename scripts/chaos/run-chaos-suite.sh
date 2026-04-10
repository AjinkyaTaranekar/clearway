#!/usr/bin/env bash
# =============================================================================
# run-chaos-suite.sh — Full VCS Chaos Engineering Suite
# =============================================================================
#
# Runs a planned sequence of chaos experiments against the live GCP stack,
# verifies health between each experiment, and generates a combined report.
#
# Run from the project root in GCP Cloud Shell:
#   ./scripts/chaos/run-chaos-suite.sh [OPTIONS]
#
# Options:
#   --cell    eu|us|apac      Cell under test (default: eu)
#   --duration SECONDS        Chaos window per experiment (default: 60)
#   --pause   SECONDS         Minimum cool-down between experiments (default: 60)
#   --dry-run                 Preview actions, execute nothing
#   --skip-node               Skip the drain-node and block-firewall experiments
#   -h, --help
#
# Examples:
#   ./scripts/chaos/run-chaos-suite.sh                     # EU suite, default timing
#   ./scripts/chaos/run-chaos-suite.sh --dry-run           # Preview only
#   ./scripts/chaos/run-chaos-suite.sh --duration 90       # Longer windows
#   ./scripts/chaos/run-chaos-suite.sh --skip-node         # Service-level only
#
# Experiment sequence (EU cell):
#   Phase 1 — App services (scale to 0, tests circuit breakers)
#     1  capacity-service   → journey CB trips after 10 failures
#     2  map-service        → journey CB trips after 10 failures
#     3  iam-service        → auth fails on this node
#     4  notification-service → notifications fail (journeys still work)
#   Phase 2 — Infrastructure (scale to 0)
#     5  redis              → Stream consumer stops (silent capacity leak)
#     6  db                 → all DB ops fail on this node
#   Phase 3 — Node-level (requires EU worker node, --skip-node omits these)
#     7  drain-node         → EU worker evacuated (EU manager still serves)
#     8  block-firewall     → EU worker network blocked
#
# For US and APAC (single-node cells), Phase 3 is ALWAYS skipped.
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CHAOS_BIN="${SCRIPT_DIR}/chaos-monkey.sh"
ANALYZE_BIN="${SCRIPT_DIR}/analyze-chaos-results.py"

# ── Defaults ──────────────────────────────────────────────────────────────────
CELL="eu"
DURATION=60
PAUSE_BETWEEN=60
DRY_RUN=false
SKIP_NODE=false

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'

ts()   { date '+%H:%M:%S'; }
info() { echo -e "${GREEN}[$(ts)] SUITE  ${NC} $*"; }
warn() { echo -e "${YELLOW}[$(ts)] SUITE  ${NC} $*"; }
err()  { echo -e "${RED}[$(ts)] SUITE  ${NC} $*" >&2; }
sep()  { echo -e "${BOLD}$(printf '%.0s━' {1..64})${NC}"; }

# ── Argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cell)       CELL="$2";            shift 2 ;;
    --duration)   DURATION="$2";        shift 2 ;;
    --pause)      PAUSE_BETWEEN="$2";   shift 2 ;;
    --dry-run)    DRY_RUN=true;         shift ;;
    --skip-node)  SKIP_NODE=true;       shift ;;
    -h|--help)
      sed -n '3,/^# ====/p' "$0" | head -50 | sed 's/^# \?//'
      exit 0 ;;
    *) err "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Validate ──────────────────────────────────────────────────────────────────
[[ -f "$CHAOS_BIN" ]] || { err "chaos-monkey.sh not found at $CHAOS_BIN"; exit 1; }
chmod +x "$CHAOS_BIN"
cd "$PROJECT_ROOT"

# ── Cell topology ─────────────────────────────────────────────────────────────
declare -A CELL_URL=(
  [eu]="https://35.244.162.92.nip.io"
  [us]="https://35.227.198.68.nip.io"
  [apac]="https://34.8.134.246.nip.io"
)
declare -A CELL_WORKER=(
  [eu]="vcs-vm-eu2"       # EU has 2 nodes — eu2 is the worker, eu1 stays as manager
  [us]=""                  # US: single node — no drain/block experiments
  [apac]=""                # APAC: single node — no drain/block experiments
)
declare -A CELL_MANAGER=(
  [eu]="vcs-vm-eu1"
  [us]="vcs-vm-us1"
  [apac]="vcs-vm-ap1"
)

TARGET_VM="${CELL_MANAGER[$CELL]}"   # default: scale-service targets manager (or only) node
WORKER_VM="${CELL_WORKER[$CELL]:-}"  # empty for single-node cells

# EU: target the worker for service tests so the manager stays healthy as observer
[[ "$CELL" == "eu" && -n "$WORKER_VM" ]] && TARGET_VM="$WORKER_VM"

# ── Suite output directory ────────────────────────────────────────────────────
SUITE_TS=$(date '+%Y%m%dT%H%M%SZ')
SUITE_DIR="debug-artifacts/chaos/suite-${SUITE_TS}"
SUITE_MANIFEST="${SUITE_DIR}/manifest.csv"
SUITE_LOG="${SUITE_DIR}/suite.log"

# ── Experiment table ──────────────────────────────────────────────────────────
# Format: "phase|label|experiment_type|extra_flags"
# SERVICE_EXPERIMENTS are always run.
# NODE_EXPERIMENTS are run only when WORKER_VM is set and --skip-node is false.

SERVICE_EXPERIMENTS=(
  "1|capacity-service|scale-service|--service capacity-service"
  "1|map-service|scale-service|--service map-service"
  "1|iam-service|scale-service|--service iam-service"
  "1|notification-service|scale-service|--service notification-service"
  "2|redis|scale-service|--service redis"
  "2|db|scale-service|--service db"
)

NODE_EXPERIMENTS=(
  "3|drain-node|drain-node|"
  "3|block-firewall|block-firewall|"
)

# Build the final ordered list based on flags
EXPERIMENTS=()
for exp in "${SERVICE_EXPERIMENTS[@]}"; do
  EXPERIMENTS+=("$exp")
done
if ! $SKIP_NODE && [[ -n "$WORKER_VM" ]]; then
  for exp in "${NODE_EXPERIMENTS[@]}"; do
    EXPERIMENTS+=("$exp")
  done
else
  if $SKIP_NODE; then
    warn "Phase 3 (node-level) skipped — --skip-node flag set."
  elif [[ -z "$WORKER_VM" ]]; then
    warn "Phase 3 (node-level) skipped — $CELL cell has only one node; drain/block would cause total outage."
  fi
fi

TOTAL="${#EXPERIMENTS[@]}"

# ── Tracking ──────────────────────────────────────────────────────────────────
PASS=0; FAIL=0
declare -a RUN_DIRS=()   # paths to each chaos-monkey.sh report directory
declare -a RUN_LABELS=()
declare -a RUN_RESULTS=()

# ── Helper: health gate ───────────────────────────────────────────────────────
all_cells_healthy() {
  local ok=true
  for c in eu us apac; do
    local h
    h=$(curl -sf --max-time 5 "${CELL_URL[$c]}/nginx-health" &>/dev/null && echo "healthy" || echo "down")
    [[ "$h" != "healthy" ]] && ok=false
  done
  $ok
}

wait_for_suite_recovery() {
  local max="${1:-120}" elapsed=0
  info "Waiting up to ${max}s for all cells to return healthy..."
  while [[ $elapsed -lt $max ]]; do
    if all_cells_healthy; then
      info "All cells healthy after ${elapsed}s."
      return 0
    fi
    sleep 10
    elapsed=$((elapsed + 10))
    info "  Still recovering... ${elapsed}s"
  done
  warn "Not all cells healthy after ${max}s — continuing anyway."
  return 1
}

# Find the most recently created directory under debug-artifacts/chaos/
# that is NOT our own suite dir. Used to locate the report chaos-monkey.sh made.
find_latest_chaos_dir() {
  find debug-artifacts/chaos -mindepth 1 -maxdepth 1 -type d \
    ! -name "suite-*" \
    -newer "${SUITE_DIR}" \
    -printf '%T@ %p\n' 2>/dev/null \
    | sort -rn | head -1 | awk '{print $2}'
}

# ── Log tee ───────────────────────────────────────────────────────────────────
log_tee() { tee -a "$SUITE_LOG"; }

# ── Run a single experiment ───────────────────────────────────────────────────
run_experiment() {
  local num="$1" phase="$2" label="$3" exp_type="$4" extra_flags="$5"
  local vm; vm="$TARGET_VM"
  # drain-node and block-firewall target the worker, not the manager
  if [[ "$exp_type" == "drain-node" || "$exp_type" == "block-firewall" ]]; then
    vm="${WORKER_VM:-$TARGET_VM}"
  fi

  sep | log_tee
  info "[$num/$TOTAL] Phase ${phase} — ${BOLD}${label}${NC}  (${exp_type} → ${vm})" | log_tee
  sep | log_tee
  echo "" | log_tee

  # Touch a marker so find_latest_chaos_dir can locate the new directory
  local ts_before; ts_before=$(date '+%Y%m%dT%H%M%SZ')
  touch "${SUITE_DIR}/.before_${num}"

  local dry_flag=""
  $DRY_RUN && dry_flag="--dry-run"

  local exit_code=0
  # shellcheck disable=SC2086
  bash "$CHAOS_BIN" \
    --cell "$CELL" \
    --duration "$DURATION" \
    --recover 120 \
    --max-cell-fails 2 \
    $dry_flag \
    $extra_flags \
    "$exp_type" "$vm" 2>&1 | log_tee || exit_code=$?

  echo "" | log_tee

  # Locate report directory produced by this chaos-monkey.sh run
  local run_dir=""
  if ! $DRY_RUN; then
    run_dir=$(find debug-artifacts/chaos -mindepth 1 -maxdepth 1 -type d \
      ! -name "suite-*" \
      -newer "${SUITE_DIR}/.before_${num}" \
      -printf '%T@ %p\n' 2>/dev/null \
      | sort -rn | head -1 | awk '{print $2}')
  fi

  RUN_DIRS+=("${run_dir:-}")
  RUN_LABELS+=("$label")

  if [[ $exit_code -eq 0 ]]; then
    PASS=$((PASS + 1))
    RUN_RESULTS+=("PASS")
    info "[$num/$TOTAL] ${GREEN}PASSED${NC} — ${label}" | log_tee
  else
    FAIL=$((FAIL + 1))
    RUN_RESULTS+=("FAIL(${exit_code})")
    warn "[$num/$TOTAL] ${RED}FAILED (exit ${exit_code})${NC} — ${label}" | log_tee
  fi

  # Append to manifest
  printf '%s,%s,%s,%s,%s,%s\n' \
    "$num" "$phase" "$label" "$exp_type" "${run_dir:-none}" "${RUN_RESULTS[-1]}" \
    >> "$SUITE_MANIFEST"
}

# =============================================================================
# MAIN
# =============================================================================

# ── Suite header ──────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${RED}╔══════════════════════════════════════════════════════════════╗"
echo -e "║        VCS CHAOS MONKEY — FULL SUITE  (v3)                  ║"
echo -e "╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

mkdir -p "$SUITE_DIR"
echo "# VCS Chaos Suite Log — ${SUITE_TS}" > "$SUITE_LOG"

info "Suite directory  : ${SUITE_DIR}"
info "Cell             : ${BOLD}${CELL^^}${NC}"
info "Target VM        : ${BOLD}${TARGET_VM}${NC}"
[[ -n "$WORKER_VM" ]] && info "Worker VM (node) : ${BOLD}${WORKER_VM}${NC}"
info "Experiments      : ${BOLD}${TOTAL}${NC}"
info "Duration each    : ${BOLD}${DURATION}s${NC}"
info "Cool-down        : ${BOLD}${PAUSE_BETWEEN}s${NC}"
$DRY_RUN && warn "DRY-RUN MODE — no real changes will be made"
echo ""

# ── Print plan ────────────────────────────────────────────────────────────────
echo -e "${BOLD}Planned experiment sequence:${NC}"
printf "  %-4s %-8s %-24s %s\n" "Num" "Phase" "Label" "Experiment type"
printf "  %-4s %-8s %-24s %s\n" "───" "─────" "──────────────────────" "──────────────────"
idx=0
for exp_def in "${EXPERIMENTS[@]}"; do
  idx=$((idx + 1))
  IFS='|' read -r phase label exp_type extra_flags <<< "$exp_def"
  printf "  %-4s Phase%-3s %-24s %s\n" "$idx" "$phase" "$label" "$exp_type"
done
echo ""

# ── Confirmation ──────────────────────────────────────────────────────────────
if ! $DRY_RUN; then
  echo -e "${YELLOW}${BOLD}This will inject real failures into the live ${CELL^^} cell.${NC}"
  echo -e "${YELLOW}All experiments restore automatically. The suite cannot be interrupted"
  echo -e "mid-experiment without a partial restore — always let it complete.${NC}"
  echo ""
  read -r -p "  Type 'yes' to start, anything else to cancel: " confirm
  if [[ "$confirm" != "yes" ]]; then
    info "Cancelled."
    exit 0
  fi
  echo ""
fi

# ── Initialise manifest ───────────────────────────────────────────────────────
printf '%s\n' "num,phase,label,experiment_type,report_dir,result" > "$SUITE_MANIFEST"

# ── Pre-flight ────────────────────────────────────────────────────────────────
if ! $DRY_RUN; then
  sep
  info "PRE-FLIGHT — verifying all cells are healthy before starting"
  sep

  local_ok=true
  for c in eu us apac; do
    h=$(curl -sf --max-time 5 "${CELL_URL[$c]}/nginx-health" &>/dev/null && echo "healthy" || echo "UNREACHABLE")
    if [[ "$h" == "healthy" ]]; then
      info "  ${c}: ${GREEN}healthy${NC}"
    else
      warn "  ${c}: ${RED}${h}${NC}"
      local_ok=false
    fi
  done

  if ! $local_ok; then
    err "Not all cells healthy. Fix the cluster before running the suite."
    err "Re-run with --dry-run to preview without executing."
    exit 1
  fi
  info "All cells healthy — starting suite."
  echo ""
fi

# ── Run experiments ───────────────────────────────────────────────────────────
exp_num=0
for exp_def in "${EXPERIMENTS[@]}"; do
  exp_num=$((exp_num + 1))
  IFS='|' read -r phase label exp_type extra_flags <<< "$exp_def"

  run_experiment "$exp_num" "$phase" "$label" "$exp_type" "$extra_flags"

  # Cool-down + recovery check between experiments
  if [[ $exp_num -lt $TOTAL ]]; then
    echo "" | log_tee
    info "Cool-down: ${PAUSE_BETWEEN}s before next experiment..." | log_tee
    if ! $DRY_RUN; then
      sleep "$PAUSE_BETWEEN"
      wait_for_suite_recovery 120 | log_tee || true
    fi
    echo "" | log_tee
  fi
done

# ── Suite summary ─────────────────────────────────────────────────────────────
sep | log_tee
echo "" | log_tee
echo -e "${BOLD}CHAOS SUITE COMPLETE — $(date '+%Y-%m-%dT%H:%M:%S')${NC}" | log_tee
echo "" | log_tee
printf "  %-28s %s\n" "Total experiments:" "$TOTAL" | log_tee
printf "  %-28s ${GREEN}%s${NC}\n" "Passed:" "$PASS" | log_tee
printf "  %-28s ${RED}%s${NC}\n"   "Failed:" "$FAIL" | log_tee
echo "" | log_tee

info "Suite log     : ${SUITE_LOG}" | log_tee
info "Manifest      : ${SUITE_MANIFEST}" | log_tee
echo "" | log_tee

# ── Invoke the analyzer ───────────────────────────────────────────────────────
if [[ -f "$ANALYZE_BIN" ]] && ! $DRY_RUN; then
  info "Running analysis..." | log_tee
  python3 "$ANALYZE_BIN" \
    --suite-dir "$SUITE_DIR" \
    --manifest "$SUITE_MANIFEST" \
    2>&1 | log_tee || warn "Analyzer exited with errors — check output above."
else
  $DRY_RUN && info "[DRY-RUN] Would run analyzer: python3 ${ANALYZE_BIN} --suite-dir ${SUITE_DIR}"
  [[ ! -f "$ANALYZE_BIN" ]] && warn "Analyzer not found at ${ANALYZE_BIN} — skipping analysis."
fi

echo "" | log_tee
if [[ $FAIL -eq 0 ]]; then
  echo -e "${GREEN}${BOLD}ALL EXPERIMENTS PASSED${NC}" | log_tee
  exit 0
else
  echo -e "${YELLOW}${BOLD}${FAIL} EXPERIMENT(S) FAILED — review ${SUITE_LOG}${NC}" | log_tee
  exit 1
fi
