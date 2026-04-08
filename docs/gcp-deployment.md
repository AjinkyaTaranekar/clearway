# VCS - GCP Deployment Guide

> **Project:** `distributed-capacity-system`  
> **Account:** `ajinkyataranekar26@gmail.com`  
> **Shell:** GCP Cloud Shell (gcloud pre-installed, pre-authenticated)

---

## Architecture

```
                    ┌─────────────────────────────────────────────────┐
                    │           Per-cell Network Load Balancer         │
                    │   EU LB ──► us-east LB ──► asia-east LB         │
                    └───────┬─────────────┬──────────────┬────────────┘
                            │             │              │
              ┌─────────────┴──┐    ┌─────┴──────┐  ┌───┴────────┐
              │   Cell EU      │    │  Cell US   │  │  Cell APAC │
              │ europe-west1-b │    │ us-east1-b │  │ asia-east1-b│
              │                │    │            │  │            │
              │ vcs-vm-eu1     │    │ vcs-vm-us1 │  │ vcs-vm-ap1 │
              │  10.0.1.11     │    │ 10.0.2.11  │  │ 10.0.3.11  │
              │ vcs-vm-eu2     │    │            │  │            │
              │  10.0.1.12     │    │            │  │            │
              │                │    │            │  │            │
              │ Docker Swarm A │    │  Swarm B   │  │  Swarm C   │
              │ (2-node: eu1   │    │ (1-node)   │  │  (1-node)  │
              │  leader+eu2)   │    │            │  │            │
              └───────┬────────┘    └─────┬──────┘  └───┬────────┘
                      │                   │              │
                      │  Postgres logical replication    │
                      │  (chain: EU → US → APAC)         │
                      └──────────►────────►──────────────┘
                               ~80ms lag       ~150ms lag
```

**Key points:**
- Each cell is an **independent** Docker Swarm cluster. Swarms never join each other.
- Cross-cell communication = Postgres logical replication **only** (chain: EU publishes → US subscribes + publishes → APAC subscribes with `origin = any`).
- EU has 2 VMs for Swarm HA demo. US and APAC have 1 VM each for latency simulation.
- EU 2-node Swarm note: quorum = 2/2. If one node is down, running containers persist but no new scheduling until it comes back. Sufficient for demo.

---

## Credentials reference

> **Never commit passwords or PAT to git.** The doc uses variable names - set them in your shell before running commands.

```bash
# Set once in your shell session - all commands below use these
export ADMIN_IP="34.38.106.154"
export GH_USER="AjinkyaTaranekar"
export GH_PAT="<your-github-pat>"          # do not paste in the doc
export DB_PASS="RA5Rgr7jb8HiXyfasng8wo2Vcs!"
export REPL_PASS="2IdbDCxBt4mHbPArRtzBu4gRep!"
```

---

## Part 1 - One-time GCP project setup

### Step 1 - Configure gcloud

```bash
gcloud config set project distributed-capacity-system
gcloud config set compute/region europe-west1
gcloud config set compute/zone europe-west1-b

# Enable Compute Engine API (not yet enabled - takes ~1 minute)
gcloud services enable compute.googleapis.com

gcloud config list   # verify
```

### Step 2 - Generate SSH key

```bash
ssh-keygen -t ed25519 -C "vcs-deploy" -f ~/.ssh/vcs_key
# No passphrase - CI needs passwordless access
# ~/.ssh/vcs_key.pub  → goes to VMs
# ~/.ssh/vcs_key      → goes to GitHub secret SWARM_SSH_KEY (never commit)
```

### Step 3 - Create global VPC with 3 regional subnets

```bash
# Global custom VPC
gcloud compute networks create vcs-vpc --subnet-mode=custom

# EU subnet
gcloud compute networks subnets create vcs-subnet-eu \
  --network=vcs-vpc --region=europe-west1 --range=10.0.1.0/24

# US subnet
gcloud compute networks subnets create vcs-subnet-us \
  --network=vcs-vpc --region=us-east1 --range=10.0.2.0/24

# APAC subnet
gcloud compute networks subnets create vcs-subnet-ap \
  --network=vcs-vpc --region=asia-east1 --range=10.0.3.0/24
```

