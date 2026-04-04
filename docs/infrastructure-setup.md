# Infrastructure Setup Guide

> Full walkthrough: from zero to a running 3-node Docker Swarm cluster on Azure
> with PostgreSQL replication, load balancing, and Cloudflare CDN for the frontend.

---

## Architecture overview

```
                        ┌─────────────────────────────────────────┐
                        │            Cloudflare (CDN)              │
                        │  - Serves React SPA (HTML/JS/CSS)        │
                        │  - Caches static assets at edge globally  │
                        │  - Forwards /api/* to Azure LB           │
                        └─────────────────┬───────────────────────┘
                                          │ HTTPS (api.yourdomain.com)
                        ┌─────────────────▼───────────────────────┐
                        │         Azure Load Balancer              │
                        │  - Public IP, TCP port 80                │
                        │  - Health probe: GET /nginx-health        │
                        │  - Algorithm: round-robin                │
                        └───────┬─────────────┬─────────────┬──────┘
                                │             │             │
                    ┌───────────▼──┐  ┌───────▼──────┐  ┌──▼───────────┐
                    │   VM-A       │  │   VM-B       │  │   VM-C       │
                    │  (manager)   │  │  (worker)    │  │  (worker)    │
                    │              │  │              │  │              │
                    │  nginx:80    │  │  nginx:80    │  │  nginx:80    │
                    │  iam:8082    │  │  iam:8082    │  │  iam:8082    │
                    │  journey:8083│  │  journey:8083│  │  journey:8083│
                    │  capacity:8081  │  capacity:8081  │  capacity:8081│
                    │  map:8084    │  │  map:8084    │  │  map:8084    │
                    │  notif:8085  │  │  notif:8085  │  │  notif:8085  │
                    │  postgres:5432  │  postgres:5432  │  postgres:5432│
                    │  redis:6379  │  │  redis:6379  │  │  redis:6379  │
                    └──────┬───────┘  └──────┬────────┘  └──────┬──────┘
                           │                 │                   │
                           └────── PostgreSQL logical replication ┘
                                    (all-to-all, each VM
                                     publishes + subscribes)
```

**Key points:**
- All 3 VMs live in the **same Azure region** (West Europe / Ireland) — this is
  one *cell*. Cross-region is a future extension.
- The frontend React SPA is served by **Cloudflare** as a static site (free).
  The browser downloads the app from the nearest Cloudflare edge, then makes
  API calls to `api.yourdomain.com` which resolves to the Azure Load Balancer.
- Each VM runs every service. The Swarm `mode: global` ensures this automatically.
- PostgreSQL replication is **logical** (row-level, not streaming) so all 3
  instances stay in sync. Each VM reads/writes its *local* PostgreSQL — no
  cross-VM DB calls.
- Redis is **local per VM** — no cross-VM Redis sync. Route caches and event
  streams are local.

---

## 1. VM selection

### What to get

| Option | vCPU | RAM | Cost (West EU) | Verdict |
|--------|------|-----|----------------|---------|
| **B1ms** (recommended) | 1 | 2 GB | ~€15/month × 3 = €45/mo | Best balance for prototype |
| B2s | 2 | 4 GB | ~€30/month × 3 = €90/mo | Comfortable but expensive |
| B1s (minimum) | 1 | 1 GB | ~€7/month × 3 = €21/mo | Very tight; may OOM |

**Recommendation: 3 × Standard_B1ms**  
With €80 in Azure credits at €45/month, you get ~1.5–2 months of runtime.
To extend budget: stop VMs when not in use (€0 compute when deallocated, only
storage ~€1/month per VM).

**OS:** Ubuntu 24.04 LTS (free, widely documented)

**Disk:** 30 GB Premium SSD per VM (default, keeps PostgreSQL I/O fast)

### Why not B1s?
Memory budget at runtime on B1s (1 GB total):
```
PostgreSQL:          ~250 MB (shared_buffers=128MB default)
Redis:               ~100 MB
5 Go services:       ~30 MB each = 150 MB
nginx:               ~20 MB
OS + Docker:         ~200 MB
────────────────────────────────
Total:               ~720 MB   ← tight but workable
Peak (busy booking): ~900 MB   ← OOM risk
```
B1ms (2 GB) gives a comfortable 2× headroom.

