# K6 Load Test Analysis

- Generated UTC: `2026-04-10T12:02:29.275477+00:00`
- Artifact dir: `/home/ajinkyataranekar26/distributed-vehicle-capacity-system/debug-artifacts/load-tests/20260410T102719Z-fixed-full-suite`
- Completed runs: `9`
- Non-zero exits: `9`

> Response bodies are **not** recorded in current k6 JSON output; only metrics and request tags are available.

## EU / SMOKE

- Exit: `99` | Duration: `35s` | Requests: `32` | Error rate: `0.00%`
- Status distribution: `{'200': 23, '201': 9}`

**Threshold failures**
- `http_req_failed`: `rate<0.05`
- `capacity_check_duration`: `p(95)<800`
- `http_req_duration`: `p(99)<3000`
- `route_compute_duration`: `p(95)<2000`
- `auth_success`: `rate>0.95`
- `journey_create_success`: `rate>0.90`

**Common console issues**
- (1x) ✓ auth_success...................: 100.00% ✓ 5        ✗ 0
- (1x) checks.........................: 100.00% ✓ 44       ✗ 0
- (1x) ✗ http_req_duration..............: avg=343.59ms min=4.08ms   med=275.55ms max=1.39s    p(90)=827.65ms p(95)=1.1s
- (1x) ✓ http_req_failed................: 0.00%   ✓ 0        ✗ 32
- (1x) ✓ journey_create_success.........: 100.00% ✓ 4        ✗ 0
- (1x) time="2026-04-10T10:40:21Z" level=error msg="thresholds on metrics 'http_req_duration' have been crossed"

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 4 | 1121.554 | 1385.947 | 1394.639 | 0.00% | `{'201': 4}` |
| `GET /api/v1/capacity/segments/occupancy` | 4 | 275.559 | 754.007 | 820.452 | 0.00% | `{'200': 4}` |
| `POST /api/v1/auth/register` | 5 | 377.386 | 490.362 | 499.865 | 0.00% | `{'201': 5}` |
| `GET /api/v1/capacity/segments` | 4 | 451.648 | 462.189 | 462.265 | 0.00% | `{'200': 4}` |
| `POST /api/v1/routes/compute` | 4 | 292.552 | 396.368 | 405.051 | 0.00% | `{'200': 4}` |
| `GET /api/v1/map/search` | 4 | 23.362 | 43.570 | 44.832 | 0.00% | `{'200': 4}` |
| `GET /api/v1/auth/profile` | 5 | 14.091 | 17.120 | 17.590 | 0.00% | `{'200': 5}` |
| `GET /nginx-health` | 2 | 4.478 | 4.833 | 4.864 | 0.00% | `{'200': 2}` |

## EU / LOAD

- Exit: `99` | Duration: `663s` | Requests: `6988` | Error rate: `0.34%`
- Status distribution: `{'200': 5360, '201': 1604, '502': 24}`

**Threshold failures**
- `auth_success`: `rate>0.95`
- `http_req_failed`: `rate<0.05`

**Top failing checks**
- `journey create: status 201/200/409/422` fails=24 passes=425
- `journey create: not 5xx (except 503)` fails=24 passes=425

