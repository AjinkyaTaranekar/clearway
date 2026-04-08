output "load_balancer_public_ip" {
  description = "Public IP of the GCP Network Load Balancer - point your DNS / Cloudflare proxy here"
  value       = google_compute_address.lb.address
}

output "vm_external_ips" {
  description = "Per-VM external IPs for SSH (ephemeral - reassigned on stop/start)"
  value = {
    for i, vm in google_compute_instance.vm :
    "vm${i + 1}" => vm.network_interface[0].access_config[0].nat_ip
  }
}

output "vm_internal_ips" {
  description = "Per-VM internal IPs used for Swarm overlay and Postgres replication"
  value = {
    for i, addr in google_compute_address.vm_internal :
    "vm${i + 1}" => addr.address
  }
}

output "ssh_commands" {
  description = "SSH commands to access each VM"
  value = {
    for i, vm in google_compute_instance.vm :
    "vm${i + 1}" => "ssh ${var.admin_username}@${vm.network_interface[0].access_config[0].nat_ip}"
  }
}
