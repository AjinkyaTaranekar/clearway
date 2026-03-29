

**CS7NS6**   
**Global Distributed Traffic Service**   
Interim Architecture Report \- March 2026  
**Ajinkya Taranekar  |  Deepika Nag  |  Jai Nagle  |  Xiaoxuan Duan  | Ziwei Zhao**

# 1\. Introduction

**Problem Statement.** The system enables road-vehicle drivers to prebook every journey before travel. Road capacity is treated as a finite, reservable resource: each road segment has a maximum number of vehicles allowed per time window. No driver may start a journey without prior system approval confirming sufficient capacity on all segments along the route.  
**Functional Scope.** Drivers register, authenticate, and submit journey requests specifying origin, destination, departure time, and vehicle type. The system decomposes the route into road segments, checks capacity on each segment for the relevant time windows, and returns an approval or rejection. Drivers can cancel approved journeys, releasing capacity. Administrators can view all bookings and override capacity or journey status via a dashboard. All booking outcomes are delivered as push notifications via Firebase Cloud Messaging.  
**Assumptions.** Journeys must be booked at least 1 hour before departure. Cross-region journeys are out of scope for the prototype (each cell handles regional journeys only). The road network is modeled as a simplified graph of approximately 20 to 30 major road segments. The system assumes a crash-recovery failure model with no Byzantine faults. Read-to-write ratio is estimated at 10:1, with peak booking activity during predictable evening and weekend planning windows.

# 2\. Technical Architecture

![][image1]The system follows a cell-based architecture. Each geographic region operates a fully self-contained deployment of all services, the data layer, the event bus, and the API gateway. Cells are independent with no cross-cell communication. For the prototype, a single cell is deployed across a three-node Docker Swarm cluster on Azure. 

# 3\. Services

The system consists of five application-level services. Each service is owned by one team member and communicates via REST/HTTP (synchronous) and Redis Streams (asynchronous events).

| \# | Service | Responsibility | Owner |
| :---- | :---- | :---- | :---- |
| S1 | IAM / Auth | User registration, JWT-based authentication, driver profiles, role-based access control, token management. | Deepika Nag |
| S2 | Journey | Booking lifecycle orchestrator: create, approve, reject, cancel journeys. Coordinates with Capacity and Map services. Implements saga pattern (compensating transactions) for multi-segment reservations. Publishes events to Redis Streams. | Ajinkya Taranekar |
| S3 | Capacity | Road segment slot management with 15-minute time windows. Vehicle-type weighted allocation (car=1, truck=3, motorcycle=0.5). Optimistic concurrency control for conflict detection. Availability queries with Redis caching (30s TTL). | Jai Nagle |
| S4 | Map / Route | Pre-defined road segment graph. Shortest-path computation (Dijkstra). Route decomposition: translates origin/destination into ordered segment list with traversal time estimates. TomTom API integration for client-side visualization. | Xiaoxuan Duan |
| S5 | Notification | Consumes booking events from Redis Streams via consumer groups. Delivers push notifications through Firebase Cloud Messaging. Retry with exponential backoff for failed deliveries. | Ziwei Zhao |

# 4\. Tech Stack and Third-Party Technologies

| Category | Technology | Purpose |
| :---- | :---- | :---- |
| Backend Language | Go 1.25 | All five microservices. Low memory footprint ideal for constrained VMs. |
| Frontend | React (Vite) as PWA | Mobile-responsive driver-facing web application. |
| Database | PostgreSQL 18 | Primary data store. Primary/replica with streaming replication for failover. |
| Cache \+ Event Bus | Redis 8 (Streams) | Caching, session storage, and persistent event bus (Redis Streams with consumer groups). Redis Sentinel for HA. |
| API Gateway | Nginx | TLS termination, request routing, rate limiting, static asset serving. |
| Container Orchestration | Docker Swarm | Multi-node container orchestration across 3 Azure B1s VMs (Ireland, Belgium, London). |
| Map Visualization | TomTom Maps API | Client-side route visualization for drivers. |
| Push Notifications | Firebase Cloud Messaging | Push notifications for booking confirmations, rejections, and cancellations. |
| TLS Certificates | Let's Encrypt | Automated TLS certificate provisioning. |
| Infrastructure | Azure B1s VMs (3x) | 1 vCPU, 1GB RAM each. Docker Swarm cluster: 1 manager \+ 2 workers. |

The proposed architecture and technology selections are intended to balance sound distributed systems design with the practical constraints of this project, including a limited budget (€80 in Azure credits), a five-person development team, and a single-semester timeframe. Within these boundaries, the focus is on demonstrating robust architectural principles while using cost-effective and manageable technologies.

In a production environment supporting millions of users, several components would typically be replaced with enterprise-grade solutions. For instance, Kubernetes would likely be used for container orchestration instead of Docker Swarm, while Apache Kafka or Redpanda would serve as the event streaming platform in place of Redis Streams. Internal Microservice communication could be done with gRPC instead of REST to reduce tail latencies. Identity and access management would be handled by established solutions such as Keycloak or Auth0 rather than a custom JWT service, and managed database services like Azure Database for PostgreSQL with built-in high availability would replace self-managed replication.

Despite these potential infrastructure upgrades, the fundamental architectural approach would remain the same. Key design principles including the cell-based deployment model, optimistic concurrency for managing capacity, the saga pattern for coordinating multi-segment bookings, and the separation of synchronous request handling from asynchronous event processing are designed to scale effectively to production environments. As a result, the system is structured so that these infrastructure improvements can be introduced as largely drop-in replacements rather than requiring substantial architectural changes.

