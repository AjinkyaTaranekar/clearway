output "load_balancer_dns" {
  description = "DNS name of the AWS NLB — point your DNS CNAME / Cloudflare proxy here"
  value       = aws_lb.vcs.dns_name
}

output "vm_elastic_ips" {
  description = "Per-VM Elastic IPs for SSH access (stable across stops)"
  value = {
    for i, eip in aws_eip.vm :
    "vm${i + 1}" => eip.public_ip
  }
}

output "vm_private_ips" {
  description = "Per-VM private IPs (10.0.1.11–13) used for Swarm overlay and Postgres replication"
  value = {
    for i, eni in aws_network_interface.vm :
    "vm${i + 1}" => eni.private_ip
  }
}

output "ssh_commands" {
  description = "SSH commands to access each VM"
  value = {
    for i, eip in aws_eip.vm :
    "vm${i + 1}" => "ssh ubuntu@${eip.public_ip}"
  }
}
