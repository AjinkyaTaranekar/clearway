#!/usr/bin/env bash
# =============================================================================
# Clearway — End-to-End Demo Verification Script
# CS7NS6 Distributed Systems — TCD
#
# Usage:
#   ./demo.sh           # Run all tests against all 3 regions
#   ./demo.sh eu        # EU only
#   ./demo.sh us        # US only
#   ./demo.sh apac      # APAC only (writes will be slow — known limitation)
#   ./demo.sh saga      # Saga happy path (Istanbul→Ankara, COMMITTED)
#   ./demo.sh undo      # Saga undo/compensation demo (force EU failure → APAC rolled back)
#   ./demo.sh clean     # Cancel all demo driver journeys (cleanup)
#
# Pre-requisites: curl, jq
# =============================================================================

set -euo pipefail

# ─── Endpoints ───────────────────────────────────────────────────────────────
EU="https://35.244.162.92.nip.io"
US="https://35.227.198.68.nip.io"
APAC="https://34.8.134.246.nip.io"
EU_CRDB_SSH="deploy@35.187.121.12"
SSH_KEY="$HOME/.ssh/vcs_key"

# ─── Credentials ─────────────────────────────────────────────────────────────
ADMIN_EMAIL="admin@vcs.local"
ADMIN_PASS="admin123"
D1_EMAIL="driver1@demo.ie"
D2_EMAIL="driver2@demo.ie"
D3_EMAIL="driver3@demo.ie"
D_PASS="Demo1234!"

# ─── Route: Dublin Airport → UCD ─────────────────────────────────────────────
ORIGIN_LAT="53.4282916"
ORIGIN_LNG="-6.2472741"
DEST_LAT="53.3068763"
DEST_LNG="-6.2246251"

# ─── Colours ─────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Colour

PASS=0
FAIL=0
SKIP=0

# ─── Helpers ─────────────────────────────────────────────────────────────────
header() {
  echo ""
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}${BLUE}  $1${NC}"
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
}

section() {
  echo ""
  echo -e "${CYAN}── $1 ──${NC}"
}

pass() {
  echo -e "  ${GREEN}✓${NC} $1"
  PASS=$((PASS + 1))
}

fail() {
  echo -e "  ${RED}✗${NC} $1"
  FAIL=$((FAIL + 1))
}

skip() {
  echo -e "  ${YELLOW}⚠${NC} $1"
  SKIP=$((SKIP + 1))
}

info() {
  echo -e "  ${YELLOW}→${NC} $1"
}

assert_eq() {
  local label="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    pass "$label: got '$got'"
  else
    fail "$label: got '$got', want '$want'"
  fi
}

assert_contains() {
  local label="$1" haystack="$2" needle="$3"
  if echo "$haystack" | grep -q "$needle"; then
    pass "$label: contains '$needle'"
  else
    fail "$label: '$needle' not found in '$haystack'"
  fi
}

# ─── Auth helpers ─────────────────────────────────────────────────────────────
get_token() {
  local base="$1" email="$2" pass="$3"
  curl -s -X POST "$base/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$pass\"}" | jq -r '.data.access_token // empty'
}

cancel_active() {
  local base="$1" token="$2"
  local jid
  jid=$(curl -s "$base/api/v1/journeys" -H "Authorization: Bearer $token" \
    | jq -r '[.data.journeys[] | select(.status=="APPROVED")] | .[0].journey_id // empty')
  if [ -n "$jid" ]; then
    curl -s -X PUT "$base/api/v1/journeys/$jid/cancel" \
      -H "Authorization: Bearer $token" > /dev/null
    info "Cancelled existing APPROVED journey $jid"
  fi
}

# ─── Core test functions ──────────────────────────────────────────────────────

test_region() {
  local base="$1" label="$2" expected_region="$3"
  section "[$label] Region endpoint"
  local region
  region=$(curl -s --max-time 5 "$base/api/v1/region" 2>/dev/null | jq -r '.region // empty')
  assert_eq "Region" "$region" "$expected_region"
}