**Common console issues**
- (2x) ↳  94% — ✓ 425 / ✗ 24
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 1463      ✗ 0
- (1x) ✗ capacity_check_duration........: avg=1.19s    min=21ms     med=719ms    max=6.97s    p(90)=3.01s    p(95)=3.99s
- (1x) checks.........................: 99.50%  ✓ 9613      ✗ 48
- (1x) ✗ http_req_duration..............: avg=867.84ms min=2.29ms   med=400.79ms max=12.64s   p(90)=2.69s    p(95)=4.23s
- (1x) ✓ http_req_failed................: 0.34%   ✓ 24        ✗ 6964
- (1x) ✗ journey_create_success.........: 31.40%  ✓ 141       ✗ 308
- (1x) ✗ route_compute_duration.........: avg=1.57s    min=43ms     med=797ms    max=7.73s    p(90)=4.29s    p(95)=5.34s

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 449 | 1224.990 | 5832.006 | 10117.320 | 5.35% | `{'200': 284, '201': 141, '502': 24}` |
| `POST /api/v1/routes/compute` | 661 | 796.564 | 5347.489 | 6187.285 | 0.00% | `{'200': 661}` |
| `POST /api/v1/auth/register` | 1463 | 647.748 | 5254.576 | 7005.833 | 0.00% | `{'201': 1463}` |
| `GET /api/v1/capacity/segments` | 762 | 719.311 | 3998.196 | 5023.804 | 0.00% | `{'200': 762}` |
| `GET /api/v1/capacity/segments/occupancy` | 762 | 632.399 | 3890.159 | 5933.879 | 0.00% | `{'200': 762}` |
| `GET /api/v1/notifications` | 277 | 40.262 | 464.703 | 601.430 | 0.00% | `{'200': 277}` |
| `GET /api/v1/auth/profile` | 1463 | 23.950 | 452.186 | 707.811 | 0.00% | `{'200': 1463}` |
| `PUT /api/v1/notifications/read-all` | 277 | 17.846 | 206.057 | 246.113 | 0.00% | `{'200': 277}` |
| `GET /api/v1/map/search` | 661 | 10.742 | 137.353 | 196.105 | 0.00% | `{'200': 661}` |
| `GET /nginx-health` | 213 | 3.698 | 6.202 | 12.291 | 0.00% | `{'200': 213}` |

## EU / STRESS

- Exit: `99` | Duration: `906s` | Requests: `8675` | Error rate: `11.25%`
- Status distribution: `{'200': 5989, '201': 1710, '502': 976}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `register: status 201 or 409` fails=854 passes=1646
- `journey create: status 201/200/409/422` fails=122 passes=363
- `journey create: not 5xx (except 503)` fails=122 passes=363

**Common console issues**
- (2x) ↳  74% — ✓ 363 / ✗ 122
- (1x) ✗ register: status 201 or 409
- (1x) ↳  65% — ✓ 1646 / ✗ 854
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 1643     ✗ 0
- (1x) ✗ capacity_check_duration........: avg=2.02s    min=23ms     med=1.36s    max=8.28s    p(90)=4.72s    p(95)=5.23s
- (1x) checks.........................: 90.55%  ✓ 10531    ✗ 1098
- (1x) ✗ http_req_duration..............: avg=5.62s    min=2.54ms   med=1.09s    max=50.39s   p(90)=27.23s   p(95)=30s
- (1x) ✗ http_req_failed................: 11.25%  ✓ 976      ✗ 7699

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/auth/register` | 2500 | 13684.841 | 30006.202 | 30175.515 | 34.16% | `{'201': 1646, '502': 854}` |
| `GET /api/v1/auth/profile` | 1643 | 313.023 | 10716.131 | 27134.401 | 0.00% | `{'200': 1643}` |
| `POST /api/v1/routes/compute` | 729 | 2711.011 | 6516.745 | 7146.185 | 0.00% | `{'200': 729}` |
| `POST /api/v1/journeys` | 485 | 3106.496 | 6018.118 | 7576.394 | 25.15% | `{'200': 299, '201': 64, '502': 122}` |
| `GET /api/v1/capacity/segments` | 827 | 1367.544 | 5233.772 | 6042.434 | 0.00% | `{'200': 827}` |
| `GET /api/v1/capacity/segments/occupancy` | 827 | 1066.960 | 4937.455 | 6193.084 | 0.00% | `{'200': 827}` |
| `GET /api/v1/notifications` | 359 | 212.867 | 995.473 | 1459.820 | 0.00% | `{'200': 359}` |
| `PUT /api/v1/notifications/read-all` | 359 | 33.130 | 248.617 | 483.753 | 0.00% | `{'200': 359}` |
| `GET /api/v1/map/search` | 729 | 11.416 | 162.779 | 205.535 | 0.00% | `{'200': 729}` |
| `GET /nginx-health` | 217 | 4.045 | 77.725 | 172.787 | 0.00% | `{'200': 217}` |

