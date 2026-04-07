#!/usr/bin/env bash
# deploy-cell.sh — Deploy or update the VCS stack on a single cell.
#
# Run this from GCP Cloud Shell. It handles everything:
#   - Copies observability config files to all VMs in the cell
#   - Deploys the Docker stack
#   - Waits for CockroachDB to be healthy
#   - Initialises the cluster (idempotent — safe to re-run)
#   - Creates the postgres user and trafficservice database (idempotent)
#   - Force-restarts any services that hit max_attempts
#
# Usage:
#   ./scripts/deploy-cell.sh <manager_vm> <zone> <join_addrs> [worker_vm ...]
#
# Examples:
#   # EU cell (2 VMs, first arg is the Swarm manager):
#   ./scripts/deploy-cell.sh vcs-vm-eu1 europe-west1-b \
#     "35.187.121.12:26257,34.76.63.61:26257"           \
#     vcs-vm-eu2
#
#   # US cell (single VM):
#   ./scripts/deploy-cell.sh vcs-vm-us1 us-east1-b \
#     "35.187.121.12:26257,34.76.63.61:26257,<US_IP>:26257"
#
#   # APAC cell (single VM):
#   ./scripts/deploy-cell.sh vcs-vm-ap1 asia-east1-b \
#     "35.187.121.12:26257,34.76.63.61:26257,<US_IP>:26257,<AP_IP>:26257"

set -euo pipefail

# ── Args ────────────────────────────────────────────────────────────────────
MANAGER_VM="${1:?Usage: $0 <manager_vm> <zone> <join_addrs> [worker_vm ...]}"
ZONE="${2:?missing zone}"
CRDB_JOIN="${3:?missing join_addrs}"
shift 3
EXTRA_VMS=("$@")   # optional additional VMs in the same cell (workers)

PROJECT="distributed-capacity-system"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GITHUB_REPOSITORY="ajinkyataranekar/clearway"
IMAGE_TAG="latest"

ALL_VMS=("$MANAGER_VM" "${EXTRA_VMS[@]}")

# ── Colours ─────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()    { echo -e "${GREEN}[deploy]${NC} $*"; }
warning() { echo -e "${YELLOW}[warn]${NC}  $*"; }
error()   { echo -e "${RED}[error]${NC} $*"; exit 1; }

# ── Helper: run a command on a VM ───────────────────────────────────────────
remote() {
  local vm="$1"; shift
  gcloud compute ssh "$vm" --project="$PROJECT" --zone="$ZONE" \
    --command="$*" 2>/dev/null
}

# ── Step 1: Copy observability configs to all VMs ───────────────────────────
info "Step 1 — copying observability configs to all VMs"
for vm in "${ALL_VMS[@]}"; do
  info "  → $vm"
  remote "$vm" "sudo mkdir -p \
    /root/vcs/observability/grafana/provisioning/dashboards \
    /root/vcs/observability/grafana/provisioning/datasources \
    /root/vcs/observability/loki \
    /root/vcs/observability/promtail"

  gcloud compute scp \
    "$REPO_ROOT/observability/prometheus.yml" \
    "$REPO_ROOT/observability/loki/local-config.yaml" \
    "$REPO_ROOT/observability/promtail/config.yml" \
    "$REPO_ROOT/observability/grafana/provisioning/dashboards/dashboards.yml" \
    "$REPO_ROOT/observability/grafana/provisioning/dashboards/vcs-overview.json" \
    "$REPO_ROOT/observability/grafana/provisioning/datasources/datasources.yml" \
    "$vm":~/ --project="$PROJECT" --zone="$ZONE" 2>/dev/null

  remote "$vm" "
    sudo mv ~/prometheus.yml     /root/vcs/observability/prometheus.yml
    sudo mv ~/local-config.yaml  /root/vcs/observability/loki/local-config.yaml
    sudo mv ~/config.yml         /root/vcs/observability/promtail/config.yml
    sudo mv ~/dashboards.yml     /root/vcs/observability/grafana/provisioning/dashboards/dashboards.yml
    sudo mv ~/vcs-overview.json  /root/vcs/observability/grafana/provisioning/dashboards/vcs-overview.json
    sudo mv ~/datasources.yml    /root/vcs/observability/grafana/provisioning/datasources/datasources.yml
  "
done

