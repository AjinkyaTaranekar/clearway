# Debugging Guide: Docker Swarm on GCP

## 1. SSH into a VM

```bash
# Format: gcloud compute ssh <vm-name> --project=<project> --zone=<zone>
gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b
gcloud compute ssh vcs-vm-us1 --project=distributed-capacity-system --zone=us-east1-d
gcloud compute ssh vcs-vm-ap1 --project=distributed-capacity-system --zone=asia-east1-b
```

Run a command without interactive SSH:

```bash
gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b \
  --command="sudo docker service ls"
```

## 2. Service Status

```bash
# All services and replica counts
sudo docker service ls

# Detailed task history for a specific service (shows failures + errors)
sudo docker service ps vcs_journey-service --no-trunc

# Filter to only failed tasks
sudo docker service ps vcs_journey-service --filter desired-state=shutdown --no-trunc
```

## 3. Reading Logs

```bash
# Last 50 lines from a service (all replicas combined)
sudo docker service logs vcs_journey-service --tail 50

# Follow logs in real time
sudo docker service logs vcs_journey-service --follow

# With timestamps
sudo docker service logs vcs_journey-service --tail 50 --timestamps

# From a specific container (not service)
CONTAINER=$(sudo docker ps --filter name=vcs_journey --format "{{.ID}}" | head -1)
sudo docker logs $CONTAINER --tail 50
sudo docker logs $CONTAINER --follow
```

## 4. Inspecting a Running Container

```bash
# Get container ID
CONTAINER=$(sudo docker ps --filter name=vcs_journey --format "{{.ID}}" | head -1)

# Run commands inside it
sudo docker exec $CONTAINER env                           # see all env vars
sudo docker exec $CONTAINER cat /app/config.yaml         # read a file
sudo docker exec $CONTAINER cat /run/secrets/jwt_secret  # read a secret

# Open an interactive shell (if sh/bash is available)
sudo docker exec -it $CONTAINER sh
```

## 5. CockroachDB Specific

```bash
CRDB=$(sudo docker ps --filter name=vcs_db --format "{{.ID}}" | head -1)

# Interactive SQL shell
sudo docker exec -it $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257

# Run a one-liner
sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \
  --execute="SHOW DATABASES;"

# Against a specific database
sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \
  --database=trafficservice \
  --execute="SHOW SCHEMAS;"

# As a specific user
sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \
  --database=trafficservice --user=postgres \
  --execute="SHOW GRANTS ON SCHEMA journey;"

# Pipe a SQL file in
sudo docker exec -i $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \
  --database=trafficservice < migration.sql

# Node/cluster status
sudo docker exec $CRDB /cockroach/cockroach node status --insecure --host=localhost:26257
```

## 6. Networking and Connectivity

```bash
# List overlay networks
sudo docker network ls | grep vcs

# Test if a service hostname resolves from within the overlay network
sudo docker run --rm --network vcs_vcs-internal busybox nslookup db
sudo docker run --rm --network vcs_vcs-internal busybox wget -qO- http://iam-service:8082/health

# Test postgres connection from overlay network (simulates what Go app does)
sudo docker run --rm --network vcs_vcs-internal postgres:15-alpine \
  psql "postgresql://postgres@db:26257/trafficservice?sslmode=disable" -c "SELECT 1;"

# Check if a port is open on the host
curl -sf http://localhost:8080/health   # CockroachDB Admin UI
curl -sf http://localhost/nginx-health  # nginx
```

## 7. Secrets and Configs

```bash
# List secrets in the swarm
sudo docker secret ls

# Read a secret from inside a running container (secrets mount at /run/secrets/)
CONTAINER=$(sudo docker ps --filter name=vcs_iam --format "{{.ID}}" | head -1)
sudo docker exec $CONTAINER cat /run/secrets/jwt_secret

# List swarm configs
sudo docker config ls

# Inspect a config
sudo docker config inspect nginx_conf --pretty
```

## 8. Diagnosing a Failing Service

Workflow when a service shows `0/1` or keeps restarting:

1. See the error message.

```bash
sudo docker service ps vcs_journey-service --no-trunc | grep -i shutdown
```

2. Read the logs.

```bash
sudo docker service logs vcs_journey-service --tail 30
```

3. Check if the image can be pulled.

```bash
sudo docker pull ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest
```

4. Run the image manually to reproduce the crash.