## US / SMOKE

- Exit: `99` | Duration: `34s` | Requests: `18` | Error rate: `0.00%`
- Status distribution: `{'200': 15, '201': 3}`

**Threshold failures**
- `auth_success`: `rate>0.95`
- `http_req_failed`: `rate<0.05`
- `capacity_check_duration`: `p(95)<800`

**Common console issues**
- (1x) ✓ auth_success...................: 100.00% ✓ 3        ✗ 0
- (1x) checks.........................: 100.00% ✓ 24       ✗ 0
- (1x) ✗ http_req_duration..............: avg=1.39s    min=96.73ms  med=466.42ms max=7.48s    p(90)=4.06s    p(95)=6.41s
- (1x) ✓ http_req_failed................: 0.00%   ✓ 0        ✗ 18
- (1x) ✗ journey_create_success.........: 0.00%   ✓ 0        ✗ 2
- (1x) ✗ route_compute_duration.........: avg=2.54s    min=1.94s    med=2.54s    max=3.14s    p(90)=3.02s    p(95)=3.08s
- (1x) time="2026-04-10T11:07:04Z" level=error msg="thresholds on metrics 'http_req_duration, journey_create_success, route_compute_duration' have been crossed"

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 2 | 6855.621 | 7426.524 | 7477.271 | 0.00% | `{'200': 2}` |
| `POST /api/v1/routes/compute` | 2 | 2546.547 | 3084.295 | 3132.095 | 0.00% | `{'200': 2}` |
| `GET /api/v1/map/search` | 2 | 834.385 | 1324.440 | 1368.000 | 0.00% | `{'200': 2}` |
| `POST /api/v1/auth/register` | 3 | 776.617 | 868.846 | 877.045 | 0.00% | `{'201': 3}` |
| `GET /api/v1/capacity/segments/occupancy` | 2 | 466.421 | 497.022 | 499.742 | 0.00% | `{'200': 2}` |
| `GET /api/v1/capacity/segments` | 2 | 291.557 | 344.834 | 349.570 | 0.00% | `{'200': 2}` |
| `GET /api/v1/auth/profile` | 3 | 198.086 | 201.982 | 202.328 | 0.00% | `{'200': 3}` |
| `GET /nginx-health` | 2 | 98.362 | 99.831 | 99.961 | 0.00% | `{'200': 2}` |

## US / LOAD

