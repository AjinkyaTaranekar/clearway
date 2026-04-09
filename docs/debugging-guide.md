# SRE Runbook — Distributed Vehicle Capacity System

Quick reference for operating, debugging, and scaling the VCS stack on GCP Docker Swarm.

---

## Quick Reference

### VM Inventory

| VM | Zone | Internal IP | External IP | Role |
|---|---|---|---|---|
| vcs-vm-eu1 | europe-west1-b | 10.0.1.11 | 35.187.121.12 | EU Swarm manager |
| vcs-vm-eu2 | europe-west1-b | 10.0.1.12 | 34.76.63.61 | EU Swarm worker |
| vcs-vm-us1 | us-east1-d | — | 34.138.242.217 | US Swarm manager |
| vcs-vm-ap1 | asia-east1-b | — | 34.80.180.64 | APAC Swarm manager |

### Public HTTPS Endpoints

| Region | App | Grafana | CockroachDB UI |
|---|---|---|---|
| EU | https://35.244.162.92.nip.io | http://35.187.121.12:3000 | http://35.187.121.12:8080 |
| US | https://35.227.198.68.nip.io | http://34.138.242.217:3000 | http://34.138.242.217:8080 |
| APAC | https://34.8.134.246.nip.io | http://34.80.180.64:3000 | http://34.80.180.64:8080 |

### Service Port Map

| Service | Internal Port | Path prefix |
|---|---|---|
| nginx (gateway) | 80 | `/` |
| iam-service | 8082 | `/api/v1/auth/` |
| journey-service | 8083 | `/api/v1/journeys`, `/api/v1/admin/`, `/api/v1/enforcement/` |
| capacity-service | 8081 | `/api/v1/capacity/` |
| map-service | 8084 | `/api/v1/map/`, `/api/v1/routes/` |
| notification-service | 8085 | `/api/v1/notifications`, `/api/v1/admin/notifications` |
| CockroachDB SQL | 26257 | — |
| CockroachDB UI | 8080 | — |
| Prometheus | 9090 | — |
| Grafana | 3000 | — |
| Loki | 3100 | — |

---

## 1. SSH into a VM

```bash
# EU manager (most operations happen here)
gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b

# EU worker
gcloud compute ssh vcs-vm-eu2 --project=distributed-capacity-system --zone=europe-west1-b

# US manager
gcloud compute ssh vcs-vm-us1 --project=distributed-capacity-system --zone=us-east1-d

# APAC manager
gcloud compute ssh vcs-vm-ap1 --project=distributed-capacity-system --zone=asia-east1-b

# Run a one-liner without interactive shell
gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b \
  --command="sudo docker service ls"
```

---

## 2. Service Status

```bash
# All services — shows replicas, image, ports
sudo docker service ls

# One service in detail (task history, failure reasons, node placement)
sudo docker service ps vcs_journey-service --no-trunc

# Only failed/stopped tasks
sudo docker service ps vcs_journey-service --filter desired-state=shutdown --no-trunc

# Full inspect (env vars, mounts, constraints, update config)
sudo docker service inspect vcs_journey-service --pretty

# Running containers on this node only
sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Image}}"
```

---

## 3. Reading Logs

```bash
# Last N lines from a service (all replicas merged)
sudo docker service logs vcs_journey-service --tail 50
sudo docker service logs vcs_journey-service --tail 50 --timestamps

# Follow in real time
sudo docker service logs vcs_journey-service --follow

# Filter to errors only
sudo docker service logs vcs_journey-service --tail 200 --timestamps 2>&1 \
  | grep '"level":"error"'

# From a specific replica (useful when two nodes log different things)
sudo docker service logs vcs_journey-service --tail 100 2>&1 | grep "vcs-vm-eu2"

# Logs from all services at once (pipe to less)
for svc in iam-service journey-service capacity-service map-service notification-service nginx; do
  echo "=== $svc ===" 
  sudo docker service logs vcs_${svc} --tail 20 --timestamps 2>&1 | grep -E 'error|warn|fatal' || true
done

# From a specific container directly
CONTAINER=$(sudo docker ps --filter name=vcs_journey --format "{{.ID}}" | head -1)
sudo docker logs $CONTAINER --tail 50
sudo docker logs $CONTAINER --follow
sudo docker logs $CONTAINER --since 10m   # last 10 minutes
```

---

## 4. CPU and Memory Usage

```bash
# Live resource usage for all running containers (like top, refreshes every 1s)
sudo docker stats

# Non-streaming snapshot (good for scripting)
sudo docker stats --no-stream --format \
  "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}"

# Filter to VCS containers only
sudo docker stats --no-stream --format \
  "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}" \
  $(sudo docker ps --filter name=vcs_ --format "{{.ID}}")

# Host-level CPU and memory
top -bn1 | head -15
free -h
df -h

# Disk usage by Docker (images, containers, volumes, cache)
sudo docker system df -v

# Which containers use the most disk (logs)
sudo du -sh /var/lib/docker/containers/*/  2>/dev/null | sort -rh | head -10
```

