#!/bin/bash
# Bootstraps Docker + Swarm on VM${vm_index}
set -e

# Install Docker CE
curl -fsSL https://get.docker.com | sh
systemctl enable docker
systemctl start docker
usermod -aG docker ubuntu

%{ if is_manager }
# VM1 — initialise Swarm manager
docker swarm init --advertise-addr ${manager_ip}
# Save worker join token so other VMs can retrieve it via SSH
docker swarm join-token worker -q > /root/swarm-worker-token
%{ else }
# VM2/3 — placeholder; join Swarm manually after manager is up
# SSH to VM1: cat /root/swarm-worker-token
# Then here: docker swarm join --token <token> ${manager_ip}:2377
echo "Worker VM${vm_index} ready — join Swarm manually"
%{ endif }
