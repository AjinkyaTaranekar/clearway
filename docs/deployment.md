# VCS Deployment Guide

## Architecture

3 Azure B1s VMs (1 vCPU / 1 GB RAM each) in the same region (Ireland).  
Docker Swarm: 1 manager node + 2 worker nodes.  
Each VM runs one replica of every service (`mode: global`).  
PostgreSQL instances replicate data to each other via logical replication.  
Redis is local to each node — no cross-node sync.

```
Internet → Azure Load Balancer (TCP 80/443)
               │
    ┌──────────┼──────────┐
    VM-A       VM-B       VM-C     (each runs all services)
    nginx      nginx      nginx
    iam        iam        iam
    journey    journey    journey
    capacity   capacity   capacity
    map        map        map
    notif      notif      notif
    postgres ←→ postgres ←→ postgres  (logical replication)
    redis      redis      redis        (local per-VM)
```

---

## First-time Swarm setup

```bash
# On VM-A (becomes the manager)
docker swarm init --advertise-addr <VM-A-PRIVATE-IP>

# Copy the join token output, then on VM-B and VM-C:
docker swarm join --token <TOKEN> <VM-A-PRIVATE-IP>:2377

# Verify on VM-A
docker node ls
```

---

## Create Docker secrets

Run on the Swarm manager (VM-A):

```bash
echo "your-strong-db-password" | docker secret create db_password -
echo "your-jwt-secret-32chars" | docker secret create jwt_secret -
cat /path/to/firebase-service-account.json | docker secret create firebase_credentials -
```

---

## Create Docker configs

```bash
docker config create nginx_conf ./nginx/nginx.conf
```

---

## GHCR authentication on Swarm nodes

Each node needs to be able to pull from GitHub Container Registry:

```bash
# On each node (or use --with-registry-auth in deploy command)
echo $GITHUB_PAT | docker login ghcr.io -u <github-username> --password-stdin
```

---

## Deploy the stack

```bash
export GITHUB_REPOSITORY=ajinkyataranekar/distributed-vehicle-capacity-system
export IMAGE_TAG=latest

docker stack deploy \
  --with-registry-auth \
  --prune \
  -c docker-stack.yml \
  vcs
```

---

## PostgreSQL logical replication setup

Run once after all VMs are up. Execute on each VM to set up cross-node replication:

```bash
# Connect to postgres on each VM:
docker exec -it $(docker ps -q -f name=vcs_db) psql -U postgres trafficservice
```

```sql
-- On EVERY node: create a publication
CREATE PUBLICATION vcs_pub FOR ALL TABLES;

-- On VM-B and VM-C: subscribe to VM-A
CREATE SUBSCRIPTION vcs_sub_vm_a
  CONNECTION 'host=<VM-A-IP> port=5432 dbname=trafficservice user=replicator password=<pw>'
  PUBLICATION vcs_pub;

-- On VM-A and VM-C: subscribe to VM-B
CREATE SUBSCRIPTION vcs_sub_vm_b
  CONNECTION 'host=<VM-B-IP> port=5432 dbname=trafficservice user=replicator password=<pw>'
  PUBLICATION vcs_pub;

-- On VM-A and VM-B: subscribe to VM-C
CREATE SUBSCRIPTION vcs_sub_vm_c
  CONNECTION 'host=<VM-C-IP> port=5432 dbname=trafficservice user=replicator password=<pw>'
  PUBLICATION vcs_pub;
```

Monitor replication lag:
```sql
SELECT * FROM pg_stat_replication;
-- Alert if write_lag > interval '1 second'
```

---

## Redis eviction policy

Redis is configured via the stack file with:
```
--maxmemory 96mb --maxmemory-policy allkeys-lru
```

This means when Redis reaches 96 MB, least-recently-used keys are evicted first. Route cache entries (24h TTL) are naturally recycled without manual intervention.

---

## Updating services (rolling deploy)

The CD pipeline handles this automatically on every push to `main`.  
Manual update:

```bash
export IMAGE_TAG=<new-sha>
docker stack deploy --with-registry-auth -c docker-stack.yml vcs
```

Docker Swarm performs rolling updates: `parallelism: 1`, `order: start-first` — the new container is started and health-checked before the old one is stopped. Zero-downtime.

---

## Useful operational commands

```bash
# List all services and replica counts
docker service ls

# Check a specific service's tasks across nodes
docker service ps vcs_journey-service

# Tail logs from any journey-service replica
docker service logs -f vcs_journey-service

# Force redeploy a single service without changing image
docker service update --force vcs_journey-service

# Scale a service (override global mode for testing)
# Note: global mode services cannot be scaled — they always run on every node

# Drain a node for maintenance
docker node update --availability drain <node-id>
# Restore
docker node update --availability active <node-id>

# Remove the entire stack
docker stack rm vcs
```

---

## Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `SWARM_HOST` | IP/hostname of the Swarm manager |
| `SWARM_SSH_KEY` | Private SSH key for `deploy` user on VMs |
| `SWARM_KNOWN_HOSTS` | Output of `ssh-keyscan <SWARM_HOST>` |

The `GITHUB_TOKEN` secret is automatic and used for GHCR pushes.

---

## Local development

```bash
# Start everything locally (single node, no replication)
docker compose up --build

# API available at http://localhost
# Swagger docs at http://localhost/docs/journey/
```