---

## 5. Scaling Replicas Up / Down

> **Note:** `journey-service`, `map-service`, `capacity-service`, `iam-service`, and `notification-service` run in `global` mode (one replica per Swarm node). You can't scale them with `--replicas`. To scale global services, add/remove nodes from the Swarm.
>
> `nginx`, `prometheus`, `grafana`, `loki` run in `replicated` mode and can be scaled freely.

```bash
# Scale a replicated service
sudo docker service update --replicas 2 vcs_nginx
sudo docker service update --replicas 1 vcs_grafana

# Temporarily remove a global service from a specific node
# (useful to drain a node for maintenance)
sudo docker node update --availability drain vcs-vm-eu2

# Bring the node back
sudo docker node update --availability active vcs-vm-eu2

# Check node availability
sudo docker node ls

# Force restart a stuck service (re-creates all tasks)
sudo docker service update --force vcs_journey-service

# Roll back a service to its previous image
sudo docker service rollback vcs_journey-service

# Pin a service to a specific image SHA
sudo docker service update \
  --image ghcr.io/ajinkyataranekar/clearway/journey-service:254cdb3 \
  --with-registry-auth \
  vcs_journey-service
```

---

## 6. Deploying / Redeploying the Stack

```bash
# EU — full redeploy (picks up docker-stack.yml changes, prunes removed services)
sudo GITHUB_REPOSITORY=ajinkyataranekar/clearway \
     IMAGE_TAG=latest \
     CRDB_JOIN="10.0.1.11:26257,10.0.1.12:26257" \
     SWAGGER_PUBLIC_BASE_URL=https://35.244.162.92.nip.io \
  docker stack deploy --with-registry-auth --prune -c ~/docker-stack.yml vcs

# US
sudo GITHUB_REPOSITORY=ajinkyataranekar/clearway \
     IMAGE_TAG=latest \
     CRDB_JOIN="localhost:26257" \
     SWAGGER_PUBLIC_BASE_URL=https://35.227.198.68.nip.io \
  docker stack deploy --with-registry-auth --prune -c ~/docker-stack.yml vcs

# APAC
sudo GITHUB_REPOSITORY=ajinkyataranekar/clearway \
     IMAGE_TAG=latest \
     CRDB_JOIN="localhost:26257" \
     SWAGGER_PUBLIC_BASE_URL=https://34.8.134.246.nip.io \
  docker stack deploy --with-registry-auth --prune -c ~/docker-stack.yml vcs

# Update a single service to a specific commit SHA
sudo docker service update \
  --image ghcr.io/ajinkyataranekar/clearway/map-service:1fcd8da \
  --with-registry-auth \
  vcs_map-service

# Remove the entire stack (DESTRUCTIVE — stops all services)
sudo docker stack rm vcs
```

---

## 7. CockroachDB

```bash
CRDB=$(sudo docker ps --filter name=vcs_db --format "{{.ID}}" | head -1)

# Interactive SQL shell
sudo docker exec -it $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice

# One-liner query
sudo docker exec $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice \
  --execute="SELECT count(*) FROM journey.journeys;"

# Applied migrations
sudo docker exec $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice \
  --execute="SELECT name, applied_at FROM public.schema_migrations ORDER BY applied_at;"

# Active sessions / running queries
sudo docker exec $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 \
  --execute="SELECT * FROM crdb_internal.cluster_queries;"

# Table sizes
sudo docker exec $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice \
  --execute="SELECT table_name, total_bytes FROM crdb_internal.table_span_stats WHERE database_name='trafficservice' ORDER BY total_bytes DESC;"

# Node status
sudo docker exec $CRDB /cockroach/cockroach node status \
  --insecure --host=localhost:26257

# Pipe a SQL migration file in
sudo docker exec -i $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice \
  < ~/vcs/migrations/capacity-service/004_dynamic_segment_defaults.sql
```

---

## 8. Swarm Secrets

```bash
# List all secrets
sudo docker secret ls

# Read a secret from inside a running container (mounted at /run/secrets/)
CONTAINER=$(sudo docker ps --filter name=vcs_iam --format "{{.ID}}" | head -1)
sudo docker exec $CONTAINER cat /run/secrets/jwt_secret

# Read the Firebase service account JSON
CONTAINER=$(sudo docker ps --filter name=vcs_notification --format "{{.ID}}" | head -1)
sudo docker exec $CONTAINER cat /run/secrets/firebase_credentials | python3 -m json.tool | head -5

# Rotate a secret (must remove service first — secrets in use can't be deleted)
sudo docker service rm vcs_notification-service
sudo docker secret rm firebase_credentials
cat /path/to/new-service-account.json | sudo docker secret create firebase_credentials -
# Then redeploy the stack to recreate the service with the new secret
sudo GITHUB_REPOSITORY=ajinkyataranekar/clearway IMAGE_TAG=latest CRDB_JOIN=... \
  docker stack deploy --with-registry-auth -c ~/docker-stack.yml vcs
```