---

## 2. Azure setup

### 2.1 Install Azure CLI and log in

```bash
# macOS
brew install azure-cli

# Windows (PowerShell)
winget install Microsoft.AzureCLI

# Log in
az login
```

### 2.2 Create resource group and VNet

```bash
REGION="westeurope"
RG="vcs-rg"
VNET="vcs-vnet"
SUBNET="vcs-subnet"

az group create --name $RG --location $REGION

az network vnet create \
  --resource-group $RG \
  --name $VNET \
  --address-prefix 10.0.0.0/16 \
  --subnet-name $SUBNET \
  --subnet-prefix 10.0.1.0/24
```

### 2.3 Create Network Security Group

```bash
NSG="vcs-nsg"
az network nsg create --resource-group $RG --name $NSG

# Allow SSH (your IP only — replace YOUR_IP)
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowSSH --priority 100 \
  --source-address-prefixes YOUR_IP/32 \
  --destination-port-ranges 22 --protocol Tcp --access Allow

# Allow HTTP from internet (Azure LB health probe + user traffic)
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowHTTP --priority 110 \
  --source-address-prefixes Internet \
  --destination-port-ranges 80 --protocol Tcp --access Allow

# Allow Docker Swarm ports between VMs (within VNet only)
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowSwarmInternal --priority 120 \
  --source-address-prefixes 10.0.1.0/24 \
  --destination-port-ranges 2376 2377 7946 4789 \
  --protocol '*' --access Allow

# Allow PostgreSQL between VMs (within VNet only)
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowPostgres --priority 130 \
  --source-address-prefixes 10.0.1.0/24 \
  --destination-port-ranges 5432 --protocol Tcp --access Allow
```

### 2.4 Create 3 VMs

```bash
# SSH key (if you don't have one)
ssh-keygen -t ed25519 -C "vcs-deploy" -f ~/.ssh/vcs_key

for i in A B C; do
  az vm create \
    --resource-group $RG \
    --name "vcs-vm-${i}" \
    --image Ubuntu2404 \
    --size Standard_B1ms \
    --admin-username deploy \
    --ssh-key-values ~/.ssh/vcs_key.pub \
    --vnet-name $VNET \
    --subnet $SUBNET \
    --nsg $NSG \
    --public-ip-sku Standard \
    --os-disk-size-gb 30 \
    --storage-sku Premium_LRS \
    --no-wait
done

# Wait for all to be running, then get IPs
az vm list-ip-addresses --resource-group $RG --output table
```

Note the **private IPs** (e.g. 10.0.1.4, 10.0.1.5, 10.0.1.6) — used for
Swarm and Postgres replication. Note the **public IPs** for SSH access.

---

## 3. Install Docker on all 3 VMs

Run this on **each VM** (SSH in with `ssh -i ~/.ssh/vcs_key deploy@<PUBLIC-IP>`):

```bash
# Update and install Docker
sudo apt-get update -y
sudo apt-get install -y ca-certificates curl gnupg

sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Allow deploy user to run docker without sudo
sudo usermod -aG docker deploy

# Verify
docker run hello-world
```

---

## 4. Initialize Docker Swarm

### On VM-A only (becomes the manager):

```bash
# Use the VM's private IP as the advertise address
PRIVATE_IP=$(hostname -I | awk '{print $1}')
docker swarm init --advertise-addr $PRIVATE_IP

# Copy the output — it looks like:
# docker swarm join --token SWMTKN-1-xxx... 10.0.1.4:2377
```

### On VM-B and VM-C (run the join command from above):

```bash
docker swarm join --token SWMTKN-1-xxx... 10.0.1.4:2377
```

### Verify on VM-A:

```bash
docker node ls
# ID                STATUS    AVAILABILITY   MANAGER STATUS
# abc123 *          Ready     Active         Leader
# def456            Ready     Active
# ghi789            Ready     Active
```

---

## 5. Create Docker secrets

Run **on VM-A** (Swarm manager). Secrets are distributed to all nodes automatically:

