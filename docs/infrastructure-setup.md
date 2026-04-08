# Infrastructure Setup Guide

> Full walkthrough: from zero to a running 3-node Docker Swarm cluster on
> **Azure**, **GCP**, or **AWS** with PostgreSQL replication, load balancing,
> and Cloudflare CDN for the frontend.
>
> Sections 1–3 are provider-specific. Sections 4 onwards are identical
> regardless of cloud.

---

## Architecture overview

```
                        ┌─────────────────────────────────────────┐
                        │            Cloudflare (CDN)              │
                        │  - Serves React SPA (HTML/JS/CSS)        │
                        │  - Caches static assets at edge globally  │
                        │  - Forwards /api/* to cloud LB           │
                        └─────────────────┬───────────────────────┘
                                          │ HTTPS (api.yourdomain.com)
                        ┌─────────────────▼───────────────────────┐
                        │         Cloud Load Balancer              │
                        │  - Public IP/DNS, TCP port 80            │
                        │  - Health probe: GET /nginx-health        │
                        │  - Algorithm: round-robin                │
                        └───────┬─────────────┬─────────────┬──────┘
                                │             │             │
                    ┌───────────▼──┐  ┌───────▼──────┐  ┌──▼───────────┐
                    │   VM-A       │  │   VM-B       │  │   VM-C       │
                    │  (manager)   │  │  (manager)   │  │  (manager)   │
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
- All 3 VMs are in the **same region** (Ireland/EU) - one *cell*. Cross-region is a future extension.
- All 3 VMs are **Swarm managers**. Raft quorum = 2/3, so the cluster survives losing any single node - a new leader is elected automatically within ~5 seconds.
- The frontend React SPA is served by **Cloudflare Pages** (free). The browser downloads the app from the nearest Cloudflare edge, then makes API calls to `api.yourdomain.com` which resolves to the cloud load balancer.
- Each VM runs every service (`mode: global` in docker-stack.yml).
- PostgreSQL replication is **logical** (all-to-all). Each VM reads/writes its local PostgreSQL - no cross-VM DB calls at runtime.
- Redis is local per VM - no cross-VM Redis sync.

---

## 1. VM selection

### Recommended specs

| Cloud | SKU | vCPU | RAM | Est. cost/mo (×3) | Best region |
|-------|-----|------|-----|-------------------|-------------|
| **Azure** | Standard_B2s | 2 | 4 GB | ~€90 | `northeurope` (Dublin) |
| **GCP** | e2-medium | 2 | 4 GB | ~$75 | `europe-west1` (Belgium) |
| **AWS** | t3.small | 2 | 2 GB | ~$45 | `eu-west-1` (Ireland) |

> **AWS t3.small has 2 GB RAM.** That's tight - upgrade to `t3.medium` (4 GB, ~$30/mo each) if services OOM.

**OS:** Ubuntu 22.04 LTS on all providers (free, consistent tooling)  
**Disk:** 30 GB SSD per VM (keeps PostgreSQL I/O fast)

### Memory budget per VM

```
PostgreSQL:        ~250 MB (shared_buffers default)
Redis:             ~100 MB (maxmemory 96mb in docker-stack.yml)
5 Go services:     ~150 MB (30 MB each)
nginx:              ~20 MB
OS + Docker:       ~200 MB
─────────────────────────
Total:             ~720 MB   ← fits in 2 GB (t3.small) with ~1.3 GB headroom
Peak (busy):       ~900 MB   ← still safe on 2 GB
```

---

## 2. CLI setup and authentication

### Azure

```bash
# Install
brew install azure-cli          # macOS
winget install Microsoft.AzureCLI  # Windows

# Log in
az login
az account set --subscription "<your-subscription-id>"
```

### GCP

```bash
# Install
brew install --cask google-cloud-sdk  # macOS
# Windows: download installer from cloud.google.com/sdk

# Log in and set project
gcloud auth login
gcloud config set project YOUR_PROJECT_ID
gcloud config set compute/region europe-west1
gcloud config set compute/zone europe-west1-b
```

### AWS

```bash
# Install
brew install awscli   # macOS
winget install Amazon.AWSCLI  # Windows

