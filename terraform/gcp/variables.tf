variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region. europe-west1 = Belgium (lowest latency from Ireland). europe-west2 = London."
  type        = string
  default     = "europe-west1"
}

variable "zone" {
  description = "GCP zone within the region"
  type        = string
  default     = "europe-west1-b"
}

variable "machine_type" {
  description = "GCP machine type. e2-medium = 2 vCPU / 4 GB RAM (~$25/mo). e2-standard-2 = 2 vCPU / 8 GB."
  type        = string
  default     = "e2-medium"
}

variable "admin_username" {
  description = "SSH user on each VM"
  type        = string
  default     = "vcsadmin"
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key file"
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "management_cidr" {
  description = "CIDR allowed to SSH. Set to your IP/32 in production."
  type        = string
  default     = "0.0.0.0/0"
}