---

## 9. Networking and Connectivity

```bash
# List overlay networks
sudo docker network ls | grep vcs

# Check if a service DNS name resolves from within the overlay network
sudo docker run --rm --network vcs_vcs-internal busybox nslookup map-service
sudo docker run --rm --network vcs_vcs-internal busybox nslookup capacity-service

# Hit a service endpoint from within the overlay (bypass nginx)
sudo docker run --rm --network vcs_vcs-internal busybox \
  wget -qO- http://iam-service:8082/health

sudo docker run --rm --network vcs_vcs-internal busybox \
  wget -qO- http://map-service:8084/api/v1/map/nodes

# Test nginx health from the host
curl -sf http://localhost/nginx-health

# Test HTTPS endpoint
curl -I https://35.244.162.92.nip.io/nginx-health

# Test that HTTP redirects to HTTPS
curl -I http://35.244.162.92.nip.io/nginx-health
# Expect: 301 Moved Permanently → https://...

# Check which port is listening on the host
sudo ss -tlnp | grep -E '80|443|8080|9090|3000|26257'
```

---

## 10. Diagnosing a Failing Service

**Workflow when a service shows `0/N` or keeps restarting:**

```bash
# Step 1 — find the error message
sudo docker service ps vcs_journey-service --no-trunc | grep -i shutdown

# Step 2 — read the logs
sudo docker service logs vcs_journey-service --tail 50 --timestamps

# Step 3 — check the image can be pulled (GHCR auth issue?)
echo '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' | \
  sudo docker login ghcr.io -u AjinkyaTaranekar --password-stdin
sudo docker pull ghcr.io/ajinkyataranekar/clearway/journey-service:latest

# Step 4 — run the image manually to reproduce the crash
sudo docker run --rm \
  --network vcs_vcs-internal \
  -e VCS_DATABASE_MASTER_HOST=db \
  -e VCS_DATABASE_MASTER_PORT=26257 \
  -e VCS_DATABASE_MASTER_USER=postgres \
  -e VCS_DATABASE_MASTER_DBNAME=trafficservice \
  -e VCS_SERVER_PORT=8083 \
  ghcr.io/ajinkyataranekar/clearway/journey-service:latest

# Step 5 — force restart
sudo docker service update --force vcs_journey-service

# Step 6 — roll back to the last known-good image
sudo docker service rollback vcs_journey-service
```

**Common failure patterns:**

| Symptom | Likely cause | Fix |
|---|---|---|
| `No such image` on worker node | GHCR auth not set on that node | `docker login ghcr.io` on the worker |
| `connection refused` to db | CockroachDB not started yet | Wait or `docker service update --force vcs_db` |
| `pq: password authentication failed` | Wrong DB user/pass env | Check `VCS_DATABASE_MASTER_USER` in stack |
| Migration fails on start | Duplicate migration or schema mismatch | Check `schema_migrations` table, see migration logs |
| Service restarts every 10s | OOM kill | Increase memory limit in `docker-stack.yml` or reduce load |
| `secret not found` | Secret not created in this Swarm | Run `docker secret create` on the manager |

---

## 11. Observability Stack

### Grafana

```bash
# Access: http://<vm-ip>:3000  (admin / admin by default)
# EU: http://35.187.121.12:3000
# US: http://34.138.242.217:3000
# APAC: http://34.80.180.64:3000

# Restart Grafana
sudo docker service update --force vcs_grafana
```

### Prometheus

```bash
# Access: http://<vm-ip>:9090
# Check scrape targets (green = healthy)
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep -E '"health"|"job"'

# Query CPU usage across all containers
curl -s 'http://localhost:9090/api/v1/query?query=rate(container_cpu_usage_seconds_total[5m])' \
  | python3 -m json.tool | grep -E '"value"|"name"' | head -20

# Query memory
curl -s 'http://localhost:9090/api/v1/query?query=container_memory_usage_bytes' \
  | python3 -m json.tool | grep '"value"' | head -10
```

### Loki / Log querying

```bash
# Loki runs internally on port 3100; query via Grafana Explore tab
# Or use logcli if installed:
logcli query '{container_name=~"vcs_.*"}' --limit=50

# Restart Loki
sudo docker service update --force vcs_loki
```

---

## 12. GCP Load Balancer (HTTPS)