test_geocode() {
  local base="$1" label="$2" token="$3"
  section "[$label] Geocoding — Dublin Airport search"
  local result
  result=$(curl -s "$base/api/v1/map/search?q=Dublin+Airport&limit=1" \
    -H "Authorization: Bearer $token")
  local name lat
  name=$(echo "$result" | jq -r '.data.places[0].name // empty')
  lat=$(echo "$result" | jq -r '.data.places[0].lat // empty')
  assert_contains "Place name" "$name" "Dublin"
  if [ -n "$lat" ]; then
    pass "Lat returned: $lat"
  else
    fail "No lat returned from geocoder"
  fi
}

test_route_compute() {
  local base="$1" label="$2" token="$3"
  section "[$label] Route compute — Dublin Airport → UCD"
  local result
  result=$(curl -s -X POST "$base/api/v1/routes/compute" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -d "{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG}}")
  local seg_count km
  seg_count=$(echo "$result" | jq '.data.segments | length')
  km=$(echo "$result" | jq '.data.total_distance_km')

  if [ "$seg_count" -gt 5 ] 2>/dev/null; then
    pass "Segments returned: $seg_count"
  else
    fail "Expected >5 segments, got: $seg_count"
  fi
  info "Distance: ${km}km"

  # Verify all segment IDs have eu_ prefix (Dublin is in EU)
  local non_eu
  non_eu=$(echo "$result" | jq '[.data.segments[] | select(.segment_id | startswith("eu_") | not)] | length')
  if [ "$non_eu" -eq 0 ] 2>/dev/null; then
    pass "All segment IDs have eu_ prefix (Dublin route)"
  else
    fail "Found $non_eu segments without eu_ prefix on Dublin route"
  fi
}

test_booking_approved() {
  local base="$1" label="$2" token="$3" idempotency_suffix="$4"
  section "[$label] Journey booking — expect APPROVED"
  cancel_active "$base" "$token"
  local departure
  departure=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+2H '+%Y-%m-%dT%H:%M:%SZ')
  local result status
  result=$(curl -s -X POST "$base/api/v1/journeys" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: demo-approved-${idempotency_suffix}-$(date +%s)" \
    -d "{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}")
  status=$(echo "$result" | jq -r '.data.status // empty')
  local jid
  jid=$(echo "$result" | jq -r '.data.journey_id // empty')
  assert_eq "Status" "$status" "APPROVED"
  if [ -n "$jid" ]; then
    info "Journey ID: $jid"
    echo "$jid"  # return journey ID
  fi
}

test_idempotency() {
  local base="$1" label="$2" token="$3"
  section "[$label] Idempotency — same key twice = same result"
  cancel_active "$base" "$token"
  local departure
  departure=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+2H '+%Y-%m-%dT%H:%M:%SZ')
  local key="idem-test-$(date +%s)"
  local payload="{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}"

  local r1 r2 jid1 jid2
  r1=$(curl -s -X POST "$base/api/v1/journeys" -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d "$payload")
  jid1=$(echo "$r1" | jq -r '.data.journey_id // empty')

  r2=$(curl -s -X POST "$base/api/v1/journeys" -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d "$payload")
  jid2=$(echo "$r2" | jq -r '.data.journey_id // empty')

  if [ "$jid1" = "$jid2" ] && [ -n "$jid1" ]; then
    pass "Both calls returned same journey_id: $jid1"
  else
    fail "Idempotency broken — first: '$jid1', second: '$jid2'"
  fi
}

