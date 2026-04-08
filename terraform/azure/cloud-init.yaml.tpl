#cloud-config
# Bootstraps Docker + Swarm on VM${vm_index}
# Template variables injected by Terraform: is_manager, manager_ip, vm_index

package_update: true
package_upgrade: false

packages:
  - apt-transport-https
  - ca-certificates
  - curl
  - gnupg
  - lsb-release
  - jq

runcmd:
  # Install Docker CE
  - curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
  - echo "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" > /etc/apt/sources.list.d/docker.list
  - apt-get update -qq
  - apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
  - systemctl enable docker
  - systemctl start docker
  %{ if is_manager }
  # VM1 - initialise Swarm manager
  - docker swarm init --advertise-addr ${manager_ip}
  # Save MANAGER join token - VM2 and VM3 join as managers for Raft quorum
  - docker swarm join-token manager -q > /root/swarm-manager-token
  # Also save worker token for any future non-manager nodes
  - docker swarm join-token worker -q > /root/swarm-worker-token
  %{ else }
  # VM2/3 - join as MANAGER (not worker) so Raft can elect a new leader if VM1 goes down.
  # Must run after VM1 cloud-init completes (~60s). In practice:
  #   ssh vcsadmin@<vm1-ip> cat /root/swarm-manager-token
  #   docker swarm join --token <manager-token> ${manager_ip}:2377
  # The infrastructure-setup.md documents the full manual join steps.
  - echo "Manager VM${vm_index} ready - join Swarm as manager manually after VM1 initialises"
  %{ endif }
