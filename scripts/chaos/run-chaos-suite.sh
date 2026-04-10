#!/usr/bin/env bash
# =============================================================================
# run-chaos-suite.sh — Full GCP Swarm Chaos Suite for VCS
# =============================================================================
#
# Run from GCP Cloud Shell. Executes a planned sequence of chaos experiments
# against the live deployed stack, with health verification and cool-down
# between each experiment to allow the system to fully recover.
#
# Usage:
#   ./scripts/chaos/run-chaos-suite.sh [OPTIONS]
#
# Options:
#   --cell     eu|us|apac    Cell to test against (default: eu)
#   --dry-run                Preview all actions without executing
#   --duration SECONDS       Length of each experiment (default: 60)
#   --pause    SECONDS       Cool-down between experiments (default: 45)
#
# Examples:
#   # Full suite against EU cell (default)
#   ./scripts/chaos/run-chaos-suite.sh
#
#   # Preview the full suite without executing anything
#   ./scripts/chaos/run-chaos-suite.sh --dry-run
#
#   # Longer experiments against APAC cell
#   ./scripts/chaos/run-chaos-suite.sh --cell apac --duration 90 --pause 60
#
# Experiments in this suite (in order):
#   1. pause-container capacity-service  (vcs-vm-eu2) → trips journey CB
#   2. pause-container map-service       (vcs-vm-eu2) → trips journey CB
#   3. drain-node vcs-vm-eu2            → EU worker goes offline, EU LB still works
#   4. pause-container iam-service       (vcs-vm-eu1) → auth fails on eu1 only
#   5. block-firewall vcs-vm-eu1        → GCP denies HTTP to eu1 backend
#   (Adjust target VMs below if your topology differs)
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHAOS_BIN="$SCRIPT_DIR/chaos-monkey.sh"

# ── Defaults ──────────────────────────────────────────────────────────────────
CELL="eu"
DRY_RUN_FLAG=""
DURATION=60
PAUSE_BETWEEN=45

# ── Colours ───────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'
BOLD='\033[1m'; NC='\033[0m'
info()  { echo -e "${GREEN}[suite $(date '+%H:%M:%S')]${NC} $*"; }
warn()  { echo -e "${YELLOW}[suite $(date '+%H:%M:%S')]${NC} $*"; }
sep()   { echo -e "${BOLD}$(printf '%.0s━' {1..60})${NC}"; }

# ── Argument parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cell)     CELL="$2";         shift 2 ;;
    --dry-run)  DRY_RUN_FLAG="--dry-run"; shift ;;
    --duration) DURATION="$2";    shift 2 ;;
    --pause)    PAUSE_BETWEEN="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

chmod +x "$CHAOS_BIN"

# ── Experiment Definitions ────────────────────────────────────────────────────
# Format: "<experiment_type>|<target_vm>|<extra_flags>"
# For pause-container, extra_flags should include --service <svc>

# EU cell experiments (adjust VMs if your cell layout differs)
EU_EXPERIMENTS=(
  "pause-container|vcs-vm-eu2|--service capacity-service"
  "pause-container|vcs-vm-eu2|--service map-service"
  "drain-node|vcs-vm-eu2|"
  "pause-container|vcs-vm-eu1|--service iam-service"
  "block-firewall|vcs-vm-eu2|"
)

# US cell experiments (single node — only service-level chaos, no drain)
US_EXPERIMENTS=(
  "pause-container|vcs-vm-us1|--service capacity-service"
  "pause-container|vcs-vm-us1|--service map-service"
  "block-firewall|vcs-vm-us1|"
)

# APAC cell experiments (single node)
APAC_EXPERIMENTS=(
  "pause-container|vcs-vm-ap1|--service capacity-service"
  "pause-container|vcs-vm-ap1|--service map-service"
  "block-firewall|vcs-vm-ap1|"
)

# Select experiment list based on cell
case "$CELL" in
  eu)   EXPERIMENTS=("${EU_EXPERIMENTS[@]}") ;;
  us)   EXPERIMENTS=("${US_EXPERIMENTS[@]}") ;;
  apac) EXPERIMENTS=("${APAC_EXPERIMENTS[@]}") ;;
  *)    echo "Unknown cell: $CELL"; exit 1 ;;
esac

# ── Tracking ──────────────────────────────────────────────────────────────────
PASS=0; FAIL=0; SKIP=0
TOTAL="${#EXPERIMENTS[@]}"
REPORT_FILE="chaos-suite-report-$(date '+%Y%m%dT%H%M%S').txt"

# ── Health gate: check all cells before starting ──────────────────────────────
preflight_suite() {
  info "Suite pre-flight: verifying all cells are healthy..."
  declare -A CELL_URLS=(
    [eu]="https://35.244.162.92.nip.io"
    [us]="https://35.227.198.68.nip.io"
    [apac]="https://34.8.134.246.nip.io"
  )
  local all_ok=true
  for c in eu us apac; do
    local status
    status=$(curl -sf --max-time 5 "${CELL_URLS[$c]}/nginx-health" 2>/dev/null \
      && echo "healthy" || echo "UNREACHABLE")
    if [[ "$status" != "healthy" ]]; then
      warn "  Cell $c is $status — suite will still proceed but results may be inaccurate"
      all_ok=false
    else
      info "  Cell $c: OK"
    fi
  done
  $all_ok || warn "Not all cells were healthy at suite start. Review results carefully."
}

