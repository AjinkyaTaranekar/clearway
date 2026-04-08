#!/usr/bin/env bash
# collect-regional-service-logs.sh — Collect per-region logs for selected app services from GCP Swarm cells.
#
# Usage:
#   ./scripts/collect-regional-service-logs.sh [--output-dir <dir>] [--tail <lines>] [--include-eu2]
#
# Examples:
#   ./scripts/collect-regional-service-logs.sh
#   ./scripts/collect-regional-service-logs.sh --output-dir ./debug-artifacts --tail 400
#   ./scripts/collect-regional-service-logs.sh --include-eu2

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  collect-regional-service-logs.sh [--output-dir <dir>] [--tail <lines>] [--include-eu2]

Options:
  --output-dir <dir>   Base directory for collected logs (default: ./debug-artifacts)
  --tail <lines>       Number of recent lines to fetch per service (default: 300)
  --include-eu2        Also collect directly from vcs-vm-eu2 (optional; EU manager already sees EU replicas)
  -h, --help           Show this help

Environment variables:
  PROJECT              GCP project (default: distributed-capacity-system)
  LOG_TIMEOUT_SECONDS  Timeout per remote command (default: 45)

Notes:
  This script only collects services matching:
  journey, iam, capacity, map, notification, nginx
EOF
}

PROJECT="${PROJECT:-distributed-capacity-system}"
OUTPUT_DIR="./debug-artifacts"
TAIL_LINES=300
INCLUDE_EU2=false
LOG_TIMEOUT_SECONDS="${LOG_TIMEOUT_SECONDS:-45}"
TARGET_SERVICE_REGEX='(^|[_-])(journey|iam|capacity|map|notification|nginx)([_-]|$)'
TARGET_SERVICE_LABELS='journey, iam, capacity, map, notification, nginx'

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    --tail)
      TAIL_LINES="${2:?missing value for --tail}"
      shift 2
      ;;
    --include-eu2)
      INCLUDE_EU2=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'
info() { echo -e "${GREEN}[collect]${NC} $*"; }
warn() { echo -e "${YELLOW}[warn]${NC}    $*" >&2; }
err() { echo -e "${RED}[error]${NC}   $*" >&2; exit 1; }

command -v gcloud >/dev/null 2>&1 || err "gcloud is not installed"
command -v timeout >/dev/null 2>&1 || err "timeout is required"

if ! [[ "$TAIL_LINES" =~ ^[0-9]+$ ]]; then
  err "--tail must be a positive integer"
fi
if ! [[ "$LOG_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]]; then
  err "LOG_TIMEOUT_SECONDS must be a positive integer"
fi

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="${OUTPUT_DIR%/}/logs-${TIMESTAMP}"
mkdir -p "$RUN_DIR"
SUMMARY_FILE="$RUN_DIR/SUMMARY.md"

cat > "$SUMMARY_FILE" <<EOF
# Regional Service Log Collection

- **Timestamp (UTC):** $TIMESTAMP
- **GCP project:** $PROJECT
- **Tail lines per service:** $TAIL_LINES
- **Per-command timeout:** ${LOG_TIMEOUT_SECONDS}s
- **Target services:** $TARGET_SERVICE_LABELS

| Region | VM | Service | Task error rows | Error log lines |
|---|---|---|---:|---:|
EOF

REGIONS=(
  "eu:vcs-vm-eu1:europe-west1-b"
  "us:vcs-vm-us1:us-east1-d"
  "apac:vcs-vm-ap1:asia-east1-b"
)

if [[ "$INCLUDE_EU2" == "true" ]]; then
  REGIONS+=("eu2:vcs-vm-eu2:europe-west1-b")
fi

remote() {
  local vm="$1"
  local zone="$2"
  local cmd="$3"
  timeout "$LOG_TIMEOUT_SECONDS" gcloud compute ssh "$vm" \
    --project="$PROJECT" \
    --zone="$zone" \
    --quiet \
    --command="$cmd" \
    < /dev/null
}

for entry in "${REGIONS[@]}"; do
  IFS=':' read -r region vm zone <<<"$entry"
  region_dir="$RUN_DIR/$region"
  mkdir -p "$region_dir"

  info "Collecting target services from $region ($vm/$zone)"
  if ! all_services="$(remote "$vm" "$zone" "sudo docker service ls --format '{{.Name}}'")"; then
    warn "Failed to list services for $region ($vm)"
    {
      echo
      echo "## Failed regions"
      echo "- ${region} (${vm}/${zone}) — could not list services"
    } >> "$SUMMARY_FILE"
    continue
  fi

  services="$(printf "%s\n" "$all_services" | grep -E "$TARGET_SERVICE_REGEX" || true)"
  if [[ -z "$services" ]]; then
    warn "No matching target services found in $region ($vm)"
    : > "$region_dir/services.txt"
    continue
  fi

  printf "%s\n" "$services" > "$region_dir/services.txt"
  mapfile -t target_services < "$region_dir/services.txt"

  for svc in "${target_services[@]}"; do
    [[ -z "$svc" ]] && continue
    svc_dir="$region_dir/$svc"
    mkdir -p "$svc_dir"

    info "  ${region}/${svc}"

    ps_file="$svc_dir/service-ps.txt"
    task_errors_file="$svc_dir/task-errors.txt"
    logs_file="$svc_dir/logs-tail.txt"
    log_errors_file="$svc_dir/error-lines.txt"

    if ! remote "$vm" "$zone" "sudo docker service ps $svc --no-trunc --format '{{.Name}}|{{.Node}}|{{.DesiredState}}|{{.CurrentState}}|{{.Error}}'" > "$ps_file"; then
      warn "    service ps failed for ${region}/${svc}"
      : > "$ps_file"
    fi

    awk -F'|' 'NF >= 5 && $5 != "" {print}' "$ps_file" > "$task_errors_file" || true

    if ! remote "$vm" "$zone" "sudo docker service logs $svc --tail $TAIL_LINES --timestamps 2>&1" > "$logs_file"; then
      warn "    service logs failed/timed out for ${region}/${svc}"
      {
        echo "[log collection failed or timed out]"
        echo "region=$region vm=$vm service=$svc"
      } > "$logs_file"
    fi

    grep -Eai 'level.?=error|"level":"error"|panic|fatal|exception|connection refused|no such host|unauthorized|permission denied|timeout| failed |relation ".*" does not exist' "$logs_file" > "$log_errors_file" || true

    task_error_count=$(wc -l < "$task_errors_file")
    log_error_count=$(wc -l < "$log_errors_file")
    printf "| %s | %s | %s | %s | %s |\n" \
      "$region" "$vm" "$svc" "$task_error_count" "$log_error_count" >> "$SUMMARY_FILE"
  done
done

info "Done. Output directory: $RUN_DIR"
info "Summary file: $SUMMARY_FILE"