# Configure (needs IAM user with EC2 + ELB permissions)
aws configure
# AWS Access Key ID:     <your-key>
# AWS Secret Access Key: <your-secret>
# Default region:        eu-west-1
# Default output format: json
```

---

## 3. Provision infrastructure

> **Terraform alternative:** `terraform/azure/`, `terraform/gcp/`, `terraform/aws/`
> do all of the below automatically. See `terraform/README.md`.
> The manual steps below are for understanding what Terraform creates.

### 3.1 SSH key (all providers)

```bash
ssh-keygen -t ed25519 -C "vcs-deploy" -f ~/.ssh/vcs_key
# Public key: ~/.ssh/vcs_key.pub
# Private key: ~/.ssh/vcs_key  (never commit this)
```

---

### Azure

#### 3.2a Resource group + VNet + NSG

```bash
REGION="northeurope"
RG="vcs-rg"

az group create --name $RG --location $REGION

az network vnet create \
  --resource-group $RG --name vcs-vnet \
  --address-prefix 10.0.0.0/16 \
  --subnet-name vcs-subnet --subnet-prefix 10.0.1.0/24

NSG="vcs-nsg"
az network nsg create --resource-group $RG --name $NSG

# SSH - your IP only
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowSSH --priority 100 \
  --source-address-prefixes YOUR_IP/32 \
  --destination-port-ranges 22 --protocol Tcp --access Allow

# HTTP
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowHTTP --priority 110 \
  --source-address-prefixes Internet \
  --destination-port-ranges 80 --protocol Tcp --access Allow

# Swarm + Postgres (internal subnet only)
az network nsg rule create --resource-group $RG --nsg-name $NSG \
  --name AllowInternal --priority 120 \
  --source-address-prefixes 10.0.1.0/24 \
  --destination-port-ranges 2377 7946 4789 5432 \
  --protocol '*' --access Allow
```

#### 3.3a Create 3 VMs

```bash
for i in A B C; do
  az vm create \
    --resource-group $RG \
    --name "vcs-vm-${i}" \
    --image Ubuntu2204 \
    --size Standard_B2s \
    --admin-username deploy \
    --ssh-key-values ~/.ssh/vcs_key.pub \
    --vnet-name vcs-vnet \
    --subnet vcs-subnet \
    --nsg $NSG \
    --public-ip-sku Standard \
    --os-disk-size-gb 30 \
    --storage-sku Premium_LRS \
    --no-wait
done

# Wait, then get IPs
az vm list-ip-addresses --resource-group $RG --output table
```

#### 3.4a Azure Load Balancer

```bash
az network public-ip create \
  --resource-group $RG --name vcs-lb-pip \
  --sku Standard --allocation-method Static

az network lb create \
  --resource-group $RG --name vcs-lb --sku Standard \
  --public-ip-address vcs-lb-pip \
  --frontend-ip-name vcs-frontend \
  --backend-pool-name vcs-backend

az network lb probe create \
  --resource-group $RG --lb-name vcs-lb \
  --name http-probe --protocol Http \
  --port 80 --path /nginx-health \
  --interval 10 --threshold 2

az network lb rule create \
  --resource-group $RG --lb-name vcs-lb \
  --name http-rule --protocol Tcp \
  --frontend-port 80 --backend-port 80 \
  --frontend-ip-name vcs-frontend \
  --backend-pool-name vcs-backend \
  --probe-name http-probe

# Add each VM NIC to the backend pool
for NIC in vcs-vm-AVMNic vcs-vm-BVMNic vcs-vm-CVMNic; do
  az network nic ip-config address-pool add \
    --resource-group $RG --nic-name $NIC \
    --ip-config-name ipconfig1 \
    --lb-name vcs-lb --address-pool vcs-backend
done

# Get LB public IP
az network public-ip show \
  --resource-group $RG --name vcs-lb-pip \
  --query ipAddress -o tsv