### Step 4 - Firewall rules

```bash
# SSH - Cloud Shell IP only
gcloud compute firewall-rules create vcs-allow-ssh \
  --network=vcs-vpc \
  --allow=tcp:22 \
  --source-ranges=$ADMIN_IP/32 \
  --target-tags=vcs-node \
  --description="SSH from Cloud Shell only"

# HTTP - open (LB health probes + API traffic)
gcloud compute firewall-rules create vcs-allow-http \
  --network=vcs-vpc \
  --allow=tcp:80 \
  --source-ranges=0.0.0.0/0 \
  --target-tags=vcs-node

# Internal - Swarm + Postgres + Redis across all 3 subnets
gcloud compute firewall-rules create vcs-allow-internal \
  --network=vcs-vpc \
  --allow=tcp:2377,tcp:7946,udp:7946,udp:4789,tcp:5432,tcp:6379 \
  --source-ranges=10.0.1.0/24,10.0.2.0/24,10.0.3.0/24 \
  --target-tags=vcs-node \
  --description="Swarm + Postgres + Redis - all cells"
```

---

## Part 2 - EU cell (europe-west1, 2 VMs)

### Step 5 - Create EU VMs

```bash
# EU VM 1 - will be Swarm leader
gcloud compute instances create vcs-vm-eu1 \
  --zone=europe-west1-b \
  --machine-type=e2-medium \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-balanced \
  --subnet=vcs-subnet-eu \
  --private-network-ip=10.0.1.11 \
  --no-address \
  --tags=vcs-node \
  --metadata="ssh-keys=deploy:$(cat ~/.ssh/vcs_key.pub)"

# EU VM 2 - will join as second manager
gcloud compute instances create vcs-vm-eu2 \
  --zone=europe-west1-b \
  --machine-type=e2-medium \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-balanced \
  --subnet=vcs-subnet-eu \
  --private-network-ip=10.0.1.12 \
  --no-address \
  --tags=vcs-node \
  --metadata="ssh-keys=deploy:$(cat ~/.ssh/vcs_key.pub)"

# Static external IPs
gcloud compute addresses create vcs-vm-eu1-ip --region=europe-west1
gcloud compute addresses create vcs-vm-eu2-ip --region=europe-west1

gcloud compute instances add-access-config vcs-vm-eu1 \
  --zone=europe-west1-b --access-config-name="External NAT" \
  --address=$(gcloud compute addresses describe vcs-vm-eu1-ip \
    --region=europe-west1 --format='value(address)')

gcloud compute instances add-access-config vcs-vm-eu2 \
  --zone=europe-west1-b --access-config-name="External NAT" \
  --address=$(gcloud compute addresses describe vcs-vm-eu2-ip \
    --region=europe-west1 --format='value(address)')

# Get IPs - write down EU1 and EU2 external IPs
gcloud compute instances list --filter="name~vcs-vm-eu" \
  --format="table(name,networkInterfaces[0].networkIP,networkInterfaces[0].accessConfigs[0].natIP)"
```

### Step 6 - EU Load Balancer

```bash
EU1_EXT=$(gcloud compute addresses describe vcs-vm-eu1-ip \
  --region=europe-west1 --format='value(address)')

gcloud compute addresses create vcs-lb-eu-ip --region=europe-west1
LB_EU=$(gcloud compute addresses describe vcs-lb-eu-ip \
  --region=europe-west1 --format='value(address)')
echo "EU LB IP: $LB_EU"

gcloud compute http-health-checks create vcs-hc-eu \
  --request-path=/nginx-health --port=80 \
  --check-interval=10s --timeout=5s \
  --healthy-threshold=2 --unhealthy-threshold=2

gcloud compute target-pools create vcs-pool-eu \
  --region=europe-west1 --health-checks=vcs-hc-eu

gcloud compute target-pools add-instances vcs-pool-eu \
  --instances=vcs-vm-eu1,vcs-vm-eu2 \
  --instances-zone=europe-west1-b

gcloud compute forwarding-rules create vcs-fwd-eu \
  --region=europe-west1 \
  --load-balancing-scheme=EXTERNAL \
  --address=$LB_EU \
  --target-pool=vcs-pool-eu \
  --ports=80
```

