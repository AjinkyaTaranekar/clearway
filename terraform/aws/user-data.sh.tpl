#!/bin/bash
# Bootstraps Docker + Swarm on VM${vm_index}
set -e

# Install Docker CE
curl -fsSL https://get.docker.com | sh
systemctl enable docker
systemctl start docker
usermod -aG docker ubuntu

%{ if is_manager }
# VM1 - initialise Swarm manager
docker swarm init --advertise-addr ${manager_ip}
# Save MANAGER join token - VM2 and VM3 join as managers for Raft quorum
docker swarm join-token manager -q > /root/swarm-manager-token
# Also save worker token for any future non-manager nodes
docker swarm join-token worker -q > /root/swarm-worker-token
%{ else }
# VM2/3 - join as MANAGER (not worker) so Raft can elect a new leader if VM1 goes down.
# Must run after VM1 user-data completes (~60s). In practice:
#   ssh ubuntu@<vm1-ip> cat /root/swarm-manager-token
#   docker swarm join --token <manager-token> ${manager_ip}:2377
echo "Manager VM${vm_index} ready - join Swarm as manager manually after VM1 initialises"
%{ endif }