# ── Between-experiment health check ──────────────────────────────────────────
wait_for_recovery() {
  local max_wait=120
  local elapsed=0
  info "Recovery wait: up to ${max_wait}s for all cells to return healthy..."

  declare -A CELL_URLS=(
    [eu]="https://35.244.162.92.nip.io"
    [us]="https://35.227.198.68.nip.io"
    [apac]="https://34.8.134.246.nip.io"
  )

  while [[ $elapsed -lt $max_wait ]]; do
    local all_ok=true
    for c in eu us apac; do
      local s
      s=$(curl -sf --max-time 5 "${CELL_URLS[$c]}/nginx-health" 2>/dev/null \
        && echo "ok" || echo "fail")
      [[ "$s" != "ok" ]] && all_ok=false
    done

    if $all_ok; then
      info "All cells healthy after ${elapsed}s of recovery."
      return 0
    fi

    sleep 10
    elapsed=$((elapsed + 10))
    info "  Still recovering... ${elapsed}s elapsed"
  done

  warn "System did not fully recover within ${max_wait}s — continuing to next experiment anyway"
  return 1
}

# ── Run a single experiment ───────────────────────────────────────────────────
run_experiment() {
  local num="$1"
  local exp_type="$2"
  local target_vm="$3"
  local extra_flags="$4"

  sep
  info "Experiment ${num}/${TOTAL}: ${BOLD}$exp_type${NC} → ${BOLD}$target_vm${NC}"
  [[ -n "$extra_flags" ]] && info "  Extra flags: $extra_flags"
  sep

  local cmd="bash '$CHAOS_BIN' \
    --cell '$CELL' \
    --duration '$DURATION' \
    --recover 120 \
    $DRY_RUN_FLAG \
    $extra_flags \
    '$exp_type' '$target_vm'"

  info "Running: $cmd"
  echo ""

  if eval "$cmd"; then
    PASS=$((PASS + 1))
    info "Experiment ${num} — ${GREEN}PASSED${NC}"
  else
    FAIL=$((FAIL + 1))
    warn "Experiment ${num} — ${RED}FAILED${NC} (exit code $?)"
  fi

  echo "${num}/${TOTAL} | $exp_type | $target_vm | $(date '+%H:%M:%S')" >> "$REPORT_FILE"
}

# =============================================================================
# MAIN
# =============================================================================

echo ""
echo -e "${BOLD}${RED}╔══════════════════════════════════════════════════════════╗"
echo -e "║          VCS CHAOS MONKEY — FULL SUITE (GCP)            ║"
echo -e "╚══════════════════════════════════════════════════════════╝${NC}"
echo ""
info "Cell          : ${BOLD}${CELL^^}${NC}"
info "Experiments   : ${BOLD}$TOTAL${NC}"
info "Duration each : ${BOLD}${DURATION}s${NC}"
info "Cool-down     : ${BOLD}${PAUSE_BETWEEN}s${NC}"
info "Report file   : ${BOLD}$REPORT_FILE${NC}"
[[ -n "$DRY_RUN_FLAG" ]] && warn "DRY-RUN MODE — no real changes will be made"
echo ""

echo "VCS Chaos Suite — $(date '+%Y-%m-%dT%H:%M:%S')" > "$REPORT_FILE"
echo "Cell: $CELL | Experiments: $TOTAL" >> "$REPORT_FILE"
echo "$(printf '%.0s─' {1..60})" >> "$REPORT_FILE"

[[ -z "$DRY_RUN_FLAG" ]] && preflight_suite

# ── Run all experiments ───────────────────────────────────────────────────────
for i in "${!EXPERIMENTS[@]}"; do
  exp_def="${EXPERIMENTS[$i]}"
  IFS='|' read -r exp_type target_vm extra_flags <<< "$exp_def"

  run_experiment "$((i+1))" "$exp_type" "$target_vm" "$extra_flags"

  # Cool-down between experiments (not after the last one)
  if [[ $((i+1)) -lt $TOTAL ]]; then
    echo ""
    info "Cool-down: waiting ${PAUSE_BETWEEN}s before next experiment..."
    if [[ -z "$DRY_RUN_FLAG" ]]; then
      sleep "$PAUSE_BETWEEN"
      wait_for_recovery || true
    fi
    echo ""
  fi
done

# ── Final summary ─────────────────────────────────────────────────────────────
sep
echo ""
echo -e "${BOLD}CHAOS SUITE COMPLETE${NC}"
echo ""
printf "  %-20s %s\n" "Total experiments:" "$TOTAL"
printf "  %-20s ${GREEN}%s${NC}\n" "Passed:" "$PASS"
printf "  %-20s ${RED}%s${NC}\n" "Failed:" "$FAIL"
echo ""
info "Detailed report: $REPORT_FILE"
echo ""

if [[ $FAIL -eq 0 ]]; then
  echo -e "${GREEN}${BOLD}ALL EXPERIMENTS PASSED — System demonstrates solid fault tolerance${NC}"
  exit 0
else
  echo -e "${YELLOW}${BOLD}$FAIL EXPERIMENT(S) FAILED — Review output above${NC}"
  exit 1
fi