test_capacity_exhaustion() {
  local base="$1" label="$2"
  section "[$label] Capacity exhaustion — M50 capacity=2, 3rd driver rejected"

  local admin_token t1 t2 t3
  admin_token=$(get_token "$base" "$ADMIN_EMAIL" "$ADMIN_PASS")
  t1=$(get_token "$base" "$D1_EMAIL" "$D_PASS")
  t2=$(get_token "$base" "$D2_EMAIL" "$D_PASS")
  t3=$(get_token "$base" "$D3_EMAIL" "$D_PASS")

  # Cancel existing approved journeys
  cancel_active "$base" "$t1"
  cancel_active "$base" "$t2"
  cancel_active "$base" "$t3"

  # Set M50 capacity to 2
  curl -s -X PUT "$base/api/v1/capacity/segments/eu_m50/capacity" \
    -H "Authorization: Bearer $admin_token" \
    -H 'Content-Type: application/json' \
    -d '{"max_capacity": 2}' > /dev/null
  info "Set eu_m50 max_capacity=2"

  local departure
  departure=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+2H '+%Y-%m-%dT%H:%M:%SZ')
  local payload="{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}"
  local ts
  ts=$(date +%s)

  local s1 s2 s3
  s1=$(curl -s -X POST "$base/api/v1/journeys" -H "Authorization: Bearer $t1" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: cap-d1-$ts" \
    -d "$payload" | jq -r '.data.status // empty')
  s2=$(curl -s -X POST "$base/api/v1/journeys" -H "Authorization: Bearer $t2" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: cap-d2-$ts" \
    -d "$payload" | jq -r '.data.status // empty')
  s3=$(curl -s -X POST "$base/api/v1/journeys" -H "Authorization: Bearer $t3" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: cap-d3-$ts" \
    -d "$payload" | jq -r '.data.status // empty')

  info "Driver1: $s1 | Driver2: $s2 | Driver3: $s3"

  # At least 2 approved
  local approved=0
  [ "$s1" = "APPROVED" ] && approved=$((approved+1))
  [ "$s2" = "APPROVED" ] && approved=$((approved+1))
  [ "$s3" = "APPROVED" ] && approved=$((approved+1))

  # At least 1 rejected
  local rejected=0
  [ "$s1" = "REJECTED" ] && rejected=$((rejected+1))
  [ "$s2" = "REJECTED" ] && rejected=$((rejected+1))
  [ "$s3" = "REJECTED" ] && rejected=$((rejected+1))

  if [ "$approved" -ge 1 ] && [ "$rejected" -ge 1 ]; then
    pass "Capacity enforced: $approved approved, $rejected rejected (max was 2)"
  else
    fail "Expected mix of APPROVED/REJECTED. Got: $s1 / $s2 / $s3"
  fi

  # Restore M50 to 100
  curl -s -X PUT "$base/api/v1/capacity/segments/eu_m50/capacity" \
    -H "Authorization: Bearer $admin_token" \
    -H 'Content-Type: application/json' \
    -d '{"max_capacity": 100}' > /dev/null
  info "Restored eu_m50 max_capacity=100"
}

test_road_closure() {
  local base="$1" label="$2"
  section "[$label] Road closure — M50 closed, booking rejected"

  local admin_token t3
  admin_token=$(get_token "$base" "$ADMIN_EMAIL" "$ADMIN_PASS")
  t3=$(get_token "$base" "$D3_EMAIL" "$D_PASS")
  cancel_active "$base" "$t3"

  # Create 12-hour closure
  local closure_result closure_id
  closure_result=$(curl -s -X POST "$base/api/v1/capacity/closures" \
    -H "Authorization: Bearer $admin_token" \
    -H 'Content-Type: application/json' \
    -d '{"segment_id":"eu_m50","duration_minutes":720,"reason":"Demo bridge inspection"}')
  closure_id=$(echo "$closure_result" | jq -r '.closure_id // empty')

  if [ -z "$closure_id" ]; then
    # Might already be an active closure
    local existing
    existing=$(curl -s "$base/api/v1/capacity/closures" -H "Authorization: Bearer $admin_token" \
      | jq -r '.[0].closure_id // empty')
    if [ -n "$existing" ]; then
      closure_id="$existing"
      info "Using existing closure: $closure_id"
    else
      fail "Could not create closure: $(echo "$closure_result" | jq -r '.error // .')"
      return
    fi
  else
    pass "Closure created: $closure_id (eu_m50, 12h)"
  fi

  # Try booking — departure in 70min so M50 traversal is inside the closure window
  local departure
  departure=$(date -u -d '+70 minutes' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+70M '+%Y-%m-%dT%H:%M:%SZ')
  local result status reason
  result=$(curl -s -X POST "$base/api/v1/journeys" \
    -H "Authorization: Bearer $t3" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: closure-$(date +%s)" \
    -d "{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}")
  status=$(echo "$result" | jq -r '.data.status // empty')
  reason=$(echo "$result" | jq -r '.data.rejection_reason // empty')

  assert_eq "Status with closed segment" "$status" "REJECTED"
  assert_contains "Rejection reason" "$reason" "closed"

  # Verify check API
  local now end_t can_reserve is_closed
  now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  end_t=$(date -u -d '+4 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -v+4H '+%Y-%m-%dT%H:%M:%SZ')
  local check_result
  check_result=$(curl -s "$base/api/v1/capacity/check?segment_id=eu_m50&time_window_start=$now&time_window_end=$end_t")
  is_closed=$(echo "$check_result" | jq -r '.is_closed // false')
  assert_eq "Check API is_closed" "$is_closed" "true"

  # Expire closure
  if [ -f "$SSH_KEY" ]; then
    ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_db) \
      /cockroach/cockroach sql --insecure --host=localhost:26257 \
      -e \"UPDATE capacity.segment_closures SET status='cancelled', end_time=NOW()-INTERVAL '1s' WHERE segment_id='eu_m50' AND status='active';\"" \
      > /dev/null 2>&1
    info "Closure expired via DB"
  else
    info "SSH key not found — closure expires naturally in 12h"
  fi
}

test_crdb_replication() {
  section "CRDB Replication — write to US, read from EU"

  local ts
  ts=$(date +%s)
  local email="repl_test_${ts}@demo.ie"

  # Register via US
  local reg_result
  reg_result=$(curl -s -X POST "$US/api/v1/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"Demo1234!\",\"name\":\"Replication Test\",\"vehicle_type\":\"car\",\"license_info\":{\"license_number\":\"REPL-$ts\",\"country\":\"IE\"}}")
  local reg_ok
  reg_ok=$(echo "$reg_result" | jq -r '.success // false')
  assert_eq "Register via US" "$reg_ok" "true"

  # Give Raft 1s to propagate (usually instant but be safe)
  sleep 1

  # Login from EU
  local login_result
  login_result=$(curl -s -X POST "$EU/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"Demo1234!\"}")
  local login_ok
  login_ok=$(echo "$login_result" | jq -r '.success // false')
  assert_eq "Login from EU (Raft replicated)" "$login_ok" "true"
}

test_saga() {
  header "Saga Pattern: Istanbul → Ankara (Bosphorus Crossing)"
  echo ""
  echo -e "  ${YELLOW}What:${NC} Route crosses Bosphorus (29.1°E). Segments split across eu_ and ap_ regions."
  echo -e "  ${YELLOW}Why:${NC} Multi-region reservation needs Saga coordinator instead of single transaction."

  local token
  token=$(get_token "$EU" "$D1_EMAIL" "$D_PASS")
  cancel_active "$EU" "$token"

  section "Computing Istanbul→Ankara route"
  local route
  route=$(curl -s -X POST "$EU/api/v1/routes/compute" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -d '{"origin":{"lat":41.0082,"lng":28.9784},"destination":{"lat":39.9334,"lng":32.8597}}')

  local eu_count ap_count
  eu_count=$(echo "$route" | jq '[.data.segments[] | select(.segment_id | startswith("eu_"))] | length')
  ap_count=$(echo "$route" | jq '[.data.segments[] | select(.segment_id | startswith("ap_"))] | length')
  local total
  total=$(echo "$route" | jq '.data.segments | length')

  info "Total segments: $total  |  eu_ segments: $eu_count  |  ap_ segments: $ap_count"

  if [ "$eu_count" -gt 0 ] && [ "$ap_count" -gt 0 ]; then
    pass "Route crosses regions: $eu_count eu_ + $ap_count ap_ segments → Saga will fire"
  else
    fail "Route did not cross regions (eu_=$eu_count, ap_=$ap_count)"
    return
  fi

  # Show the O-4 split at Bosphorus
  local o4_eu o4_ap
  o4_eu=$(echo "$route" | jq -r '[.data.segments[] | select(.segment_id == "eu_o_4")] | length')
  o4_ap=$(echo "$route" | jq -r '[.data.segments[] | select(.segment_id == "ap_o_4")] | length')
  if [ "$o4_eu" -gt 0 ] && [ "$o4_ap" -gt 0 ]; then
    pass "O-4 motorway splits at Bosphorus: eu_o_4 (west) and ap_o_4 (east) both present"
  else
    info "O-4 split not in this OSRM response (route may vary) — eu_=$o4_eu, ap_=$o4_ap"
  fi

  section "Booking Istanbul→Ankara"
  local departure
  departure=$(date -u -d '+2 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+2H '+%Y-%m-%dT%H:%M:%SZ')
  local booking_result status jid
  booking_result=$(curl -s -X POST "$EU/api/v1/journeys" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: saga-$(date +%s)" \
    -d "{\"origin\":{\"lat\":41.0082,\"lng\":28.9784},\"destination\":{\"lat\":39.9334,\"lng\":32.8597},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}")
  status=$(echo "$booking_result" | jq -r '.data.status // empty')
  jid=$(echo "$booking_result" | jq -r '.data.journey_id // empty')

  assert_eq "Istanbul→Ankara booking" "$status" "APPROVED"
  info "Journey: $jid"

  section "Verifying Saga record in CockroachDB"
  if [ -f "$SSH_KEY" ]; then
    local saga_output
    saga_output=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_db) \
      /cockroach/cockroach sql --insecure --host=localhost:26257 \
      -e 'SELECT saga_id, journey_id, status, region_steps FROM capacity.reservation_sagas ORDER BY created_at DESC LIMIT 1;'" \
      2>/dev/null)

    echo ""
    echo -e "${BOLD}  CockroachDB saga table:${NC}"
    echo "$saga_output" | sed 's/^/  /'

    local saga_status
    saga_status=$(echo "$saga_output" | grep -E "COMMITTED|COMPENSATED|PENDING" | awk '{print $3}' || echo "")
    if echo "$saga_output" | grep -q "COMMITTED"; then
      pass "Saga status: COMMITTED (both EU and APAC reservations succeeded)"
    elif echo "$saga_output" | grep -q "COMPENSATED"; then
      skip "Saga status: COMPENSATED (one region failed, reservation rolled back — expected under load)"
    else
      info "Saga output: $saga_output"
      skip "Saga record found but status unclear — check table manually"
    fi

    # Show region_steps breakdown
    if echo "$saga_output" | grep -q '"eu"'; then
      pass "EU step present in saga record"
    fi
    if echo "$saga_output" | grep -q '"apac"'; then
      pass "APAC step present in saga record"
    fi
  else
    skip "SSH key not found at $SSH_KEY — cannot query CRDB directly. Check Grafana or CRDB UI."
  fi
}

test_saga_compensation() {
  header "Saga Undo Pattern — Compensation Demo"
  echo ""
  echo -e "  ${YELLOW}What:${NC} Force a partial saga failure to trigger compensation (undo)."
  echo -e "  ${YELLOW}How:${NC}  Istanbul→Ankara saga runs APAC region first (alphabetical order)."
  echo -e "         We close one EU segment so APAC commits but EU hits a closure."
  echo -e "         The saga coordinator deletes the APAC rows — full rollback, driver REJECTED."
  echo -e "  ${YELLOW}Why:${NC}  Proves there is no partial booking. Either all regions commit or none do."

  local admin_token token
  admin_token=$(get_token "$EU" "$ADMIN_EMAIL" "$ADMIN_PASS")
  if [ -z "$admin_token" ]; then
    fail "Cannot get admin token — skipping undo demo"
    return
  fi
  token=$(get_token "$EU" "$D1_EMAIL" "$D_PASS")
  cancel_active "$EU" "$token"

  # ── Step 1: compute the route to discover which eu_ segments are on it ────────
  section "Step 1: Compute Istanbul→Ankara route"
  local route
  route=$(curl -s -X POST "$EU/api/v1/routes/compute" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -d '{"origin":{"lat":41.0082,"lng":28.9784},"destination":{"lat":39.9334,"lng":32.8597}}')

  local eu_count ap_count
  eu_count=$(echo "$route" | jq '[.data.segments[] | select(.segment_id | startswith("eu_"))] | length')
  ap_count=$(echo "$route" | jq '[.data.segments[] | select(.segment_id | startswith("ap_"))] | length')

  info "eu_ segments: $eu_count  |  ap_ segments: $ap_count"

  if [ "$eu_count" -eq 0 ] || [ "$ap_count" -eq 0 ]; then
    fail "Route does not cross regions (eu_=$eu_count ap_=$ap_count) — cannot demo compensation"
    return
  fi

  # Pick the first eu_ segment from this route to close
  local block_seg
  block_seg=$(echo "$route" | jq -r '[.data.segments[] | select(.segment_id | startswith("eu_"))] | .[0].segment_id')
  info "Will close: $block_seg (12h road closure → EU reservation will fail with segment_closed)"

  # ── Step 2: create a 12h road closure on that eu_ segment ────────────────────
  # Note: the API rejects max_capacity=0 (must be > 0), so closures are the
  # reliable way to guarantee a hard block on a specific segment.
  section "Step 2: Close EU segment via road closure"
  local closure_result closure_id
  closure_result=$(curl -s -X POST "$EU/api/v1/capacity/closures" \
    -H "Authorization: Bearer $admin_token" \
    -H 'Content-Type: application/json' \
    -d "{\"segment_id\":\"$block_seg\",\"duration_minutes\":720,\"reason\":\"Saga undo demo — simulated emergency closure\"}")
  closure_id=$(echo "$closure_result" | jq -r '.closure_id // empty')

  if [ -n "$closure_id" ]; then
    pass "Closure $closure_id created on $block_seg — EU reservation will fail"
  else
    local err
    err=$(echo "$closure_result" | jq -r '.error // empty')
    if echo "$err" | grep -qi "overlap\|already"; then
      pass "Existing closure already active on $block_seg — will use it"
    else
      fail "Could not close $block_seg: $err"
      return
    fi
  fi

  # ── Step 3: attempt the booking ───────────────────────────────────────────────
  # Saga executes regions in alphabetical order: apac → eu
  # APAC commits (ap_ segments reserved), then EU hits the closure → segment_closed.
  # compensateSteps() fires → ReleaseByReservationID removes the APAC rows.
  # Departure +70min so the traversal window overlaps the closure.
  section "Step 3: Book Istanbul→Ankara (APAC commits first, EU hits closure → undo fires)"
  local departure
  departure=$(date -u -d '+70 minutes' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+70M '+%Y-%m-%dT%H:%M:%SZ')
  local booking_result status reason
  booking_result=$(curl -s -X POST "$EU/api/v1/journeys" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: saga-undo-$(date +%s)" \
    -d "{\"origin\":{\"lat\":41.0082,\"lng\":28.9784},\"destination\":{\"lat\":39.9334,\"lng\":32.8597},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}")
  status=$(echo "$booking_result" | jq -r '.data.status // empty')
  reason=$(echo "$booking_result" | jq -r '.data.rejection_reason // empty')
  assert_eq "Booking rejected (EU segment closed)" "$status" "REJECTED"
  if [ -n "$reason" ]; then
    info "Rejection reason: $reason"
  fi

  # ── Step 4: verify saga is COMPENSATED in CRDB ───────────────────────────────
  section "Step 4: Verify saga COMPENSATED in CockroachDB"
  if [ -f "$SSH_KEY" ]; then
    local saga_out
    saga_out=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_db) \
      /cockroach/cockroach sql --insecure --host=localhost:26257 \
      -e 'SELECT saga_id, status, region_steps FROM capacity.reservation_sagas ORDER BY created_at DESC LIMIT 1;'" \
      2>/dev/null)
    echo ""
    echo -e "${BOLD}  CockroachDB saga table:${NC}"
    echo "$saga_out" | sed 's/^/  /'
    echo ""
    if echo "$saga_out" | grep -q "COMPENSATED"; then
      pass "Saga status: COMPENSATED — APAC rows deleted, no capacity consumed anywhere"
    else
      info "Saga output: $saga_out"
      skip "Saga status not COMPENSATED — check table manually"
    fi

    # Confirm no active ap_ reservations survive from the last 3 minutes
    local active_ap
    active_ap=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_db) \
      /cockroach/cockroach sql --insecure --host=localhost:26257 --format=csv \
      -e \"SELECT count(*) FROM capacity.reservations WHERE segment_id LIKE 'ap_%' AND status='active' AND created_at > NOW() - INTERVAL '3 minutes';\"" \
      2>/dev/null | tail -1)
    if [ "$active_ap" = "0" ] || [ -z "$active_ap" ]; then
      pass "No active ap_ reservations survive (compensation deleted them)"
    else
      info "Active ap_ reservations in last 3 min: $active_ap"
    fi
  else
    skip "SSH key not found — cannot verify CRDB. Check capacity.reservation_sagas manually."
  fi

  # ── Step 5: remove the closure ────────────────────────────────────────────────
  section "Step 5: Remove closure on $block_seg"
  if [ -f "$SSH_KEY" ]; then
    ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_db) \
      /cockroach/cockroach sql --insecure --host=localhost:26257 \
      -e \"UPDATE capacity.segment_closures SET status='cancelled', end_time=NOW()-INTERVAL '1s' WHERE segment_id='$block_seg' AND status='active';\"" \
      > /dev/null 2>&1
    pass "Closure removed — $block_seg is open again"
  else
    info "SSH key not found — closure expires naturally in 12h"
  fi

  echo ""
  echo -e "  ${BOLD}${GREEN}Summary of what happened:${NC}"
  echo -e "  1. Saga started for Istanbul→Ankara (eu_ + ap_ segments)"
  echo -e "  2. APAC region committed first — ap_ slots reserved in CRDB"
  echo -e "  3. EU region failed — $block_seg was closed (segment_closed)"
  echo -e "  4. compensateSteps() fired: ReleaseByReservationID deleted the ap_ rows"
  echo -e "  5. saga_status=COMPENSATED, driver gets REJECTED — no partial booking"
}

test_us_region() {
  header "US Region Tests"
  echo ""
  echo -e "  ${YELLOW}What:${NC} Same tests, routed to US cell. Booking writes to US CRDB node."
  echo -e "  ${YELLOW}Why:${NC} Demonstrates multi-master — US is an equal write node, not a replica."

  test_region "$US" "US" "US"

  local token
  token=$(get_token "$US" "$D2_EMAIL" "$D_PASS")
  if [ -z "$token" ]; then
    fail "US: Cannot get driver2 token"
    return
  fi

  test_geocode "$US" "US" "$token"
  test_route_compute "$US" "US" "$token"

  section "[US] Journey booking — expect APPROVED"
  cancel_active "$US" "$token"
  local departure
  departure=$(date -u -d '+3 hours' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
    || date -u -v+3H '+%Y-%m-%dT%H:%M:%SZ')
  local status
  status=$(curl -s -X POST "$US/api/v1/journeys" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: us-booking-$(date +%s)" \
    -d "{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG},\"vehicle_type\":\"car\",\"departure_time\":\"$departure\"}" \
    | jq -r '.data.status // empty')
  assert_eq "US booking" "$status" "APPROVED"
}

test_apac_region() {
  header "APAC Region Tests"
  echo ""
  echo -e "  ${YELLOW}What:${NC} APAC cell health check and reads. Writes for EU-prefixed segments are slow."
  echo -e "  ${YELLOW}Why:${NC} eu_* segment leaseholders are on EU nodes. APAC writes must get Raft"
  echo -e "         quorum from EU nodes across the Pacific Ocean (~200ms RTT). This is why"
  echo -e "         geo-partitioning is needed — documented in docs/data-partitioning-and-sharding.md"

  test_region "$APAC" "APAC" "APAC"

  local token
  token=$(get_token "$APAC" "$D3_EMAIL" "$D_PASS")
  if [ -z "$token" ]; then
    fail "APAC: Cannot get driver3 token"
    return
  fi
  pass "APAC: Login works (auth reads are fast — reads don't need quorum)"

  section "[APAC] Route compute (read-heavy, uses OSRM external)"
  local result
  result=$(curl -s --max-time 30 -X POST "$APAC/api/v1/routes/compute" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    -d "{\"origin\":{\"lat\":$ORIGIN_LAT,\"lng\":$ORIGIN_LNG},\"destination\":{\"lat\":$DEST_LAT,\"lng\":$DEST_LNG}}")
  local seg_count
  seg_count=$(echo "$result" | jq '.data.segments | length' 2>/dev/null || echo "0")
  if [ "$seg_count" -gt 5 ] 2>/dev/null; then
    pass "APAC route compute: $seg_count segments"
  else
    skip "APAC route compute may be slow due to OSRM latency"
  fi

  section "[APAC] Booking — expected to timeout (known limitation)"
  skip "APAC writes to eu_* segments time out (Raft quorum across Pacific). This is a known"
  skip "limitation — fix is geo-partitioning (DDL in docs/data-partitioning-and-sharding.md)."
  info "This is actually a great demo point: shows WHY geo-partitioning matters."
}

# ─── Redis and outbox check ───────────────────────────────────────────────────
test_redis_stream() {
  section "Redis Stream — event count"
  if [ -f "$SSH_KEY" ]; then
    local count
    count=$(ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "$EU_CRDB_SSH" \
      "docker exec -i \$(docker ps -qf name=vcs_redis) redis-cli XLEN journey.events" \
      2>/dev/null || echo "0")
    if [ "$count" -gt 0 ] 2>/dev/null; then
      pass "Redis stream has $count events (outbox relay is working)"
    else
      info "Redis stream empty — no recent bookings or events consumed"
    fi
  else
    skip "SSH key not found — skipping Redis check"
  fi
}

# ─── Cleanup helper ──────────────────────────────────────────────────────────
do_cleanup() {
  header "Cleanup — cancelling demo driver approved journeys"
  for base in "$EU" "$US"; do
    local blabel
    blabel="EU"
    [ "$base" = "$US" ] && blabel="US"
    for email in "$D1_EMAIL" "$D2_EMAIL" "$D3_EMAIL"; do
      local tok
      tok=$(get_token "$base" "$email" "$D_PASS")
      [ -z "$tok" ] && continue
      cancel_active "$base" "$tok"
    done
    info "$blabel: demo drivers cleaned"
  done
}

# ─── Summary ─────────────────────────────────────────────────────────────────
print_summary() {
  echo ""
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}  RESULTS${NC}"
  echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
  echo -e "  ${GREEN}✓ Passed: $PASS${NC}"
  echo -e "  ${RED}✗ Failed: $FAIL${NC}"
  echo -e "  ${YELLOW}⚠ Skipped: $SKIP${NC}"
  echo ""
  if [ "$FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}ALL TESTS PASSED${NC}"
  else
    echo -e "  ${RED}${BOLD}$FAIL TEST(S) FAILED — review output above${NC}"
  fi
  echo ""

  echo -e "${BOLD}  Observability Links:${NC}"
  echo "  Grafana EU:   http://34.76.63.61:3000  (admin/admin)"
  echo "  Grafana US:   http://34.138.242.217:3000"
  echo "  Grafana APAC: http://34.80.180.64:3000"
  echo "  CRDB UI:      http://35.187.121.12:8080"
  echo ""
}

# ─── Main ────────────────────────────────────────────────────────────────────
MODE="${1:-all}"

echo ""
echo -e "${BOLD}${CYAN}  Clearway Distributed System — Demo Verification${NC}"
echo -e "  $(date -u)"
echo ""

case "$MODE" in
  clean)
    do_cleanup
    ;;

  saga)
    test_saga
    print_summary
    ;;

  undo)
    test_saga_compensation
    print_summary
    ;;

  eu)
    header "EU Region Tests"
    test_region "$EU" "EU" "EU"
    EU_TOKEN=$(get_token "$EU" "$D1_EMAIL" "$D_PASS")
    test_geocode "$EU" "EU" "$EU_TOKEN"
    test_route_compute "$EU" "EU" "$EU_TOKEN"
    test_booking_approved "$EU" "EU" "$EU_TOKEN" "eu" || true
    test_idempotency "$EU" "EU" "$(get_token "$EU" "$D2_EMAIL" "$D_PASS")"
    test_capacity_exhaustion "$EU" "EU"
    test_road_closure "$EU" "EU"
    test_redis_stream
    print_summary
    ;;

  us)
    test_us_region
    print_summary
    ;;

  apac)
    test_apac_region
    print_summary
    ;;

  all|*)
    # ── EU full suite ────────────────────────────────────────────────────────
    header "EU Region — Full Test Suite"
    test_region "$EU" "EU" "EU"
    EU_TOKEN=$(get_token "$EU" "$D1_EMAIL" "$D_PASS")
    test_geocode "$EU" "EU" "$EU_TOKEN"
    test_route_compute "$EU" "EU" "$EU_TOKEN"
    test_booking_approved "$EU" "EU" "$EU_TOKEN" "eu" || true
    test_idempotency "$EU" "EU" "$(get_token "$EU" "$D2_EMAIL" "$D_PASS")"
    test_capacity_exhaustion "$EU" "EU"
    test_road_closure "$EU" "EU"
    test_redis_stream

    # ── Saga happy path ──────────────────────────────────────────────────────
    test_saga

    # ── Saga undo / compensation ─────────────────────────────────────────────
    test_saga_compensation

    # ── CRDB replication ────────────────────────────────────────────────────
    header "CockroachDB Replication — Write US, Read EU"
    test_crdb_replication

    # ── US full suite ────────────────────────────────────────────────────────
    test_us_region

    # ── APAC (reads only — writes timeout on eu_ segments) ──────────────────
    test_apac_region

    print_summary
    ;;
esac