```bash
# Check TLS cert status across all regions
for cert in vcs-nip-cert-eu vcs-nip-cert-us vcs-nip-cert-ap; do
  echo -n "$cert: "
  gcloud compute ssl-certificates describe $cert \
    --global --project=distributed-capacity-system \
    --format="get(managed.status)"
done

# Check backend health
for svc in vcs-backend-eu vcs-backend-us vcs-backend-ap; do
  echo "=== $svc ==="
  gcloud compute backend-services get-health $svc \
    --global --project=distributed-capacity-system \
    --format="yaml(status.healthStatus)" 2>&1 | grep -E "healthState|ipAddress"
done

# List all global forwarding rules
gcloud compute forwarding-rules list --global --project=distributed-capacity-system \
  --format="table(name,IPAddress,target,portRange)"

# Force-update a backend service (picks up new instance group membership)
gcloud compute backend-services update vcs-backend-eu \
  --global --project=distributed-capacity-system

# View LB access logs (last 50 HTTPS requests to EU)
gcloud logging read \
  'resource.type="http_load_balancer" AND resource.labels.forwarding_rule_name="vcs-fwd-https-eu"' \
  --project=distributed-capacity-system --limit=50 --format=json \
  | python3 -m json.tool | grep -E '"requestUrl"|"status"|"latency"'
```

---

## 13. VM Management

```bash
# List all VMs and status
gcloud compute instances list --project=distributed-capacity-system \
  --format="table(name,zone,status,networkInterfaces[0].accessConfigs[0].natIP)"

# Start / stop a VM
gcloud compute instances start vcs-vm-eu2 --zone=europe-west1-b --project=distributed-capacity-system
gcloud compute instances stop vcs-vm-eu2 --zone=europe-west1-b --project=distributed-capacity-system

# Check instance resource usage (serial port / startup logs)
gcloud compute instances get-serial-port-output vcs-vm-eu1 \
  --zone=europe-west1-b --project=distributed-capacity-system | tail -30

# SSH tunnel to access an internal port locally
# e.g. bring CockroachDB UI to localhost:8080
gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b \
  -- -L 8080:localhost:8080 -N

# Copy a file to/from a VM
gcloud compute scp ./service-account.json vcs-vm-eu1:/tmp/ \
  --zone=europe-west1-b --project=distributed-capacity-system
gcloud compute scp vcs-vm-eu1:/tmp/some-log.txt ./ \
  --zone=europe-west1-b --project=distributed-capacity-system
```

---

## 14. Common Operational Tasks

### Rotate the JWT secret

```bash
# 1. Generate a new secret
NEW_SECRET=$(openssl rand -hex 32)

# 2. On each Swarm manager (EU, US, APAC):
#    Services must be stopped first — the secret is in use
sudo docker service update --env-add JWT_SECRET_OVERRIDE=$NEW_SECRET vcs_iam-service
# Or go the proper route: scale down, rm secret, recreate, redeploy
```

### Clear a stuck migration

```bash
CRDB=$(sudo docker ps --filter name=vcs_db --format "{{.ID}}" | head -1)
# Delete the migration record so it re-runs on next service start
sudo docker exec $CRDB /cockroach/cockroach sql \
  --insecure --host=localhost:26257 --database=trafficservice \
  --execute="DELETE FROM public.schema_migrations WHERE name='capacity-service/004_dynamic_segment_defaults.sql';"
```

### Re-register OSRM route segments after data wipe

```bash
# The map service auto-registers segments when ComputeRoute is called.
# To force a refresh, just hit the endpoint:
curl -X POST https://35.244.162.92.nip.io/api/v1/routes/compute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"origin":{"lat":53.3498,"lng":-6.2603},"destination":{"lat":51.8985,"lng":-8.4756}}'
```

### Purge Docker build cache on a VM (free disk)

```bash
# Remove unused images, stopped containers, unused networks, build cache
sudo docker system prune -af

# Also remove unused volumes (careful — check first)
sudo docker volume ls -f dangling=true
sudo docker volume prune -f
```

### Check notification service Firebase config

```bash
CONTAINER=$(sudo docker ps --filter name=vcs_notification --format "{{.ID}}" | head -1)

# Confirm credentials file is mounted and valid
sudo docker exec $CONTAINER cat /run/secrets/firebase_credentials \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('project:', d['project_id'], '| type:', d['type'])"

# Check the FCM_SERVICE_ACCOUNT_JSON env var (alternative auth path)
sudo docker exec $CONTAINER env | grep FCM
```

---

## 15. Health Check One-Liner (all regions)

Run this from Cloud Shell or any machine to get a quick status across all three regions:

```bash
for region_ip in "EU:35.244.162.92.nip.io" "US:35.227.198.68.nip.io" "APAC:34.8.134.246.nip.io"; do
  region="${region_ip%%:*}"; host="${region_ip##*:}"
  code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://${host}/nginx-health)
  echo "$region ($host): HTTP $code"
done
```
