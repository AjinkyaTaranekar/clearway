#!/usr/bin/env bash
# =============================================================================
# run-load-test.sh — K6 Load Test Runner for VCS
# =============================================================================
#
# Installs K6 if needed, then runs the selected load test scenario.
#
# Usage:
#   ./scripts/load-testing/run-load-test.sh [SCENARIO] [BASE_URL]
#
# Arguments:
#   SCENARIO   smoke|load|stress|soak  (default: smoke)
#   BASE_URL   Target base URL         (default: http://localhost)
#
# Examples:
#   # Smoke test against local Docker Compose
#   ./scripts/load-testing/run-load-test.sh smoke
#
#   # Load test against production EU cell
#   ./scripts/load-testing/run-load-test.sh load https://35.244.162.92.nip.io
#
#   # Stress test, output results to JSON
#   K6_OUT="json=results-stress.json" ./scripts/load-testing/run-load-test.sh stress
#
#   # All scenarios in sequence (CI mode)
#   for s in smoke load stress; do
#     ./scripts/load-testing/run-load-test.sh "$s"
#   done
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K6_TEST="$SCRIPT_DIR/k6-load-test.js"

SCENARIO="${1:-smoke}"
BASE_URL="${2:-http://localhost}"
K6_OUT="${K6_OUT:-}"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; BOLD='\033[1m'; NC='\033[0m'
info()  { echo -e "${GREEN}[k6]${NC} $*"; }
warn()  { echo -e "${YELLOW}[k6]${NC} $*"; }
error() { echo -e "${RED}[k6]${NC} $*" >&2; }

# ── Validate scenario ─────────────────────────────────────────────────────────
case "$SCENARIO" in
  smoke|load|stress|soak) ;;
  *)
    error "Unknown scenario: $SCENARIO"
    error "Valid scenarios: smoke, load, stress, soak"
    exit 1
    ;;
esac

# ── Check / install K6 ───────────────────────────────────────────────────────
if ! command -v k6 &>/dev/null; then
  warn "K6 not found. Attempting install..."
  if command -v brew &>/dev/null; then
    brew install k6
  elif command -v apt-get &>/dev/null; then
    sudo apt-get install -y gnupg
    curl -fsSL https://dl.k6.io/key.gpg | sudo gpg --dearmor -o /usr/share/keyrings/k6-archive-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
      | sudo tee /etc/apt/sources.list.d/k6.list
    sudo apt-get update && sudo apt-get install -y k6
  elif command -v choco &>/dev/null; then
    choco install k6 -y
  else
    error "Cannot auto-install K6. Install manually: https://k6.io/docs/getting-started/installation/"
    exit 1
  fi
fi

info "K6 version: $(k6 version | head -1)"

# ── Pre-flight: check that the target is reachable ────────────────────────────
info "Pre-flight: checking ${BASE_URL}/nginx-health ..."
if ! curl -sf --max-time 5 "${BASE_URL}/nginx-health" &>/dev/null; then
  warn "Gateway health check failed. Make sure the VCS stack is running."
  warn "For local dev: docker compose up"
  warn "Proceeding anyway — K6 will report the failures."
fi

# ── Build K6 command ──────────────────────────────────────────────────────────
K6_CMD="k6 run"

# Output format (JSON for CI/Grafana dashboards)
if [[ -n "$K6_OUT" ]]; then
  K6_CMD+=" --out $K6_OUT"
  info "Results will be written to: $K6_OUT"
fi

# Results summary file
RESULTS_DIR="${SCRIPT_DIR}/results"
mkdir -p "$RESULTS_DIR"
SUMMARY_FILE="${RESULTS_DIR}/summary-${SCENARIO}-$(date '+%Y%m%dT%H%M%S').json"
K6_CMD+=" --summary-export ${SUMMARY_FILE}"

# Environment variables for the test
K6_CMD+=" --env K6_SCENARIO=${SCENARIO}"
K6_CMD+=" --env BASE_URL=${BASE_URL}"

# Test file
K6_CMD+=" ${K6_TEST}"

# ── Print banner ──────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════════╗"
echo -e "║         VCS K6 LOAD TEST RUNNER                ║"
echo -e "╚══════════════════════════════════════════════════╝${NC}"
echo ""
info "Scenario  : ${BOLD}${SCENARIO^^}${NC}"
info "Target    : ${BOLD}$BASE_URL${NC}"
info "Results   : ${BOLD}$SUMMARY_FILE${NC}"
echo ""

# ── Run ───────────────────────────────────────────────────────────────────────
info "Running: $K6_CMD"
echo ""

eval "$K6_CMD"
EXIT_CODE=$?

echo ""
if [[ $EXIT_CODE -eq 0 ]]; then
  echo -e "${GREEN}${BOLD}LOAD TEST PASSED — All thresholds met${NC}"
  info "Summary written to: $SUMMARY_FILE"
else
  echo -e "${RED}${BOLD}LOAD TEST FAILED — One or more thresholds exceeded${NC}"
  info "Check summary: $SUMMARY_FILE"
fi

exit $EXIT_CODE