```bash
# Database password
echo "YourStrongPassword123!" | docker secret create db_password -

# JWT signing secret (32+ characters)
openssl rand -base64 32 | docker secret create jwt_secret -

# Firebase service account (download from Firebase console)
cat /path/to/firebase-service-account.json | docker secret create firebase_credentials -
```

### Create Docker configs

```bash
# Upload nginx config so it can be injected into containers at deploy time
docker config create nginx_conf /path/to/nginx/nginx.conf
```

---

## 6. GHCR authentication on all nodes

The Swarm needs to pull images from GitHub Container Registry.
Either use `--with-registry-auth` in the deploy command (recommended) or log in on each node:

```bash
# On each VM (A, B, C):
echo $GITHUB_PAT | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

Where `$GITHUB_PAT` is a GitHub Personal Access Token with `read:packages` scope.

---

## 7. Deploy the stack

Back on **VM-A**:

```bash
# Clone the repo (or copy the stack file)
git clone https://github.com/YOUR_ORG/distributed-vehicle-capacity-system.git /opt/vcs
cd /opt/vcs

# Set variables
export GITHUB_REPOSITORY="your-org/distributed-vehicle-capacity-system"
export IMAGE_TAG="latest"

docker stack deploy \
  --with-registry-auth \
  --prune \
  -c docker-stack.yml \
  vcs

# Watch services come up
watch docker service ls
```

All services should reach `3/3` replicas (one per node) within ~2 minutes.

---

## 8. PostgreSQL logical replication

This runs *inside* each VM's PostgreSQL container. You must do this once after
first deploy.

### 8.1 Configure PostgreSQL for logical replication

On **each VM**, edit PostgreSQL's configuration:

```bash
# Get the postgres container name
PG_CONTAINER=$(docker ps --filter name=vcs_db --format '{{.Names}}')

# Enter the container
docker exec -it $PG_CONTAINER bash

# Edit postgresql.conf
echo "wal_level = logical
max_replication_slots = 10
max_wal_senders = 10
listen_addresses = '*'" >> /var/lib/postgresql/data/postgresql.conf

# Edit pg_hba.conf to allow replication from other VMs
echo "host replication replicator 10.0.1.0/24 md5
host all all 10.0.1.0/24 md5" >> /var/lib/postgresql/data/pg_hba.conf

exit
```

Restart the postgres container on each VM:

```bash
docker service update --force vcs_db
```

### 8.2 Create the replication user and publication

On **each VM** (connect to the local postgres container):

```bash
PG_CONTAINER=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec -it $PG_CONTAINER psql -U postgres trafficservice
```

```sql
-- Create replication user (run on each VM)
CREATE USER replicator WITH REPLICATION LOGIN PASSWORD 'ReplicatorPass123!';

-- Grant access to all tables
GRANT SELECT ON ALL TABLES IN SCHEMA journey TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA capacity TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA iam TO replicator;
-- (add other schemas as needed)

-- Create publication (run on each VM)
CREATE PUBLICATION vcs_pub FOR ALL TABLES;
```

### 8.3 Create subscriptions

Get the private IPs of your 3 VMs first (e.g. 10.0.1.4, 10.0.1.5, 10.0.1.6).

**On VM-A** — subscribe to VM-B and VM-C:
```sql
CREATE SUBSCRIPTION vcs_sub_vmb
  CONNECTION 'host=10.0.1.5 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmc
  CONNECTION 'host=10.0.1.6 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

**On VM-B** — subscribe to VM-A and VM-C:
```sql
CREATE SUBSCRIPTION vcs_sub_vma
  CONNECTION 'host=10.0.1.4 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmc
  CONNECTION 'host=10.0.1.6 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

**On VM-C** — subscribe to VM-A and VM-B:
```sql
CREATE SUBSCRIPTION vcs_sub_vma
  CONNECTION 'host=10.0.1.4 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmb
  CONNECTION 'host=10.0.1.5 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

### 8.4 Verify replication is working

```sql
-- On any VM:
SELECT * FROM pg_stat_subscription;
-- Should show 2 rows (subscriptions to the other 2 VMs), status = 'streaming'

SELECT * FROM pg_stat_replication;
-- Shows your subscribers (other VMs reading from you)
```

