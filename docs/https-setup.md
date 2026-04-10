# HTTPS Setup — GCP Global Load Balancer with nip.io

## Why we did this

Firebase Cloud Messaging (FCM) web push notifications require the page to be
served over HTTPS. Browsers will not grant notification permission on plain HTTP
origins, and service workers (which FCM web push depends on) are restricted to
secure contexts. Without HTTPS the `Notification.requestPermission()` call on
the frontend silently fails and no device tokens are ever registered with the
notification service.

A real domain was not available at the time of setup, so we used
[nip.io](https://nip.io) — a wildcard DNS service that maps
`<IP>.nip.io` → `<IP>`. This lets GCP issue a managed TLS certificate for a
valid FQDN without buying a domain, which is sufficient for development and
staging use.

---

## What was built

The existing infrastructure used **regional target pools** (Layer 4 TCP). GCP
managed SSL certificates only work with **global HTTPS load balancers**, so a
new global LB stack was created alongside the existing one (the old regional LB
on port 80 was left untouched).

### Resources created per region

| Resource type | EU | US | APAC |
|---|---|---|---|
| Global static IP | `35.244.162.92` | `35.227.198.68` | `34.8.134.246` |
| Instance group | `vcs-ig-eu1`, `vcs-ig-eu2` | `vcs-ig-us1` | `vcs-ig-ap1` |
| Health check | `vcs-hc-global-eu` | `vcs-hc-global-us` | `vcs-hc-global-ap` |
| Backend service | `vcs-backend-eu` | `vcs-backend-us` | `vcs-backend-ap` |
| URL map (HTTPS) | `vcs-urlmap-eu` | `vcs-urlmap-us` | `vcs-urlmap-ap` |
| URL map (HTTP→HTTPS redirect) | `vcs-http-redirect-eu` | `vcs-http-redirect-us` | `vcs-http-redirect-ap` |
| Managed TLS cert | `vcs-nip-cert-eu` | `vcs-nip-cert-us` | `vcs-nip-cert-ap` |
| Target HTTPS proxy | `vcs-https-proxy-eu` | `vcs-https-proxy-us` | `vcs-https-proxy-ap` |
| Target HTTP proxy | `vcs-http-proxy-eu` | `vcs-http-proxy-us` | `vcs-http-proxy-ap` |
| Forwarding rule (443) | `vcs-fwd-https-eu` | `vcs-fwd-https-us` | `vcs-fwd-https-ap` |
| Forwarding rule (80) | `vcs-fwd-http-eu` | `vcs-fwd-http-us` | `vcs-fwd-http-ap` |

### Public endpoints

| Region | HTTPS base URL |
|---|---|
| EU | `https://35.244.162.92.nip.io` |
| US | `https://35.227.198.68.nip.io` |
| APAC | `https://34.8.134.246.nip.io` |

HTTP requests to port 80 on these IPs receive a `301 Moved Permanently`
redirect to HTTPS.

### Traffic path

```
Browser / App
    │  HTTPS :443
    ▼
GCP Global HTTPS LB  (TLS termination, sets X-Forwarded-Proto: https)
    │  HTTP :80  (internal, VPC)
    ▼
nginx (Docker Swarm)  (reads X-Forwarded-Proto, forwards $real_proto to backends)
    │
    ▼
Backend services (iam / journey / capacity / map / notification)
```

---

## Code changes made

### 1. `nginx/nginx.conf` — forward the real scheme

The GCP LB terminates TLS and forwards plain HTTP to nginx. Without a fix,
nginx would always set `X-Forwarded-Proto: http` on upstream requests, which
breaks any backend code that inspects the scheme (e.g. generating redirect
URLs, security checks, Swagger base URL inference).

Added a `map` block to preserve the original scheme:

```nginx
map $http_x_forwarded_proto $real_proto {
    default $scheme;
    https   https;
}
```

All `proxy_set_header X-Forwarded-Proto` directives now use `$real_proto`
instead of `$scheme`. Added `Strict-Transport-Security` header so browsers
remember to use HTTPS.

### 2. `.github/workflows/pipeline.yml` — inject `SWAGGER_PUBLIC_BASE_URL`

The `docker stack deploy` command in each region's deploy job now passes the
correct HTTPS base URL so Swagger UI generates requests against the right
origin:

```yaml
SWAGGER_PUBLIC_BASE_URL='https://35.244.162.92.nip.io' \   # EU
SWAGGER_PUBLIC_BASE_URL='https://35.227.198.68.nip.io' \   # US
SWAGGER_PUBLIC_BASE_URL='https://34.8.134.246.nip.io'  \   # APAC
```

---

## Certificate management

Certificates are GCP-managed and auto-renewed. To check status:

```bash
for cert in vcs-nip-cert-eu vcs-nip-cert-us vcs-nip-cert-ap; do
  echo -n "$cert: "
  gcloud compute ssl-certificates describe $cert \
    --global --project=distributed-capacity-system \
    --format="get(managed.status)"
done
```

---

## Upgrading to a real domain later

When a real domain is available:

1. Point the domain's DNS A record to the existing global static IPs.
2. Create new managed certs:
   ```bash
   gcloud compute ssl-certificates create vcs-cert-eu \
     --domains=api-eu.yourdomain.com --global \
     --project=distributed-capacity-system
   ```
3. Update the HTTPS proxies:
   ```bash
   gcloud compute target-https-proxies update vcs-https-proxy-eu \
     --ssl-certificates=vcs-cert-eu --global \
     --project=distributed-capacity-system
   ```
4. Update `SWAGGER_PUBLIC_BASE_URL` in `pipeline.yml` to the new domain.
5. Delete the nip.io certs once the new ones are ACTIVE.

The global static IPs, backend services, and instance groups do not need to
change.
