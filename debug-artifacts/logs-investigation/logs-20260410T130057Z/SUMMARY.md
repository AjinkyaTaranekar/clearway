# Regional Service Log Collection

- **Timestamp (UTC):** 20260410T130057Z
- **GCP project:** distributed-capacity-system
- **Tail lines per service:** 500
- **Per-command timeout:** 45s
- **Target services:** journey, iam, capacity, map, notification, nginx

| Region | VM | Service | Task error rows | Error log lines |
|---|---|---|---:|---:|
| eu | vcs-vm-eu1 | vcs_capacity-service | 2 | 1 |
| eu | vcs-vm-eu1 | vcs_iam-service | 2 | 1 |
| eu | vcs-vm-eu1 | vcs_journey-service | 0 | 46 |
| eu | vcs-vm-eu1 | vcs_map-service | 0 | 18 |
| eu | vcs-vm-eu1 | vcs_nginx | 0 | 1 |
| eu | vcs-vm-eu1 | vcs_notification-service | 0 | 1 |
| us | vcs-vm-us1 | vcs_capacity-service | 3 | 12 |
| us | vcs-vm-us1 | vcs_iam-service | 2 | 5 |
| us | vcs-vm-us1 | vcs_journey-service | 1 | 154 |
| us | vcs-vm-us1 | vcs_map-service | 1 | 127 |
| us | vcs-vm-us1 | vcs_nginx | 0 | 0 |
| us | vcs-vm-us1 | vcs_notification-service | 1 | 1 |
| apac | vcs-vm-ap1 | vcs_capacity-service | 3 | 17 |
| apac | vcs-vm-ap1 | vcs_iam-service | 1 | 365 |
| apac | vcs-vm-ap1 | vcs_journey-service | 0 | 144 |
| apac | vcs-vm-ap1 | vcs_map-service | 0 | 137 |
| apac | vcs-vm-ap1 | vcs_nginx | 0 | 0 |
| apac | vcs-vm-ap1 | vcs_notification-service | 0 | 4 |