### Step 7 - Install Docker on EU VMs

Run on **vcs-vm-eu1** and **vcs-vm-eu2** (repeat swapping IP):

```bash
ssh -i ~/.ssh/vcs_key deploy@<EU-VM-IP>

sudo apt-get update -y
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io \
  docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker $USER
exit   # re-login for group change
```

### Step 8 - Init EU Swarm

**On vcs-vm-eu1 only:**

```bash
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP>
docker swarm init --advertise-addr 10.0.1.11
docker swarm join-token manager -q   # write this down → EU_SWARM_TOKEN
exit
```

**On vcs-vm-eu2:**

```bash
ssh -i ~/.ssh/vcs_key deploy@<EU2-IP>
docker swarm join --token <EU_SWARM_TOKEN> 10.0.1.11:2377
exit
```

**Verify on eu1:**

```bash
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP>
docker node ls
# vcs-vm-eu1 *  Ready  Active  Leader
# vcs-vm-eu2    Ready  Active  Reachable
exit
```

### Step 9 - Docker secrets (EU cell)

On **vcs-vm-eu1** only - Swarm distributes to eu2 automatically:

```bash
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP>

echo "$DB_PASS" | docker secret create db_password -
openssl rand -base64 32 | docker secret create jwt_secret -

# Firebase: copy JSON to VM first
#   scp -i ~/.ssh/vcs_key firebase-sa.json deploy@<EU1-IP>:~/
cat ~/firebase-sa.json | docker secret create firebase_credentials -
rm ~/firebase-sa.json

docker secret ls
exit
```

### Step 10 - GHCR auth + deploy (EU cell)

```bash
# Auth on both EU VMs
for IP in <EU1-IP> <EU2-IP>; do
  ssh -i ~/.ssh/vcs_key deploy@$IP \
    "echo $GH_PAT | docker login ghcr.io -u AjinkyaTaranekar --password-stdin"
done

# Deploy on eu1
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP>
git clone https://github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system.git ~/vcs
cd ~/vcs
export GITHUB_REPOSITORY="AjinkyaTaranekar/distributed-vehicle-capacity-system"
export IMAGE_TAG="latest"
docker stack deploy --with-registry-auth --prune -c docker-stack.yml vcs
watch docker service ls   # wait for 2/2 on all services
exit
```

### Step 11 - EU Postgres replication (eu1 ↔ eu2)

**On both vcs-vm-eu1 and vcs-vm-eu2:**

```bash
PG=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec $PG bash -c "
cat >> /var/lib/postgresql/data/postgresql.conf <<'EOF'
wal_level = logical
max_replication_slots = 10
max_wal_senders = 10
listen_addresses = '*'
EOF
cat >> /var/lib/postgresql/data/pg_hba.conf <<'EOF'
host replication replicator 10.0.1.0/24 md5
host replication replicator 10.0.2.0/24 md5
host replication replicator 10.0.3.0/24 md5
host all         all         10.0.1.0/24 md5
EOF
"
docker service update --force vcs_db
```

Wait ~30 seconds, then on **each EU VM**:

```bash
PG=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec -i $PG psql -U postgres trafficservice <<SQL
CREATE USER replicator WITH REPLICATION LOGIN PASSWORD '$REPL_PASS';
GRANT SELECT ON ALL TABLES IN SCHEMA journey TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA capacity TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA iam TO replicator;
CREATE PUBLICATION vcs_pub FOR ALL TABLES;
SQL
```

**On eu1** - subscribe to eu2:

```sql
CREATE SUBSCRIPTION vcs_sub_eu2
  CONNECTION 'host=10.0.1.12 port=5432 dbname=trafficservice
              user=replicator password=2IdbDCxBt4mHbPArRtzBu4gRep!'
  PUBLICATION vcs_pub;
```

**On eu2** - subscribe to eu1:

```sql
CREATE SUBSCRIPTION vcs_sub_eu1
  CONNECTION 'host=10.0.1.11 port=5432 dbname=trafficservice
              user=replicator password=2IdbDCxBt4mHbPArRtzBu4gRep!'
  PUBLICATION vcs_pub;
```

---

## Part 3 - US cell (us-east1, 1 VM)