# ── Step 2: Copy stack file to manager and deploy ───────────────────────────
info "Step 2 — copying stack file and deploying on $MANAGER_VM"
gcloud compute scp "$REPO_ROOT/docker-stack.yml" \
  "$MANAGER_VM":~/docker-stack.yml --project="$PROJECT" --zone="$ZONE" 2>/dev/null

remote "$MANAGER_VM" "
  sudo CRDB_JOIN='$CRDB_JOIN' \
       GITHUB_REPOSITORY='$GITHUB_REPOSITORY' \
       IMAGE_TAG='$IMAGE_TAG' \
    docker stack deploy -c ~/docker-stack.yml vcs 2>&1
"

# ── Step 3: Wait for CockroachDB to be healthy ──────────────────────────────
info "Step 3 — waiting for CockroachDB to be healthy (up to 3 min)"
CRDB_READY=false
for i in $(seq 1 36); do
  STATUS=$(remote "$MANAGER_VM" \
    "sudo docker service ps vcs_db --format '{{.CurrentState}}' 2>/dev/null | head -1")
  if echo "$STATUS" | grep -q "Running"; then
    info "  CockroachDB is Running (attempt $i)"
    CRDB_READY=true
    break
  fi
  warning "  attempt $i/36: $STATUS — waiting 5s"
  sleep 5
done
"$CRDB_READY" || error "CockroachDB did not start after 3 minutes. Check: gcloud compute ssh $MANAGER_VM --zone $ZONE -- 'sudo docker service logs vcs_db --tail 30'"

# Give the SQL port a few extra seconds to open after the container is Running
sleep 10

# ── Step 4: Initialise the cluster (idempotent) ─────────────────────────────
info "Step 4 — initialising CockroachDB cluster (idempotent)"
CRDB_CONTAINER=$(remote "$MANAGER_VM" \
  "sudo docker ps --filter name=vcs_db --format '{{.ID}}' | head -1")

INIT_OUT=$(remote "$MANAGER_VM" \
  "sudo docker exec $CRDB_CONTAINER /cockroach/cockroach init --insecure --host=localhost:26257 2>&1" || true)

if echo "$INIT_OUT" | grep -q "already initialized"; then
  info "  cluster already initialised — skipping"
elif echo "$INIT_OUT" | grep -q "successfully initialized"; then
  info "  cluster successfully initialised"
else
  warning "  init output: $INIT_OUT"
fi

# ── Step 5: Create database and postgres user (idempotent) ──────────────────
info "Step 5 — ensuring trafficservice database and postgres user exist"
remote "$MANAGER_VM" "
  sudo docker exec $CRDB_CONTAINER /cockroach/cockroach sql \
    --insecure --host=localhost:26257 --execute=\"
      CREATE DATABASE IF NOT EXISTS trafficservice;
      CREATE USER IF NOT EXISTS postgres;
      GRANT ALL ON DATABASE trafficservice TO postgres WITH GRANT OPTION;
    \" 2>&1
"

# ── Step 6: Force-restart any services stuck at 0 replicas ──────────────────
info "Step 6 — restarting any services that exhausted retries"
sleep 20
STUCK=$(remote "$MANAGER_VM" \
  "sudo docker service ls --format '{{.Name}} {{.Replicas}}' 2>/dev/null \
   | awk '\$2 ~ /^0\// {print \$1}'")

if [ -z "$STUCK" ]; then
  info "  all services have running replicas"
else
  for svc in $STUCK; do
    info "  force-restarting $svc"
    remote "$MANAGER_VM" "sudo docker service update --force --detach=true $svc 2>/dev/null" || true
  done
  info "  waiting 45s for restarted services"
  sleep 45
fi

# ── Step 7: Final status ─────────────────────────────────────────────────────
info "Step 7 — final service status"
remote "$MANAGER_VM" \
  "sudo docker service ls --format 'table {{.Name}}\t{{.Replicas}}\t{{.Image}}' 2>/dev/null"

info "Done. CockroachDB node status:"
remote "$MANAGER_VM" "
  CRDB_CONTAINER=\$(sudo docker ps --filter name=vcs_db --format '{{.ID}}' | head -1)
  sudo docker exec \$CRDB_CONTAINER /cockroach/cockroach node status \
    --insecure --host=localhost:26257 2>/dev/null || true
"
