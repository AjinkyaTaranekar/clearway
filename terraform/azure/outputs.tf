output "load_balancer_public_ip" {
  description = "Public IP of the Azure Load Balancer — point your DNS / Cloudflare proxy here"
  value       = azurerm_public_ip.lb.ip_address
}

output "vm_public_ips" {
  description = "Per-VM public IPs for SSH access"
  value       = { for i, pip in azurerm_public_ip.vm : "vm${i + 1}" => pip.ip_address }
}

output "vm_private_ips" {
  description = "Per-VM private IPs (10.0.1.11–13) used for Swarm overlay and Postgres replication"
  value       = { for i, nic in azurerm_network_interface.vm : "vm${i + 1}" => nic.private_ip_address }
}

output "ssh_commands" {
  description = "SSH commands to access each VM"
  value = {
    for i, pip in azurerm_public_ip.vm :
    "vm${i + 1}" => "ssh ${var.admin_username}@${pip.ip_address}"
  }
}