```

---

### GCP

#### 3.2b VPC + Firewall rules

```bash
gcloud compute networks create vcs-vpc --subnet-mode=custom

gcloud compute networks subnets create vcs-subnet \
  --network=vcs-vpc \
  --region=europe-west1 \
  --range=10.0.1.0/24

# SSH - your IP only
gcloud compute firewall-rules create vcs-allow-ssh \
  --network=vcs-vpc --allow=tcp:22 \
  --source-ranges=YOUR_IP/32 \
  --target-tags=vcs-node

# HTTP - open
gcloud compute firewall-rules create vcs-allow-http \
  --network=vcs-vpc --allow=tcp:80 \
  --source-ranges=0.0.0.0/0 \
  --target-tags=vcs-node

# Swarm + Postgres (internal subnet only)
gcloud compute firewall-rules create vcs-allow-internal \
  --network=vcs-vpc \
  --allow=tcp:2377,tcp:7946,udp:7946,udp:4789,tcp:5432 \
  --source-ranges=10.0.1.0/24 \
  --target-tags=vcs-node
```

#### 3.3b Create 3 VMs

```bash
for i in 1 2 3; do
  gcloud compute instances create vcs-vm${i} \
    --zone=europe-west1-b \
    --machine-type=e2-medium \
    --image-family=ubuntu-2204-lts \
    --image-project=ubuntu-os-cloud \
    --boot-disk-size=30GB \
    --boot-disk-type=pd-balanced \
    --network-interface="subnet=vcs-subnet,private-network-ip=10.0.1.1${i}" \
    --tags=vcs-node \
    --metadata="ssh-keys=deploy:$(cat ~/.ssh/vcs_key.pub)"
done

# Get external IPs
gcloud compute instances list --filter="name~vcs-vm"
```

#### 3.4b GCP Network Load Balancer

```bash
# Reserve a static external IP
gcloud compute addresses create vcs-lb-ip --region=europe-west1

# Health check
gcloud compute http-health-checks create vcs-health-check \
  --request-path=/nginx-health --port=80 \
  --check-interval=10s --timeout=5s

# Target pool
gcloud compute target-pools create vcs-pool \
  --region=europe-west1 \
  --health-checks=vcs-health-check

gcloud compute target-pools add-instances vcs-pool \
  --instances=vcs-vm1,vcs-vm2,vcs-vm3 \
  --instances-zone=europe-west1-b

# Forwarding rule (binds the static IP to the pool)
LB_IP=$(gcloud compute addresses describe vcs-lb-ip \
  --region=europe-west1 --format='value(address)')

gcloud compute forwarding-rules create vcs-fwd-rule \
  --region=europe-west1 \
  --load-balancing-scheme=EXTERNAL \
  --address=$LB_IP \
  --target-pool=vcs-pool \
  --ports=80

echo "LB IP: $LB_IP"
```

---

### AWS

#### 3.2c VPC + Security Group

```bash
# VPC
VPC_ID=$(aws ec2 create-vpc --cidr-block 10.0.0.0/16 \
  --query 'Vpc.VpcId' --output text)
aws ec2 modify-vpc-attribute --vpc-id $VPC_ID --enable-dns-hostnames

# Internet Gateway
IGW_ID=$(aws ec2 create-internet-gateway --query 'InternetGateway.InternetGatewayId' --output text)
aws ec2 attach-internet-gateway --vpc-id $VPC_ID --internet-gateway-id $IGW_ID

# Subnet (single AZ - all 3 VMs on same subnet for Swarm overlay)
SUBNET_ID=$(aws ec2 create-subnet \
  --vpc-id $VPC_ID --cidr-block 10.0.1.0/24 \
  --availability-zone eu-west-1a \
  --query 'Subnet.SubnetId' --output text)
aws ec2 modify-subnet-attribute --subnet-id $SUBNET_ID --map-public-ip-on-launch

# Route table
RT_ID=$(aws ec2 create-route-table --vpc-id $VPC_ID \
  --query 'RouteTable.RouteTableId' --output text)