### Step 12 - Create US VM

```bash
gcloud compute instances create vcs-vm-us1 \
  --zone=us-east1-b \
  --machine-type=e2-medium \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-balanced \
  --subnet=vcs-subnet-us \
  --private-network-ip=10.0.2.11 \
  --no-address \
  --tags=vcs-node \
  --metadata="ssh-keys=deploy:$(cat ~/.ssh/vcs_key.pub)"

gcloud compute addresses create vcs-vm-us1-ip --region=us-east1
gcloud compute instances add-access-config vcs-vm-us1 \
  --zone=us-east1-b --access-config-name="External NAT" \
  --address=$(gcloud compute addresses describe vcs-vm-us1-ip \
    --region=us-east1 --format='value(address)')

gcloud compute instances list --filter="name~vcs-vm-us" \
  --format="table(name,networkInterfaces[0].networkIP,networkInterfaces[0].accessConfigs[0].natIP)"
```

### Step 13 - US Load Balancer

```bash
gcloud compute addresses create vcs-lb-us-ip --region=us-east1
LB_US=$(gcloud compute addresses describe vcs-lb-us-ip \
  --region=us-east1 --format='value(address)')
echo "US LB IP: $LB_US"

gcloud compute http-health-checks create vcs-hc-us \
  --request-path=/nginx-health --port=80 \
  --check-interval=10s --timeout=5s \
  --healthy-threshold=2 --unhealthy-threshold=2

gcloud compute target-pools create vcs-pool-us \
  --region=us-east1 --health-checks=vcs-hc-us

gcloud compute target-pools add-instances vcs-pool-us \
  --instances=vcs-vm-us1 --instances-zone=us-east1-b

gcloud compute forwarding-rules create vcs-fwd-us \
  --region=us-east1 \
  --load-balancing-scheme=EXTERNAL \
  --address=$LB_US \
  --target-pool=vcs-pool-us \
  --ports=80
```

### Step 14 - Install Docker, Swarm, deploy (US cell)

```bash
# Install Docker (same script as Step 7 - swap IP for US1)

# Single-node Swarm init
ssh -i ~/.ssh/vcs_key deploy@<US1-IP>
docker swarm init --advertise-addr 10.0.2.11
exit

# Secrets - use same db_password and jwt_secret as EU for cross-cell JWT validation
ssh -i ~/.ssh/vcs_key deploy@<US1-IP>
echo "$DB_PASS" | docker secret create db_password -
echo "<SAME_JWT_SECRET_AS_EU>" | docker secret create jwt_secret -
cat ~/firebase-sa.json | docker secret create firebase_credentials -

# GHCR auth + deploy
echo $GH_PAT | docker login ghcr.io -u AjinkyaTaranekar --password-stdin
git clone https://github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system.git ~/vcs
cd ~/vcs
export GITHUB_REPOSITORY="AjinkyaTaranekar/distributed-vehicle-capacity-system"
export IMAGE_TAG="latest"
docker stack deploy --with-registry-auth --prune -c docker-stack.yml vcs
watch docker service ls
exit
```

---

## Part 4 - APAC cell (asia-east1, 1 VM)

### Step 15 - Create APAC VM

```bash
gcloud compute instances create vcs-vm-ap1 \
  --zone=asia-east1-b \
  --machine-type=e2-medium \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-balanced \
  --subnet=vcs-subnet-ap \
  --private-network-ip=10.0.3.11 \
  --no-address \
  --tags=vcs-node \
  --metadata="ssh-keys=deploy:$(cat ~/.ssh/vcs_key.pub)"

gcloud compute addresses create vcs-vm-ap1-ip --region=asia-east1
gcloud compute instances add-access-config vcs-vm-ap1 \
  --zone=asia-east1-b --access-config-name="External NAT" \
  --address=$(gcloud compute addresses describe vcs-vm-ap1-ip \
    --region=asia-east1 --format='value(address)')

gcloud compute instances list --filter="name~vcs-vm-ap" \
  --format="table(name,networkInterfaces[0].networkIP,networkInterfaces[0].accessConfigs[0].natIP)"
```

### Step 16 - APAC Load Balancer

