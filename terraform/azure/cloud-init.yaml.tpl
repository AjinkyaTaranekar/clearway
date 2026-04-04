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
  # VM1 — initialise Swarm manager
  - docker swarm init --advertise-addr ${manager_ip}
  # Save join token to a well-known path so workers can retrieve it via SSH
  - docker swarm join-token worker -q > /root/swarm-worker-token
  %{ else }
  # VM2/3 — join Swarm (run after VM1 is ready; provisioner must wait ~60s)
  # In practice: SSH to VM1, cat /root/swarm-worker-token, then run:
  #   docker swarm join --token <token> ${manager_ip}:2377
  # The infrastructure-setup.md documents the manual join steps.
  - echo "Worker VM ready — run swarm join manually after manager initialises"
  %{ endif }