- Exit: `99` | Duration: `674s` | Requests: `4993` | Error rate: `11.62%`
- Status distribution: `{'200': 3341, '201': 1072, '500': 324, '502': 256}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `search: status 200 or 429` fails=324 passes=182
- `journey create: status 201/200/409/422` fails=256 passes=53
- `journey create: not 5xx (except 503)` fails=256 passes=53

**Common console issues**
- (2x) ↳  17% — ✓ 53 / ✗ 256
- (1x) ✗ search: status 200 or 429
- (1x) ↳  35% — ✓ 182 / ✗ 324
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 1056     ✗ 0
- (1x) ✗ capacity_check_duration........: avg=1.72s    min=210ms    med=670ms    max=5.2s     p(90)=4.33s    p(95)=4.73s
- (1x) checks.........................: 87.81%  ✓ 6024     ✗ 836
- (1x) ✗ http_req_duration..............: avg=1.45s    min=91.74ms  med=475.93ms max=14.53s   p(90)=5.29s    p(95)=5.7s
- (1x) ✗ http_req_failed................: 11.61%  ✓ 580      ✗ 4413

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 309 | 5294.897 | 10903.818 | 11641.335 | 82.85% | `{'200': 37, '201': 16, '502': 256}` |
| `POST /api/v1/routes/compute` | 506 | 5690.519 | 5911.002 | 5938.818 | 0.00% | `{'200': 506}` |
| `GET /api/v1/capacity/segments` | 503 | 669.651 | 4732.563 | 5122.532 | 0.00% | `{'200': 503}` |
| `GET /api/v1/capacity/segments/occupancy` | 503 | 439.458 | 3731.800 | 5253.731 | 0.00% | `{'200': 503}` |
| `GET /api/v1/map/search` | 506 | 1316.227 | 2032.359 | 2108.218 | 64.03% | `{'200': 182, '500': 324}` |
| `POST /api/v1/auth/register` | 1056 | 867.757 | 1824.188 | 2847.612 | 0.00% | `{'201': 1056}` |
| `GET /api/v1/notifications` | 197 | 392.318 | 467.230 | 588.866 | 0.00% | `{'200': 197}` |
| `GET /api/v1/auth/profile` | 1056 | 198.050 | 295.624 | 421.896 | 0.00% | `{'200': 1056}` |
| `PUT /api/v1/notifications/read-all` | 197 | 197.988 | 216.492 | 265.544 | 0.00% | `{'200': 197}` |
| `GET /nginx-health` | 160 | 97.185 | 104.507 | 302.830 | 0.00% | `{'200': 160}` |

## US / STRESS

- Exit: `99` | Duration: `913s` | Requests: `7731` | Error rate: `36.58%`
- Status distribution: `{'200': 3667, '201': 1236, '500': 583, '502': 1839, '503': 259, '504': 147}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `register: status 201 or 409` fails=1826 passes=1231
- `search: status 200 or 429` fails=586 passes=0
- `journey create: status 201/200/409/422` fails=380 passes=6
- `journey create: not 5xx (except 503)` fails=380 passes=6
- `profile: status 200` fails=17 passes=1214
- `profile: has email` fails=17 passes=1214
- `route: status 200` fails=8 passes=578
- `notifications: status 200` fails=5 passes=228
- `mark-read: status 200 or 204` fails=4 passes=229
- `segments: status 200` fails=2 passes=617

**Common console issues**
- (2x) ↳  98% — ✓ 1214 / ✗ 17
- (2x) ↳  99% — ✓ 617 / ✗ 2
- (2x) ↳  1% — ✓ 6 / ✗ 380
- (1x) ✗ register: status 201 or 409
- (1x) ↳  40% — ✓ 1231 / ✗ 1826
- (1x) ✗ profile: status 200
- (1x) ✗ profile: has email
- (1x) ✗ search: status 200 or 429
- (1x) ↳  0% — ✓ 0 / ✗ 586
- (1x) ✗ route: status 200

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/auth/register` | 3057 | 7204.551 | 30149.823 | 30201.580 | 59.73% | `{'201': 1231, '502': 1459, '503': 220, '504': 147}` |
| `POST /api/v1/routes/compute` | 586 | 6002.311 | 6845.898 | 7220.817 | 1.37% | `{'200': 578, '503': 8}` |
| `POST /api/v1/journeys` | 386 | 615.625 | 5595.027 | 7078.344 | 98.45% | `{'200': 1, '201': 5, '502': 380}` |
| `GET /api/v1/capacity/segments` | 619 | 500.992 | 4641.473 | 5071.659 | 0.32% | `{'200': 617, '503': 2}` |
| `GET /api/v1/auth/profile` | 1231 | 709.895 | 3886.488 | 6478.350 | 1.38% | `{'200': 1214, '503': 17}` |
| `GET /api/v1/map/search` | 586 | 1720.255 | 2193.674 | 2225.790 | 100.00% | `{'500': 583, '503': 3}` |
| `GET /api/v1/capacity/segments/occupancy` | 619 | 830.284 | 1466.093 | 5398.621 | 0.00% | `{'200': 619}` |
| `GET /api/v1/notifications` | 233 | 568.852 | 1087.747 | 1397.124 | 2.15% | `{'200': 228, '503': 5}` |
| `PUT /api/v1/notifications/read-all` | 233 | 253.597 | 563.705 | 745.184 | 1.72% | `{'200': 229, '503': 4}` |
| `GET /nginx-health` | 181 | 105.090 | 217.392 | 244.735 | 0.00% | `{'200': 181}` |

## APAC / SMOKE

- Exit: `99` | Duration: `42s` | Requests: `16` | Error rate: `6.25%`
- Status distribution: `{'200': 12, '201': 3, '502': 1}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `journey create: status 201/200/409/422` fails=1 passes=0
- `journey create: not 5xx (except 503)` fails=1 passes=0