```bash
gcloud compute addresses create vcs-lb-ap-ip --region=asia-east1
LB_AP=$(gcloud compute addresses describe vcs-lb-ap-ip \
  --region=asia-east1 --format='value(address)')
echo "APAC LB IP: $LB_AP"

gcloud compute http-health-checks create vcs-hc-ap \
  --request-path=/nginx-health --port=80 \
  --check-interval=10s --timeout=5s \
  --healthy-threshold=2 --unhealthy-threshold=2

gcloud compute target-pools create vcs-pool-ap \
  --region=asia-east1 --health-checks=vcs-hc-ap

gcloud compute target-pools add-instances vcs-pool-ap \
  --instances=vcs-vm-ap1 --instances-zone=asia-east1-b

gcloud compute forwarding-rules create vcs-fwd-ap \
  --region=asia-east1 \
  --load-balancing-scheme=EXTERNAL \
  --address=$LB_AP \
  --target-pool=vcs-pool-ap \
  --ports=80
```

### Step 17 - Install Docker, Swarm, deploy (APAC cell)

Same as US (Step 14) - swap IP for AP1 and use `10.0.3.11` as `--advertise-addr`.

---

## Part 5 - Cross-cell Postgres replication (EU → US → APAC)

This is a **cascade chain**: EU writes replicate to US, and US re-publishes them to APAC using `origin = any`.

```
eu1 (primary publisher)
  └──► us1 (subscribes from eu1, publishes with origin = any)
          └──► ap1 (subscribes from us1 with origin = any - gets EU + US rows)
```

### Step 18 - Enable replication on US and APAC Postgres