```bash
sudo docker run --rm \
  --network vcs_vcs-internal \
  -e VCS_DATABASE_MASTER_HOST=db \
  -e VCS_DATABASE_MASTER_PORT=26257 \
  ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest
```

5. Force restart a stuck service.

```bash
sudo docker service update --force vcs_journey-service
```

## 9. Stack Management

```bash
# Redeploy stack (pick up config changes, prune removed services)
sudo GITHUB_REPOSITORY=ajinkyataranekar/distributed-vehicle-capacity-system \
  IMAGE_TAG=latest \
  CRDB_JOIN="35.187.121.12:26257,34.76.63.61:26257" \
  docker stack deploy --with-registry-auth --prune -c ~/vcs/docker-stack.yml vcs

# Remove a single service (e.g. to change its mode)
sudo docker service rm vcs_db

# Scale a service manually
sudo docker service update --replicas 2 vcs_iam-service

# Update image of a single service
sudo docker service update \
  --image ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest \
  --with-registry-auth \
  vcs_journey-service
```

## 10. Quick Reference: VM Zones

| VM         | Zone           | IP             |
|------------|----------------|----------------|
| vcs-vm-eu1 | europe-west1-b | 35.187.121.12  |
| vcs-vm-eu2 | europe-west1-b | 34.76.63.61    |
| vcs-vm-us1 | us-east1-d     | 34.138.242.217 |
| vcs-vm-ap1 | asia-east1-b   | 34.80.180.64   |
Debugging Guide: Docker Swarm on GCP       
                                             
  1. SSH into a VM                                                  
  # Format: gcloud compute ssh <vm-name> --project=<project> --zone=<zone>                              
  gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b             
  gcloud compute ssh vcs-vm-us1 --project=distributed-capacity-system --zone=us-east1-d                 
  gcloud compute ssh vcs-vm-ap1 --project=distributed-capacity-system --zone=asia-east1-b               
                                             
  Run a command without interactive SSH:     
  gcloud compute ssh vcs-vm-eu1 --project=distributed-capacity-system --zone=europe-west1-b \           
    --command="sudo docker service ls"       
                                             
  ---                                        
  2. Service Status                                                 
  # All services and replica counts       
  sudo docker service ls                                            
  # Detailed task history for a specific service (shows failures + errors)                              
  sudo docker service ps vcs_journey-service --no-trunc                                                 
                                             
  # Filter to only failed tasks              
  sudo docker service ps vcs_journey-service --filter desired-state=shutdown --no-trunc                 
                                             
  ---                                        
  3. Reading Logs                                                
  # Last 50 lines from a service (all replicas combined)                                                
  sudo docker service logs vcs_journey-service --tail 50                                                
                                             
  # Follow logs in real time                 
  sudo docker service logs vcs_journey-service --follow                                                 
                                             
  # With timestamps                          
  sudo docker service logs vcs_journey-service --tail 50 --timestamps                                   
                                             
  # From a specific container (not service)  
  CONTAINER=$(sudo docker ps --filter name=vcs_journey --format "{{.ID}}" | head -1)                    
  sudo docker logs $CONTAINER --tail 50      
  sudo docker logs $CONTAINER --follow       
                                             
  ---                                     
  4. Inspecting a Running Container                                 
  # Get container ID                         
  CONTAINER=$(sudo docker ps --filter name=vcs_journey --format "{{.ID}}" | head -1)                    
                                          
  # Run a command inside it
  sudo docker exec $CONTAINER env                        # see all env vars                             
  sudo docker exec $CONTAINER cat /app/config.yaml      # read a file                                   
  sudo docker exec $CONTAINER cat /run/secrets/jwt_secret  # read a secret                              
                                             
  # Open an interactive shell (if sh/bash is available)                                                 
  sudo docker exec -it $CONTAINER sh         
                                             
  ---                                        
  5. CockroachDB Specific                                        
  CRDB=$(sudo docker ps --filter name=vcs_db --format "{{.ID}}" | head -1)                              
                                             
  # Interactive SQL shell                    
  sudo docker exec -it $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257                 
                                             
  # Run a one-liner                          
  sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \                   
    --execute="SHOW DATABASES;"                                     
  # Against a specific database              
  sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \                   
    --database=trafficservice \              
    --execute="SHOW SCHEMAS;"                                       
  # As a specific user                       
  sudo docker exec $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \                   
    --database=trafficservice --user=postgres \                                                         
    --execute="SHOW GRANTS ON SCHEMA journey;"                                                          
                                             
  # Pipe a SQL file in                       
  sudo docker exec -i $CRDB /cockroach/cockroach sql --insecure --host=localhost:26257 \                
    --database=trafficservice < migration.sql
                                             
  # Node/cluster status                      
  sudo docker exec $CRDB /cockroach/cockroach node status --insecure --host=localhost:26257             
                                             
  ---                                        
  6. Networking & Connectivity                                   
  # List overlay networks
  sudo docker network ls | grep vcs                                 
  # Test if a service hostname resolves from within the overlay network                                 
  sudo docker run --rm --network vcs_vcs-internal busybox nslookup db                                   
  sudo docker run --rm --network vcs_vcs-internal busybox wget -qO- http://iam-service:8082/health      
                                             
  # Test postgres connection from overlay network (simulates what Go app does)                          
  sudo docker run --rm --network vcs_vcs-internal postgres:15-alpine \                                  
    psql "postgresql://postgres@db:26257/trafficservice?sslmode=disable" -c "SELECT 1;"                 
                                             
  # Check if a port is open on the host      
  curl -sf http://localhost:8080/health   # CockroachDB Admin UI                                        
  curl -sf http://localhost/nginx-health  # nginx                                                       
                                             
  ---                                        
  7. Secrets & Configs                                              
  # List secrets in the swarm                
  sudo docker secret ls                                             
  # Read a secret from inside a running container (secrets mount at /run/secrets/)                      
  CONTAINER=$(sudo docker ps --filter name=vcs_iam --format "{{.ID}}" | head -1)                        
  sudo docker exec $CONTAINER cat /run/secrets/jwt_secret                                               
                                             
  # List swarm configs                       
  sudo docker config ls                                             
  # Inspect a config                      
  sudo docker config inspect nginx_conf --pretty                                                        
                                             
  ---                                        
  8. Diagnosing a Failing Service                                
  Workflow when a service shows 0/1 or keeps restarting:                                                
                                             
  # Step 1: see the error message            
  sudo docker service ps vcs_journey-service --no-trunc | grep -i shutdown                              
                                             
  # Step 2: read the logs                    
  sudo docker service logs vcs_journey-service --tail 30                                                
                                             
  # Step 3: check if the image can even be pulled                                                       
  sudo docker pull ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest  
                                             
  # Step 4: run the image manually to reproduce the crash                                               
  sudo docker run --rm \                     
    --network vcs_vcs-internal \             
    -e VCS_DATABASE_MASTER_HOST=db \         
    -e VCS_DATABASE_MASTER_PORT=26257 \      
    ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest                 
                                             
  # Step 5: force restart a stuck service    
  sudo docker service update --force vcs_journey-service                                                
                                          
  ---
  9. Stack Management                                               
  # Redeploy stack (pick up config changes, prune removed services)                                     
  sudo GITHUB_REPOSITORY=ajinkyataranekar/distributed-vehicle-capacity-system \                         
       IMAGE_TAG=latest \                    
       CRDB_JOIN="35.187.121.12:26257,34.76.63.61:26257" \                                              
    docker stack deploy --with-registry-auth --prune -c ~/vcs/docker-stack.yml vcs                      
                                             
  # Remove a single service (e.g. to change its mode)                                                   
  sudo docker service rm vcs_db                                     
  # Scale a service manually                 
  sudo docker service update --replicas 2 vcs_iam-service                                               
                                             
  # Update image of a single service         
  sudo docker service update \               
    --image ghcr.io/ajinkyataranekar/distributed-vehicle-capacity-system/journey-service:latest \       
    --with-registry-auth \                   
    vcs_journey-service                                             
  ---                                     
  10. Quick Reference: VM Zones                                     
  ┌────────────┬────────────────┬────────────────┐                                                      
  │     VM     │      Zone      │       IP       │                                                      
  ├────────────┼────────────────┼────────────────┤                                                   
  │ vcs-vm-eu1 │ europe-west1-b │ 35.187.121.12  │
  ├────────────┼────────────────┼────────────────┤                                                      
  │ vcs-vm-eu2 │ europe-west1-b │ 34.76.63.61    │                                                      
  ├────────────┼────────────────┼────────────────┤                                                      
  │ vcs-vm-us1 │ us-east1-d     │ 34.138.242.217 │                                                      
  ├────────────┼────────────────┼────────────────┤                                                      
  │ vcs-vm-ap1 │ asia-east1-b   │ 34.80.180.64   │                                                      
  └────────────┴────────────────┴────────────────┘   