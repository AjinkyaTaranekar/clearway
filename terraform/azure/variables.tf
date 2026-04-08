variable "subscription_id" {
  description = "Azure subscription ID"
  type        = string
}

variable "resource_group_name" {
  description = "Name of the Azure resource group"
  type        = string
  default     = "vcs-rg"
}

variable "location" {
  description = "Azure region (choose one close to Ireland: northeurope = Dublin)"
  type        = string
  default     = "northeurope"
}

variable "vm_size" {
  description = "Azure VM SKU. Standard_B2s = 2 vCPU / 4 GB RAM (~£30/mo). Upgrade to Standard_B2ms (8GB) if needed."
  type        = string
  default     = "Standard_B2s"
}

variable "admin_username" {
  description = "SSH admin user on each VM"
  type        = string
  default     = "vcsadmin"
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key file (e.g. ~/.ssh/id_ed25519.pub)"
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "management_cidr" {
  description = "CIDR allowed to SSH. Set to your IP/32 (e.g. 203.0.113.5/32). Default opens to all - tighten in production."
  type        = string
  default     = "*"
}

variable "tags" {
  description = "Tags applied to all resources"
  type        = map(string)
  default = {
    project     = "distributed-vehicle-capacity-system"
    environment = "prototype"
  }
}