**Common console issues**
- (2x) ↳  0% — ✓ 0 / ✗ 1
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 3        ✗ 0
- (1x) ✗ capacity_check_duration........: avg=944ms    min=838ms    med=944ms    max=1.05s    p(90)=1.02s    p(95)=1.03s
- (1x) checks.........................: 90.47%  ✓ 19       ✗ 2
- (1x) ✗ http_req_duration..............: avg=2.01s    min=264.29ms med=1.12s    max=11.23s   p(90)=3.97s    p(95)=7.25s
- (1x) ✗ http_req_failed................: 6.25%   ✓ 1        ✗ 15
- (1x) ✗ journey_create_success.........: 0.00%   ✓ 0        ✗ 1
- (1x) ✗ route_compute_duration.........: avg=5.92s    min=5.92s    med=5.92s    max=5.92s    p(90)=5.92s    p(95)=5.92s

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 1 | 11234.808 | 11234.808 | 11234.808 | 100.00% | `{'502': 1}` |
| `POST /api/v1/routes/compute` | 1 | 5927.051 | 5927.051 | 5927.051 | 0.00% | `{'200': 1}` |
| `GET /api/v1/map/search` | 1 | 2024.337 | 2024.337 | 2024.337 | 0.00% | `{'200': 1}` |
| `POST /api/v1/auth/register` | 3 | 1659.380 | 1797.635 | 1809.925 | 0.00% | `{'201': 3}` |
| `GET /api/v1/capacity/segments/occupancy` | 2 | 1273.460 | 1339.296 | 1345.149 | 0.00% | `{'200': 2}` |
| `GET /api/v1/notifications` | 1 | 1049.551 | 1049.551 | 1049.551 | 0.00% | `{'200': 1}` |
| `GET /api/v1/capacity/segments` | 2 | 943.818 | 1039.698 | 1048.221 | 0.00% | `{'200': 2}` |
| `GET /api/v1/auth/profile` | 3 | 543.196 | 547.172 | 547.525 | 0.00% | `{'200': 3}` |
| `PUT /api/v1/notifications/read-all` | 1 | 544.399 | 544.399 | 544.399 | 0.00% | `{'200': 1}` |
| `GET /nginx-health` | 1 | 264.291 | 264.291 | 264.291 | 0.00% | `{'200': 1}` |

## APAC / LOAD

- Exit: `99` | Duration: `662s` | Requests: `4127` | Error rate: `10.56%`
- Status distribution: `{'200': 2820, '201': 871, '500': 175, '502': 261}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `journey create: status 201/200/409/422` fails=261 passes=0
- `journey create: not 5xx (except 503)` fails=261 passes=0
- `search: status 200 or 429` fails=175 passes=219

**Common console issues**
- (2x) ↳  0% — ✓ 0 / ✗ 261
- (1x) ✗ search: status 200 or 429
- (1x) ↳  55% — ✓ 219 / ✗ 175
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 871      ✗ 0
- (1x) ✗ capacity_check_duration........: avg=1.49s    min=610ms    med=750ms    max=5.57s    p(90)=3.95s   p(95)=4.72s
- (1x) checks.........................: 87.73%  ✓ 4985     ✗ 697
- (1x) ✗ http_req_duration..............: avg=1.86s    min=261.17ms med=1.12s    max=12.7s    p(90)=6.83s   p(95)=6.89s
- (1x) ✗ http_req_failed................: 10.56%  ✓ 436      ✗ 3691

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/journeys` | 261 | 811.313 | 12368.592 | 12429.364 | 100.00% | `{'502': 261}` |
| `POST /api/v1/routes/compute` | 394 | 6871.938 | 7053.873 | 7187.595 | 0.00% | `{'200': 394}` |
| `GET /api/v1/capacity/segments/occupancy` | 424 | 1152.847 | 5212.486 | 5898.363 | 0.00% | `{'200': 424}` |
| `GET /api/v1/capacity/segments` | 424 | 749.833 | 4728.597 | 5188.003 | 0.00% | `{'200': 424}` |
| `POST /api/v1/auth/register` | 871 | 1749.375 | 2640.059 | 3252.032 | 0.00% | `{'201': 871}` |
| `GET /api/v1/map/search` | 394 | 302.397 | 2176.874 | 2235.079 | 44.42% | `{'200': 219, '500': 175}` |
| `GET /api/v1/notifications` | 173 | 1063.887 | 1129.559 | 1281.011 | 0.00% | `{'200': 173}` |
| `GET /api/v1/auth/profile` | 871 | 541.657 | 595.163 | 675.539 | 0.00% | `{'200': 871}` |
| `PUT /api/v1/notifications/read-all` | 173 | 540.495 | 551.417 | 571.455 | 0.00% | `{'200': 173}` |
| `GET /nginx-health` | 142 | 267.765 | 283.771 | 284.532 | 0.00% | `{'200': 142}` |

