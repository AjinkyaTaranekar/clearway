# Terraform — Cloud-Agnostic Infrastructure

Three provider-specific configurations. Pick one based on which cloud you end up using.

| Provider | Directory | Best region | VM spec | Est. cost/mo |
|----------|-----------|-------------|---------|--------------|
| **Azure** | `azure/` | `northeurope` (Dublin) | Standard_B2s — 2 vCPU / 4 GB | ~£30 × 3 VMs |
| **GCP** | `gcp/` | `europe-west1` (Belgium) | e2-medium — 2 vCPU / 4 GB | ~$25 × 3 VMs |
| **AWS** | `aws/` | `eu-west-1` (Ireland) | t3.small — 2 vCPU / 2 GB | ~$15 × 3 VMs |

> **AWS t3.small has only 2 GB RAM.** If containers OOM, upgrade to `t3.medium` (4 GB, ~$30/mo).

## What each config provisions

- 3 VMs with fixed internal IPs: `10.0.1.11`, `10.0.1.12`, `10.0.1.13`
- VNet/VPC + subnet `10.0.1.0/24`
- Security group / NSG / firewall rules:
  - Port 22 (SSH) — restricted to `management_cidr`
  - Port 80 (HTTP) — open
  - Ports 2377, 7946, 4789 (Docker Swarm) — internal subnet only
  - Port 5432 (PostgreSQL) — internal subnet only
- Public Load Balancer on port 80 (health-checked via `/nginx-health`)
- VM bootstrap script that installs Docker CE and initialises Swarm on VM1

## Usage

```bash
# 1. Copy example vars
cd terraform/azure        # or gcp/ or aws/
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your credentials / SSH key path / management IP

# 2. Init and apply
terraform init
terraform plan
terraform apply

# 3. Get outputs
terraform output
# → load_balancer_public_ip / load_balancer_dns
# → vm_public_ips / ssh_commands
```

## After apply — complete Swarm setup

Terraform installs Docker and initialises the Swarm manager on VM1. Workers must join manually (timing of cloud-init is non-deterministic):

```bash
# On VM1:
ssh vcsadmin@<vm1-ip>
cat /root/swarm-worker-token   # prints the join token

# On VM2 and VM3:
docker swarm join --token <token> 10.0.1.11:2377
```

For the full setup walkthrough (Postgres replication, secret creation, stack deploy) see `docs/infrastructure-setup.md`.

## State file

Terraform state contains sensitive data (IPs, resource IDs). Store it remotely:

- **Azure**: use azurerm backend (Azure Blob Storage)
- **GCP**: use gcs backend (Cloud Storage bucket)
- **AWS**: use s3 backend (S3 + DynamoDB for locking)

Add a `backend.tf` in your chosen provider directory before running `terraform init` in production.

## .gitignore

The following are already git-ignored (add to your `.gitignore` if not present):

```
terraform/.terraform/
terraform/**/.terraform/
terraform/**/*.tfstate
terraform/**/*.tfstate.backup
terraform/**/terraform.tfvars
```