aws ec2 create-route --route-table-id $RT_ID \
  --destination-cidr-block 0.0.0.0/0 --gateway-id $IGW_ID
aws ec2 associate-route-table --route-table-id $RT_ID --subnet-id $SUBNET_ID

# Security Group
SG_ID=$(aws ec2 create-security-group \
  --group-name vcs-sg --description "VCS nodes" \
  --vpc-id $VPC_ID --query 'GroupId' --output text)

# SSH - your IP only
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol tcp --port 22 --cidr YOUR_IP/32

# HTTP - open
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol tcp --port 80 --cidr 0.0.0.0/0

# Swarm + Postgres (internal subnet only)
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol tcp --port 2377 --cidr 10.0.1.0/24
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol tcp --port 7946 --cidr 10.0.1.0/24
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol udp --port 7946 --cidr 10.0.1.0/24
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol udp --port 4789 --cidr 10.0.1.0/24
aws ec2 authorize-security-group-ingress --group-id $SG_ID \
  --protocol tcp --port 5432 --cidr 10.0.1.0/24
```

#### 3.3c Create 3 EC2 instances

```bash
# Import SSH key
aws ec2 import-key-pair --key-name vcs-key \
  --public-key-material fileb://~/.ssh/vcs_key.pub

# Get latest Ubuntu 22.04 AMI ID
AMI_ID=$(aws ec2 describe-images \
  --owners 099720109477 \
  --filters 'Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*' \
            'Name=state,Values=available' \
  --query 'sort_by(Images,&CreationDate)[-1].ImageId' \
  --output text)

# Launch 3 instances with fixed private IPs
for i in 1 2 3; do
  ENI_ID=$(aws ec2 create-network-interface \
    --subnet-id $SUBNET_ID \
    --private-ip-address "10.0.1.1${i}" \
    --groups $SG_ID \
    --query 'NetworkInterface.NetworkInterfaceId' --output text)

  aws ec2 run-instances \
    --image-id $AMI_ID \
    --instance-type t3.small \
    --key-name vcs-key \
    --network-interfaces "NetworkInterfaceId=${ENI_ID},DeviceIndex=0" \
    --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=30,VolumeType=gp3}' \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=vcs-vm${i}}]"
done

# Allocate and associate Elastic IPs (stable public IPs for SSH)
INSTANCE_IDS=$(aws ec2 describe-instances \
  --filters 'Name=tag:Name,Values=vcs-vm*' 'Name=instance-state-name,Values=running' \
  --query 'Reservations[*].Instances[*].InstanceId' --output text)

for INSTANCE_ID in $INSTANCE_IDS; do
  EIP=$(aws ec2 allocate-address --domain vpc --query 'AllocationId' --output text)
  aws ec2 associate-address --instance-id $INSTANCE_ID --allocation-id $EIP
done

# List IPs
aws ec2 describe-instances \
  --filters 'Name=tag:Name,Values=vcs-vm*' \
  --query 'Reservations[*].Instances[*].[Tags[?Key==`Name`].Value|[0],PublicIpAddress,PrivateIpAddress]' \
  --output table
```

#### 3.4c AWS Network Load Balancer

```bash
# Create NLB
NLB_ARN=$(aws elbv2 create-load-balancer \
  --name vcs-nlb --type network \
  --subnets $SUBNET_ID \
  --query 'LoadBalancers[0].LoadBalancerArn' --output text)

NLB_DNS=$(aws elbv2 describe-load-balancers \
  --load-balancer-arns $NLB_ARN \
  --query 'LoadBalancers[0].DNSName' --output text)

# Target group (HTTP health check on /nginx-health)
TG_ARN=$(aws elbv2 create-target-group \
  --name vcs-http-tg --protocol TCP --port 80 \
  --vpc-id $VPC_ID --target-type instance \
  --health-check-protocol HTTP \
  --health-check-path /nginx-health \
  --health-check-interval-seconds 10 \
  --query 'TargetGroups[0].TargetGroupArn' --output text)