## APAC / STRESS

- Exit: `99` | Duration: `932s` | Requests: `5217` | Error rate: `50.74%`
- Status distribution: `{'200': 1919, '201': 651, '500': 280, '502': 1824, '503': 363, '504': 180}`

**Threshold failures**
- `auth_success`: `rate>0.95`

**Top failing checks**
- `register: status 201 or 409` fails=2188 passes=651
- `search: status 200 or 429` fails=280 passes=0
- `journey create: status 201/200/409/422` fails=179 passes=0
- `journey create: not 5xx (except 503)` fails=179 passes=0

**Common console issues**
- (2x) ↳  0% — ✓ 0 / ✗ 179
- (1x) ✗ register: status 201 or 409
- (1x) ↳  22% — ✓ 651 / ✗ 2188
- (1x) ✗ search: status 200 or 429
- (1x) ↳  0% — ✓ 0 / ✗ 280
- (1x) ✗ journey create: status 201/200/409/422
- (1x) ✗ journey create: not 5xx (except 503)
- (1x) ✓ auth_success...................: 100.00% ✓ 651      ✗ 0
- (1x) ✗ capacity_check_duration........: avg=1.37s    min=634ms    med=808ms    max=5.54s    p(90)=3.61s    p(95)=4.78s
- (1x) checks.........................: 55.52%  ✓ 3528     ✗ 2826

**Per-API latency and error**

| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |
|---|---:|---:|---:|---:|---:|---|
| `POST /api/v1/auth/register` | 2839 | 16235.838 | 30352.668 | 30400.107 | 77.07% | `{'201': 651, '502': 1645, '503': 363, '504': 180}` |
| `POST /api/v1/routes/compute` | 279 | 6912.251 | 7448.604 | 7794.656 | 0.00% | `{'200': 279}` |
| `POST /api/v1/journeys` | 179 | 857.353 | 5945.375 | 7377.675 | 100.00% | `{'502': 179}` |
| `GET /api/v1/capacity/segments` | 308 | 808.584 | 4784.979 | 5228.011 | 0.00% | `{'200': 308}` |
| `GET /api/v1/auth/profile` | 651 | 598.523 | 3016.026 | 4197.932 | 0.00% | `{'200': 651}` |
| `GET /api/v1/map/search` | 280 | 1847.947 | 2259.025 | 2286.424 | 100.00% | `{'500': 280}` |
| `GET /api/v1/capacity/segments/occupancy` | 308 | 1211.703 | 2119.632 | 5590.488 | 0.00% | `{'200': 308}` |
| `GET /api/v1/notifications` | 130 | 1071.353 | 1450.093 | 1690.481 | 0.00% | `{'200': 130}` |
| `PUT /api/v1/notifications/read-all` | 130 | 544.000 | 708.028 | 936.581 | 0.00% | `{'200': 130}` |
| `GET /nginx-health` | 113 | 280.902 | 396.932 | 407.913 | 0.00% | `{'200': 113}` |