On **vcs-vm-us1** and **vcs-vm-ap1** - same config as Step 11 but with all 3 subnet ranges in pg_hba.conf (already included in Step 11's snippet above).

Create replication user + publication on both:

```sql
CREATE USER replicator WITH REPLICATION LOGIN PASSWORD '2IdbDCxBt4mHbPArRtzBu4gRep!';
GRANT SELECT ON ALL TABLES IN SCHEMA journey TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA capacity TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA iam TO replicator;
CREATE PUBLICATION vcs_pub FOR ALL TABLES;
```

### Step 19 - EU → US subscription

**On vcs-vm-us1:**

```sql
CREATE SUBSCRIPTION vcs_sub_eu
  CONNECTION 'host=10.0.1.11 port=5432 dbname=trafficservice
              user=replicator password=2IdbDCxBt4mHbPArRtzBu4gRep!'
  PUBLICATION vcs_pub
  WITH (origin = any);   -- re-publish rows that came from EU to APAC
```

### Step 20 - US → APAC subscription

**On vcs-vm-ap1:**

```sql
CREATE SUBSCRIPTION vcs_sub_us
  CONNECTION 'host=10.0.2.11 port=5432 dbname=trafficservice
              user=replicator password=2IdbDCxBt4mHbPArRtzBu4gRep!'
  PUBLICATION vcs_pub
  WITH (origin = any);   -- receive both EU-originated and US-originated rows
```

### Step 21 - Verify cross-cell replication

```bash
# Write a row in EU
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP>
PG=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec $PG psql -U postgres trafficservice \
  -c "INSERT INTO journey.journeys (id, status) VALUES ('latency-test', 'ACTIVE');"
exit

# Read from US - expect ~80–100 ms lag
ssh -i ~/.ssh/vcs_key deploy@<US1-IP>
docker exec $(docker ps -q -f name=vcs_db) psql -U postgres trafficservice \
  -c "SELECT id, status FROM journey.journeys WHERE id = 'latency-test';"
exit

# Read from APAC - expect ~200–250 ms lag (EU→US + US→APAC)
ssh -i ~/.ssh/vcs_key deploy@<AP1-IP>
docker exec $(docker ps -q -f name=vcs_db) psql -U postgres trafficservice \
  -c "SELECT id, status FROM journey.journeys WHERE id = 'latency-test';"

# Measure lag live from APAC
docker exec $(docker ps -q -f name=vcs_db) psql -U postgres trafficservice \
  -c "SELECT subname, received_lsn, latest_end_lsn,
      (latest_end_time - last_msg_send_time) AS lag
      FROM pg_stat_subscription;"
exit
```

---

## Part 6 - VM scheduler (auto stop/start)

Uses GCP's built-in **Instance Schedules** (resource policies) - no Cloud Functions needed.

### Step 22 - Create stop/start schedules per region

```bash
# EU schedule - stop 20:00, start 08:00 Mon-Fri, Dublin time
gcloud compute resource-policies create instance-schedule vcs-schedule-eu \
  --region=europe-west1 \
  --vm-start-schedule="0 8 * * 1-5" \
  --vm-stop-schedule="0 20 * * 1-5" \
  --timezone="Europe/Dublin"

# US schedule - stop 20:00, start 08:00 Mon-Fri, New York time
gcloud compute resource-policies create instance-schedule vcs-schedule-us \
  --region=us-east1 \
  --vm-start-schedule="0 8 * * 1-5" \
  --vm-stop-schedule="0 20 * * 1-5" \
  --timezone="America/New_York"

# APAC schedule - stop 20:00, start 08:00 Mon-Fri, Singapore time
gcloud compute resource-policies create instance-schedule vcs-schedule-ap \
  --region=asia-east1 \
  --vm-start-schedule="0 8 * * 1-5" \
  --vm-stop-schedule="0 20 * * 1-5" \
  --timezone="Asia/Singapore"
```

### Step 23 - Attach schedules to VMs

```bash
# EU
gcloud compute instances add-resource-policies vcs-vm-eu1 \
  --resource-policies=vcs-schedule-eu --zone=europe-west1-b
gcloud compute instances add-resource-policies vcs-vm-eu2 \
  --resource-policies=vcs-schedule-eu --zone=europe-west1-b

# US
gcloud compute instances add-resource-policies vcs-vm-us1 \
  --resource-policies=vcs-schedule-us --zone=us-east1-b

# APAC
gcloud compute instances add-resource-policies vcs-vm-ap1 \
  --resource-policies=vcs-schedule-ap --zone=asia-east1-b
```

Schedules are live immediately. VMs stop at 20:00 local time, start at 08:00 - roughly 12h/day off on weekdays, 48h off on weekends.

### Manual override

```bash
# Stop all now
gcloud compute instances stop vcs-vm-eu1 vcs-vm-eu2 --zone=europe-west1-b
gcloud compute instances stop vcs-vm-us1 --zone=us-east1-b
gcloud compute instances stop vcs-vm-ap1 --zone=asia-east1-b

# Start all now
gcloud compute instances start vcs-vm-eu1 vcs-vm-eu2 --zone=europe-west1-b
gcloud compute instances start vcs-vm-us1 --zone=us-east1-b
gcloud compute instances start vcs-vm-ap1 --zone=asia-east1-b
```

---

## Part 7 - GitHub Actions CI/CD

### Step 24 - Collect SSH known hosts

```bash
ssh-keyscan <EU1-EXTERNAL-IP>   # copy full output
```

### Step 25 - Add GitHub repository secrets

Go to **Settings → Secrets and variables → Actions → New repository secret**:

| Secret | Value |
|--------|-------|
| `SWARM_HOST` | vcs-vm-eu1 external IP |
| `SWARM_SSH_KEY` | Contents of `~/.ssh/vcs_key` (private key) |
| `SWARM_KNOWN_HOSTS` | Output of `ssh-keyscan <EU1-IP>` |

`GITHUB_TOKEN` is automatic.

---

## Part 8 - Verification checklist

```bash
# 1. EU Swarm - both nodes managers
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP> "docker node ls"
# vcs-vm-eu1 * Ready Active Leader
# vcs-vm-eu2   Ready Active Reachable

# 2. All services at 2/2 in EU cell
ssh -i ~/.ssh/vcs_key deploy@<EU1-IP> "docker service ls"

# 3. US and APAC services at 1/1
ssh -i ~/.ssh/vcs_key deploy@<US1-IP>  "docker service ls"
ssh -i ~/.ssh/vcs_key deploy@<AP1-IP>  "docker service ls"

# 4. LB health probes
curl http://<LB-EU-IP>/nginx-health    # → ok
curl http://<LB-US-IP>/nginx-health    # → ok
curl http://<LB-AP-IP>/nginx-health    # → ok

# 5. Replication active (2 subscriptions on eu1 and eu2, 1 each on us1 and ap1)
# On each VM:
docker exec $(docker ps -q -f name=vcs_db) \
  psql -U postgres -c "SELECT subname, subenabled FROM pg_stat_subscription;"

# 6. Smoke test each cell independently
for LB in <LB-EU-IP> <LB-US-IP> <LB-AP-IP>; do
  echo "Testing $LB..."
  curl -s http://$LB/nginx-health
done
```

---

## Simulating scenarios

### Geographic latency demo

```bash
# Measure round-trip time per cell from Cloud Shell (europe-west1)
time curl -s http://<LB-EU-IP>/nginx-health    # ~5–15 ms
time curl -s http://<LB-US-IP>/nginx-health    # ~90–110 ms
time curl -s http://<LB-AP-IP>/nginx-health    # ~230–270 ms
```

### Cross-cell partition demo

```bash
# Block replication from EU to US (simulates network partition)
gcloud compute firewall-rules create vcs-partition-test \
  --network=vcs-vpc \
  --action=DENY \
  --rules=tcp:5432 \
  --source-ranges=10.0.1.0/24 \
  --target-tags=vcs-node \
  --priority=900

# Both cells keep serving (AP trade-off)
# Write to EU - US and APAC go stale
# Heal:
gcloud compute firewall-rules delete vcs-partition-test --quiet
# Replication catches up automatically
```

### EU Swarm failover demo

```bash
# Stop eu1 - eu2 becomes Swarm leader within ~5 seconds
gcloud compute instances stop vcs-vm-eu1 --zone=europe-west1-b

ssh -i ~/.ssh/vcs_key deploy@<EU2-IP> "docker node ls"
# vcs-vm-eu1   Ready  Active  Unreachable
# vcs-vm-eu2 * Ready  Active  Leader       ← auto-elected

# Services keep running from eu2 - LB stops routing to eu1 within ~20s
curl http://<LB-EU-IP>/nginx-health   # still ok

# Restore
gcloud compute instances start vcs-vm-eu1 --zone=europe-west1-b
```

---

## Cost breakdown

| Resource | Qty | Monthly (full) | With scheduler (~50% off) |
|----------|-----|----------------|--------------------------|
| e2-medium (EU ×2) | europe-west1 | ~$50 | ~$25 |
| e2-medium (US ×1) | us-east1 | ~$25 | ~$13 |
| e2-medium (APAC ×1) | asia-east1 | ~$25 | ~$13 |
| pd-balanced 30 GB ×4 | all regions | ~$24 | ~$24 (disks always on) |
| 3 × Network LB | forwarding rules | ~$54 | ~$54 |
| 4 VM static IPs | idle charge | ~$28 | ~$28 |
| 3 LB static IPs | | ~$21 | ~$21 |
| Cross-region egress | ~5 GB/mo | ~$1 | ~$1 |
| **Total** | | **~$228/mo** | **~$179/mo** |

> VMs can be resized at any time while stopped:
> ```bash
> gcloud compute instances stop vcs-vm-eu1 --zone=europe-west1-b
> gcloud compute instances set-machine-type vcs-vm-eu1 \
>   --machine-type=e2-small --zone=europe-west1-b
> gcloud compute instances start vcs-vm-eu1 --zone=europe-west1-b
> ```

---

## Operational quick reference

```bash
# SSH via gcloud (no key file needed in Cloud Shell)
gcloud compute ssh vcs-vm-eu1 --zone=europe-west1-b
gcloud compute ssh vcs-vm-us1 --zone=us-east1-b
gcloud compute ssh vcs-vm-ap1 --zone=asia-east1-b

# Service status on any cell
docker service ls
docker service logs -f vcs_journey-service

# Replication lag
docker exec $(docker ps -q -f name=vcs_db) \
  psql -U postgres -c \
  "SELECT subname, (latest_end_time - last_msg_send_time) AS lag FROM pg_stat_subscription;"

# Force redeploy without image change
docker service update --force vcs_journey-service

# Drain a node for maintenance
docker node update --availability drain vcs-vm-eu2

# Tear down a cell's stack
docker stack rm vcs

# List all VMs
gcloud compute instances list --filter="name~vcs-vm"
```