Write a row on VM-A and verify it appears on VM-B within <100ms.

---

## 9. Azure Load Balancer setup

The Azure LB distributes incoming HTTP traffic (port 80) across all 3 VMs.

### Create the LB

```bash
# Public IP for the LB
az network public-ip create \
  --resource-group $RG \
  --name vcs-lb-pip \
  --sku Standard \
  --allocation-method Static

# Create the load balancer
az network lb create \
  --resource-group $RG \
  --name vcs-lb \
  --sku Standard \
  --public-ip-address vcs-lb-pip \
  --frontend-ip-name vcs-frontend \
  --backend-pool-name vcs-backend

# Health probe — checks nginx /nginx-health on each VM
az network lb probe create \
  --resource-group $RG \
  --lb-name vcs-lb \
  --name http-probe \
  --protocol Http \
  --port 80 \
  --path /nginx-health \
  --interval 15 \
  --threshold 2

# Load balancing rule: port 80 → port 80
az network lb rule create \
  --resource-group $RG \
  --lb-name vcs-lb \
  --name http-rule \
  --protocol Tcp \
  --frontend-port 80 \
  --backend-port 80 \
  --frontend-ip-name vcs-frontend \
  --backend-pool-name vcs-backend \
  --probe-name http-probe

# Add each VM's NIC to the backend pool
for NIC in vcs-vm-AVMNic vcs-vm-BVMNic vcs-vm-CVMNic; do
  az network nic ip-config address-pool add \
    --resource-group $RG \
    --nic-name $NIC \
    --ip-config-name ipconfig1 \
    --lb-name vcs-lb \
    --address-pool vcs-backend
done
```

### Get the LB public IP

```bash
az network public-ip show \
  --resource-group $RG \
  --name vcs-lb-pip \
  --query ipAddress -o tsv
# e.g. 51.105.123.45
```

The API is now reachable at `http://51.105.123.45/api/v1/...`

---

## 10. Cloudflare CDN — frontend + API routing

This is how the frontend gets to users globally and how API calls reach your VMs.

### Architecture

```
Browser (anywhere in the world)
    │
    ├── GET https://vcs-app.com/           → Cloudflare edge (nearest)
    │       Cloudflare serves cached React SPA (HTML/JS/CSS)
    │       Cache-Control: public, max-age=3600
    │
    └── POST https://api.vcs-app.com/api/v1/journeys
            → Cloudflare (passes through, no caching for API)
            → Azure Load Balancer (51.105.123.45)
            → One of 3 VMs (round-robin)
            → nginx → journey-service:8083
```

### 10.1 Set up Cloudflare (free plan is sufficient)

1. Create account at cloudflare.com
2. Add your domain (e.g. `vcs-app.com`) — follow their nameserver instructions
3. Your domain's DNS is now managed by Cloudflare

### 10.2 Deploy frontend to Cloudflare Pages (free)

```bash
# Build the frontend
cd frontend && npm run build

# Install Wrangler (Cloudflare CLI)
npm install -g wrangler
wrangler login

# Deploy to Cloudflare Pages
wrangler pages deploy dist \
  --project-name vcs-frontend \
  --branch main
```