# Register all 3 instances
for INSTANCE_ID in $INSTANCE_IDS; do
  aws elbv2 register-targets \
    --target-group-arn $TG_ARN \
    --targets Id=$INSTANCE_ID,Port=80
done

# Listener
aws elbv2 create-listener \
  --load-balancer-arn $NLB_ARN \
  --protocol TCP --port 80 \
  --default-actions Type=forward,TargetGroupArn=$TG_ARN

echo "LB DNS: $NLB_DNS"
# Point Cloudflare CNAME to this DNS name
```

---

## 4. Install Docker on all 3 VMs

Run on **each VM** (SSH: `ssh -i ~/.ssh/vcs_key deploy@<PUBLIC-IP>`,
or `ubuntu@` for AWS):

```bash
sudo apt-get update -y
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update -y
sudo apt-get install -y docker-ce docker-ce-cli containerd.io \
  docker-buildx-plugin docker-compose-plugin

sudo usermod -aG docker $USER
# Log out and back in for group change to take effect
```

---

## 5. Initialize Docker Swarm

All 3 VMs join as **managers** - Raft quorum is 2/3, so the cluster survives
losing any single node and automatically elects a new leader within ~5 seconds.

### On VM-A only (becomes the initial leader):

```bash
PRIVATE_IP=$(hostname -I | awk '{print $1}')   # e.g. 10.0.1.11
docker swarm init --advertise-addr $PRIVATE_IP

# Save MANAGER join token (VM-B and VM-C need this)
docker swarm join-token manager -q > /root/swarm-manager-token
cat /root/swarm-manager-token
# e.g. SWMTKN-1-abc...xyz
```

### On VM-B and VM-C - join as managers:

```bash
# Get the token from VM-A first:
#   ssh deploy@<VM-A-IP> cat /root/swarm-manager-token

docker swarm join --token SWMTKN-1-abc...xyz <VM-A-PRIVATE-IP>:2377
```

### Verify on VM-A:

```bash
docker node ls
# ID                STATUS    AVAILABILITY   MANAGER STATUS
# abc123 *          Ready     Active         Leader
# def456            Ready     Active         Reachable
# ghi789            Ready     Active         Reachable
```

All three nodes must show a `MANAGER STATUS`. If VM-A goes down, one of the
`Reachable` nodes is automatically promoted to `Leader` by Raft.

---

## 6. Create Docker secrets

Run on **VM-A** (secrets are distributed to all nodes by Swarm automatically):

```bash
# Database password
echo "YourStrongPassword123!" | docker secret create db_password -

# JWT signing secret (32+ characters)
openssl rand -base64 32 | docker secret create jwt_secret -

# Firebase service account (download from Firebase console)
cat /path/to/firebase-service-account.json | docker secret create firebase_credentials -
```

---

## 7. GHCR authentication on all nodes

```bash
# On each VM (A, B, C):
echo $GITHUB_PAT | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

`$GITHUB_PAT` = GitHub Personal Access Token with `read:packages` scope.

Alternatively, pass `--with-registry-auth` when deploying the stack (section 8)
and the manager distributes credentials automatically.

---

## 8. Deploy the stack

On **VM-A**:

```bash
git clone https://github.com/YOUR_ORG/distributed-vehicle-capacity-system.git /opt/vcs
cd /opt/vcs

export GITHUB_REPOSITORY="your-org/distributed-vehicle-capacity-system"
export IMAGE_TAG="latest"

docker stack deploy \
  --with-registry-auth \
  --prune \
  -c docker-stack.yml \
  vcs

# Watch services come up
watch docker service ls
# All services should reach 3/3 replicas within ~2 minutes
```

---

## 9. PostgreSQL logical replication

This runs inside each VM's PostgreSQL container. Do this **once** after first deploy.

### 9.1 Configure PostgreSQL for logical replication

On **each VM**:

```bash
PG_CONTAINER=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec -it $PG_CONTAINER bash

echo "wal_level = logical
max_replication_slots = 10
max_wal_senders = 10
listen_addresses = '*'" >> /var/lib/postgresql/data/postgresql.conf

echo "host replication replicator 10.0.1.0/24 md5
host all all 10.0.1.0/24 md5" >> /var/lib/postgresql/data/pg_hba.conf

exit
docker service update --force vcs_db
```

### 9.2 Create replication user and publication

On **each VM**:

```bash
PG_CONTAINER=$(docker ps --filter name=vcs_db --format '{{.Names}}')
docker exec -it $PG_CONTAINER psql -U postgres trafficservice
```

```sql
CREATE USER replicator WITH REPLICATION LOGIN PASSWORD 'ReplicatorPass123!';
GRANT SELECT ON ALL TABLES IN SCHEMA journey TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA capacity TO replicator;
GRANT SELECT ON ALL TABLES IN SCHEMA iam TO replicator;
CREATE PUBLICATION vcs_pub FOR ALL TABLES;
```

### 9.3 Create subscriptions (all-to-all)

Private IPs used below: VM-A = `10.0.1.11`, VM-B = `10.0.1.12`, VM-C = `10.0.1.13`
(adjust if your provider assigned different IPs - check with `hostname -I`).

**On VM-A:**
```sql
CREATE SUBSCRIPTION vcs_sub_vmb
  CONNECTION 'host=10.0.1.12 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmc
  CONNECTION 'host=10.0.1.13 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

**On VM-B:**
```sql
CREATE SUBSCRIPTION vcs_sub_vma
  CONNECTION 'host=10.0.1.11 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmc
  CONNECTION 'host=10.0.1.13 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

**On VM-C:**
```sql
CREATE SUBSCRIPTION vcs_sub_vma
  CONNECTION 'host=10.0.1.11 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;

CREATE SUBSCRIPTION vcs_sub_vmb
  CONNECTION 'host=10.0.1.12 port=5432 dbname=trafficservice user=replicator password=ReplicatorPass123!'
  PUBLICATION vcs_pub;
```

### 9.4 Verify replication

```sql
-- On any VM:
SELECT * FROM pg_stat_subscription;
-- Should show 2 rows, status = 'streaming'

SELECT * FROM pg_stat_replication;
-- Shows your subscribers (the other 2 VMs)
```

Write a row on VM-A and confirm it appears on VM-B within < 100 ms.

---

## 10. Cloudflare CDN - frontend + API routing

### Architecture

```
Browser (anywhere in the world)
    │
    ├── GET https://vcs-app.com/
    │       → Cloudflare edge (nearest PoP) serves cached React SPA
    │
    └── POST https://api.vcs-app.com/api/v1/journeys
            → Cloudflare passes through (no API caching)
            → Cloud Load Balancer
            → One of 3 VMs (round-robin)
            → nginx → journey-service:8083
```

### 10.1 Deploy frontend to Cloudflare Pages (free)

```bash
cd frontend && npm run build

npm install -g wrangler
wrangler login

wrangler pages deploy dist \
  --project-name vcs-frontend \
  --branch main
```

### 10.2 Configure API subdomain

In **Cloudflare DNS**, add an A record (Azure/GCP - use the LB public IP)
or a CNAME record (AWS - use the NLB DNS name):

| Provider | Record type | Name | Value |
|----------|-------------|------|-------|
| Azure | A | `api` | `<LB public IP>` |
| GCP | A | `api` | `<LB public IP>` |
| AWS | CNAME | `api` | `<NLB DNS name>` |

Set **Proxied = ON** (orange cloud) - hides VM IPs, enables free HTTPS on `api.vcs-app.com`.

### 10.3 Update frontend API base URL

In `frontend/src/app/services/journeyApi.ts`:
```ts
const BASE_URL = 'https://api.vcs-app.com';
```

### 10.4 Update nginx CORS

In `nginx/nginx.conf`:
```nginx
add_header Access-Control-Allow-Origin "https://vcs-app.com";
add_header Access-Control-Allow-Methods "GET, POST, PUT, OPTIONS";
add_header Access-Control-Allow-Headers "Authorization, Content-Type, Idempotency-Key";
```

