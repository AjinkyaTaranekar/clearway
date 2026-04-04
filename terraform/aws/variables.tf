variable "region" {
  description = "AWS region. eu-west-1 = Ireland — ideal for this project."
  type        = string
  default     = "eu-west-1"
}

variable "availability_zone" {
  description = "AZ within the region. All 3 VMs are in the same AZ for Swarm overlay performance."
  type        = string
  default     = "eu-west-1a"
}

variable "instance_type" {
  description = "EC2 instance type. t3.small = 2 vCPU / 2 GB (~$15/mo). t3.medium = 2 vCPU / 4 GB (~$30/mo)."
  type        = string
  default     = "t3.small"
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

variable "tags" {
  description = "Tags applied to all resources"
  type        = map(string)
  default = {
    project     = "distributed-vehicle-capacity-system"
    environment = "prototype"
  }
}