Cloudflare Pages gives you:
- A free `*.pages.dev` URL immediately
- Custom domain via `vcs-app.com` (add a CNAME in Cloudflare DNS)
- Automatic HTTPS (Let's Encrypt managed by Cloudflare)
- Edge caching of all static assets in 300+ cities globally
- Automatic redeployment from GitHub (connect the repo in Pages settings)

The frontend is now served from Cloudflare's edge. A user in Tokyo loads the
JS bundle from Tokyo's Cloudflare PoP — not from your Azure VM in Ireland.

### 10.3 Configure API subdomain

In Cloudflare DNS dashboard, add:
```
Type: A
Name: api
Value: 51.105.123.45  ← your Azure LB public IP
Proxied: YES (orange cloud)  ← enables Cloudflare's network for the API too
```

Cloudflare proxied means:
- Your VM IPs are hidden (DDoS protection)
- Cloudflare handles TLS termination (free HTTPS for `api.vcs-app.com`)
- API responses are NOT cached (correct — you don't want booking responses cached)

### 10.4 Update the frontend API base URL

In `frontend/src/app/services/journeyApi.ts`, change:
```ts
const BASE_URL = 'https://api.vcs-app.com';
```

### 10.5 Update nginx CORS

In `nginx/nginx.conf`, ensure CORS allows the Cloudflare Pages domain:
```nginx
add_header Access-Control-Allow-Origin "https://vcs-app.com";
add_header Access-Control-Allow-Methods "GET, POST, PUT, OPTIONS";
add_header Access-Control-Allow-Headers "Authorization, Content-Type, Idempotency-Key";
```

---

## 11. Full request flow — end to end

### Driver books a journey

```
1. Driver opens https://vcs-app.com on their phone (anywhere in world)
   → Cloudflare edge (e.g. Frankfurt PoP) serves React SPA from cache
   → App loads in ~200ms

2. Driver fills form, clicks "Submit booking"
   → Browser: POST https://api.vcs-app.com/api/v1/journeys
   → Cloudflare: passes through to origin (no cache for POST)
   → Azure LB: routes to VM-B (round-robin or least-conn)
   → VM-B nginx: routes to journey-service:8083
   → Journey Service:
       a. Validates JWT (local HMAC, <1ms)
       b. Checks active journey (local postgres, 5ms)
       c. Redis route cache hit → skips Map Service call
       d. Calls capacity-service:8081 (same VM, Docker overlay, <2ms)
       e. Capacity Service: SELECT FOR UPDATE → reserves slots (20ms)
       f. Journey Service: INSERT into journey.journeys (10ms)
       g. Returns 201 APPROVED to nginx → LB → Cloudflare → browser

3. Journey Service (async, after response):
   → Publishes journey.booked to Redis Streams (VM-B's local Redis, <1ms)
   → notification-service on VM-B consumes event → sends Firebase push

4. PostgreSQL on VM-B replicates new journey row to VM-A and VM-C
   → Takes <100ms on Azure intra-region LAN
   → Driver can now query journey from any VM
```

---

## 12. Cost summary

| Resource | SKU | Monthly cost (est.) |
|----------|-----|---------------------|
| 3 × VM Standard_B1ms | West Europe | €45 |
| 3 × Premium SSD OS disk 30GB | P4 | €15 |
| Azure Standard LB | (hourly) | €16 |
| Public IPs (4: 3 VMs + LB) | Standard static | €12 |
| Egress (10 GB/month) | West Europe | €1 |
| Cloudflare (free plan) | — | €0 |
| Cloudflare Pages (free) | — | €0 |
| **Total** | | **~€89/month** |

**To stay within €80 credits:**
- Stop VMs overnight/weekends: `az vm deallocate --name vcs-vm-A ...`
  (saves ~60% of VM cost; disk still charged)
- Use B1s instead of B1ms: saves €24/month (€65 total) — tight on RAM
- Remove LB and use one VM as primary for demos: saves €28/month (€61 total)

---

## 13. Verification checklist

After setup, run these to confirm everything is working:

```bash
# 1. All Swarm services running (3/3 replicas each)
docker service ls

# 2. Postgres replication active
docker exec -it $(docker ps -q -f name=vcs_db) \
  psql -U postgres -c "SELECT * FROM pg_stat_subscription;"

# 3. LB health probe passing
curl http://<LB-IP>/nginx-health

# 4. API reachable through Cloudflare
curl https://api.vcs-app.com/health

# 5. Frontend loads from CDN (check CF-Cache-Status header)
curl -I https://vcs-app.com
# CF-Cache-Status: HIT  ← means Cloudflare is serving from edge cache

# 6. End-to-end booking (replace with real JWT)
curl -X POST https://api.vcs-app.com/api/v1/journeys \
  -H "Authorization: Bearer <JWT>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"origin":{"lat":53.3498,"lng":-6.2603},"destination":{"lat":51.8985,"lng":-8.4756},"departure_time":"2026-04-16T10:00:00Z","vehicle_type":"car"}'
```