---

## 11. Cost summary

### Azure (northeurope - Dublin)

| Resource | SKU | Monthly cost |
|----------|-----|-------------|
| 3 × VM Standard_B2s | 2 vCPU / 4 GB | ~€90 |
| 3 × Premium SSD 30 GB | P4 | ~€15 |
| Standard Load Balancer | hourly | ~€16 |
| 4 Public IPs (3 VMs + LB) | Standard static | ~€12 |
| Egress ~10 GB | West EU | ~€1 |
| **Total** | | **~€134/mo** |

> Stop VMs overnight to save ~60% compute cost: `az vm deallocate --name vcs-vm-A ...`

### GCP (europe-west1 - Belgium)

| Resource | SKU | Monthly cost |
|----------|-----|-------------|
| 3 × e2-medium | 2 vCPU / 4 GB | ~$75 |
| 3 × pd-balanced 30 GB | | ~$18 |
| Network Load Balancer | forwarding rule | ~$18 |
| 1 Static external IP (LB) | | ~$7 |
| Egress ~10 GB | EU | ~$1 |
| **Total** | | **~$119/mo** |

> Preemptible VMs (70% discount) not suitable - Swarm managers must be stable.

### AWS (eu-west-1 - Ireland)

| Resource | SKU | Monthly cost |
|----------|-----|-------------|
| 3 × t3.small | 2 vCPU / 2 GB | ~$46 |
| 3 × gp3 30 GB | | ~$9 |
| Network Load Balancer | hourly | ~$16 |
| 3 Elastic IPs | (free when attached) | $0 |
| Egress ~10 GB | EU | ~$1 |
| **Total** | | **~$72/mo** |

> Upgrade to t3.medium (+$45/mo) if memory is tight.

---

## 12. Verification checklist

Run these after setup to confirm everything works:

```bash
# 1. All 3 nodes are Swarm managers
docker node ls

# 2. All services running (3/3 replicas each)
docker service ls

# 3. Postgres replication active (2 subscriptions per VM)
docker exec -it $(docker ps -q -f name=vcs_db) \
  psql -U postgres -c "SELECT subname, subenabled, received_lsn FROM pg_stat_subscription;"

# 4. LB health probe passing
curl http://<LB-IP-or-DNS>/nginx-health
# → "ok"

# 5. API reachable through Cloudflare
curl https://api.vcs-app.com/health

# 6. Frontend loads from CDN
curl -I https://vcs-app.com
# CF-Cache-Status: HIT  ← Cloudflare serving from edge

# 7. End-to-end booking
curl -X POST https://api.vcs-app.com/api/v1/journeys \
  -H "Authorization: Bearer <JWT>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{"origin":{"lat":53.3498,"lng":-6.2603},"destination":{"lat":51.8985,"lng":-8.4756},"departure_time":"2026-04-16T10:00:00Z","vehicle_type":"car"}'
# → 201 {"journey_id": "...", "status": "APPROVED"}
```

---

## 13. Full request flow - end to end

```
1. Driver opens https://vcs-app.com on their phone
   → Cloudflare edge (nearest PoP) serves React SPA from cache in ~200ms

2. Driver submits booking
   → POST https://api.vcs-app.com/api/v1/journeys
   → Cloudflare passes through to origin
   → Cloud LB routes to e.g. VM-B (round-robin)
   → VM-B nginx → journey-service:8083
       a. JWT validation (local HMAC, <1ms)
       b. Active journey check (local postgres, 5ms)
       c. Redis route cache hit → skips Map Service
       d. capacity-service:8081 (same VM, Docker overlay, <2ms)
       e. SELECT FOR UPDATE → reserve slots (20ms)
       f. INSERT journey (10ms)
       g. 201 APPROVED → browser (~94ms p50, ~300ms p99)

3. Async (after response sent):
   → Redis Streams → notification-service → Firebase push to driver

4. PostgreSQL on VM-B replicates new row to VM-A and VM-C in <100ms
   → Driver can query their journey from any VM immediately after
```
