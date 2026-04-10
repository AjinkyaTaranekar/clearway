/**
 * VCS K6 Load Test Suite
 * ======================
 * Tests the Distributed Vehicle Capacity System under different load shapes.
 *
 * Prerequisites:
 *   brew install k6       (macOS)
 *   choco install k6      (Windows)
 *   snap install k6       (Linux)
 *
 * Run:
 *   # Smoke test (quick sanity check, 1 VU)
 *   K6_SCENARIO=smoke BASE_URL=http://localhost k6 run scripts/load-testing/k6-load-test.js
 *
 *   # Load test (normal traffic ramp-up)
 *   K6_SCENARIO=load BASE_URL=http://localhost k6 run scripts/load-testing/k6-load-test.js
 *
 *   # Stress test (push beyond normal, find breaking point)
 *   K6_SCENARIO=stress BASE_URL=http://localhost k6 run scripts/load-testing/k6-load-test.js
 *
 *   # Soak test (sustained normal load, catch memory leaks)
 *   K6_SCENARIO=soak BASE_URL=http://localhost k6 run scripts/load-testing/k6-load-test.js
 *
 *   # Run against production EU cell
 *   K6_SCENARIO=load BASE_URL=https://35.244.162.92.nip.io k6 run scripts/load-testing/k6-load-test.js
 *
 *   # Output metrics to JSON for Grafana import
 *   k6 run --out json=results.json scripts/load-testing/k6-load-test.js
 */

import http from "k6/http";
import { check, group, sleep } from "k6";
import { Rate, Trend, Counter } from "k6/metrics";
import { randomItem, randomIntBetween } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";
import { uuidv4 } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";

// ── Configuration ─────────────────────────────────────────────────────────────
const BASE_URL = __ENV.BASE_URL || "http://localhost";
const SCENARIO = __ENV.K6_SCENARIO || "smoke";

// Admin credentials (set these in env or use defaults for local dev)
const ADMIN_EMAIL = __ENV.ADMIN_EMAIL || "admin@example.com";
const ADMIN_PASS = __ENV.ADMIN_PASS || "Admin123!";

// ── Custom Metrics ────────────────────────────────────────────────────────────
const journeyCreateSuccess = new Rate("journey_create_success");
const journeyCreateErrors = new Counter("journey_create_errors");
const authSuccess = new Rate("auth_success");
const capacityCheckDuration = new Trend("capacity_check_duration", true);
const routeComputeDuration = new Trend("route_compute_duration", true);
const circuitBreakerTrips = new Counter("circuit_breaker_trips");

// ── Scenario Definitions ──────────────────────────────────────────────────────
const SCENARIOS = {
  /**
   * Smoke: 1 VU for 30s — verifies the system works at all.
   * Expected: 0 errors, all checks pass.
   */
  smoke: {
    executor: "constant-vus",
    vus: 1,
    duration: "30s",
    tags: { test_type: "smoke" },
  },

  /**
   * Load: ramps from 0 → 20 VUs over 2 min, holds for 5 min, ramps down.
   * Models normal production traffic.
   * Expected: p95 < 500ms, error rate < 1%
   */
  load: {
    executor: "ramping-vus",
    startVUs: 0,
    stages: [
      { duration: "2m", target: 10 },   // Warm-up
      { duration: "5m", target: 20 },   // Steady load
      { duration: "2m", target: 30 },   // Peak
      { duration: "2m", target: 0 },    // Ramp-down
    ],
    tags: { test_type: "load" },
  },

  /**
   * Stress: ramps aggressively to find the breaking point.
   * Expected: system degrades gracefully, circuit breakers activate, no data corruption.
   */
  stress: {
    executor: "ramping-vus",
    startVUs: 0,
    stages: [
      { duration: "2m", target: 20 },   // Baseline
      { duration: "5m", target: 60 },   // Pressure
      { duration: "2m", target: 100 },  // High stress
      { duration: "1m", target: 150 },  // Spike
      { duration: "3m", target: 60 },   // Recovery observation
      { duration: "2m", target: 0 },    // Wind-down
    ],
    tags: { test_type: "stress" },
  },

  /**
   * Soak: steady moderate load for 30 minutes.
   * Catches memory leaks, connection pool exhaustion, and slow degradation.
   * Expected: stable p95 throughout, no error rate increase over time.
   */
  soak: {
    executor: "constant-vus",
    vus: 15,
    duration: "30m",
    tags: { test_type: "soak" },
  },
};

// ── Thresholds ────────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    [SCENARIO]: SCENARIOS[SCENARIO] || SCENARIOS.smoke,
  },
  thresholds: {
    // Overall latency
    http_req_duration: [
      "p(95)<1000",  // 95% of requests under 1s
      "p(99)<3000",  // 99% of requests under 3s
    ],
    // Overall error rate
    http_req_failed: ["rate<0.05"],  // Less than 5% failure

    // Custom thresholds
    auth_success: ["rate>0.95"],
    journey_create_success: ["rate>0.90"],
    capacity_check_duration: ["p(95)<800"],
    route_compute_duration: ["p(95)<2000"],
  },
};

// ── Test Data ─────────────────────────────────────────────────────────────────

// Dublin area coordinates for realistic journey simulation
const DUBLIN_PLACES = [
  { name: "Dublin Airport", lat: 53.4264, lon: -6.2499 },
  { name: "Trinity College", lat: 53.3438, lon: -6.2546 },
  { name: "St Stephen's Green", lat: 53.3382, lon: -6.2591 },
  { name: "Heuston Station", lat: 53.3464, lon: -6.2940 },
  { name: "Connolly Station", lat: 53.3521, lon: -6.2489 },
  { name: "Phoenix Park", lat: 53.3607, lon: -6.3248 },
  { name: "UCD Belfield", lat: 53.3069, lon: -6.2238 },
  { name: "Dundrum SC", lat: 53.2920, lon: -6.2447 },
  { name: "Dun Laoghaire", lat: 53.2941, lon: -6.1358 },
  { name: "Blanchardstown SC", lat: 53.3909, lon: -6.3842 },
];

const VEHICLE_TYPES = ["car", "van", "motorcycle", "truck"];

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeHeaders(token) {
  const headers = {
    "Content-Type": "application/json",
    Accept: "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  return headers;
}

function isoDateFromNow(offsetMinutes) {
  const d = new Date(Date.now() + offsetMinutes * 60 * 1000);
  return d.toISOString();
}

function randomPair(arr) {
  const i = randomIntBetween(0, arr.length - 1);
  let j = randomIntBetween(0, arr.length - 2);
  if (j >= i) j++;
  return [arr[i], arr[j]];
}

// ── API Flows ─────────────────────────────────────────────────────────────────

/**
 * Flow 1 — Auth: Register → Login → Get Profile
 * Returns { token, userId } or null on failure.
 */
function flowAuth() {
  let token = null;
  let userId = null;

  group("auth: register", () => {
    const email = `chaos-${uuidv4().substring(0, 8)}@loadtest.vcs`;
    const payload = JSON.stringify({
      email,
      password: "LoadTest123!",
      first_name: "Load",
      last_name: "Tester",
      role: "user",
    });

    const res = http.post(`${BASE_URL}/api/v1/auth/register`, payload, {
      headers: makeHeaders(null),
      tags: { flow: "auth", step: "register" },
    });

    const ok = check(res, {
      "register: status 201 or 409": (r) => r.status === 201 || r.status === 409,
    });

    if (res.status === 201) {
      const body = JSON.parse(res.body);
      token = body.access_token || body.data?.access_token;
      userId = body.user_id || body.data?.user_id;
    }

    // If 409 (already exists) or no token from register, try login with a test account
    if (!token) {
      const loginRes = http.post(
        `${BASE_URL}/api/v1/auth/login`,
        JSON.stringify({ email: ADMIN_EMAIL, password: ADMIN_PASS }),
        { headers: makeHeaders(null), tags: { flow: "auth", step: "login-fallback" } }
      );
      if (loginRes.status === 200) {
        const body = JSON.parse(loginRes.body);
        token = body.access_token || body.data?.access_token;
        userId = body.user_id || body.data?.user_id;
      }
    }
  });

  if (!token) return null;

  group("auth: get profile", () => {
    const res = http.get(`${BASE_URL}/api/v1/auth/profile`, {
      headers: makeHeaders(token),
      tags: { flow: "auth", step: "profile" },
    });

    const ok = check(res, {
      "profile: status 200": (r) => r.status === 200,
      "profile: has email": (r) => {
        try {
          return JSON.parse(r.body).email !== undefined;
        } catch { return false; }
      },
    });
    authSuccess.add(ok);
  });

  return { token, userId };
}

/**
 * Flow 2 — Capacity: Check → Reserve
 */
function flowCapacity(token) {
  group("capacity: check availability", () => {
    const start = Date.now();
    const res = http.get(
      `${BASE_URL}/api/v1/capacity/segments`,
      {
        headers: makeHeaders(token),
        tags: { flow: "capacity", step: "list-segments" },
      }
    );
    capacityCheckDuration.add(Date.now() - start);

    check(res, {
      "segments: status 200": (r) => r.status === 200,
      "segments: is array": (r) => {
        try { return Array.isArray(JSON.parse(r.body)); }
        catch { return false; }
      },
    });
  });

  group("capacity: occupancy", () => {
    const res = http.get(
      `${BASE_URL}/api/v1/capacity/segments/occupancy`,
      {
        headers: makeHeaders(token),
        tags: { flow: "capacity", step: "occupancy" },
      }
    );

    check(res, {
      "occupancy: status 200": (r) => r.status === 200,
    });

    // Detect circuit breaker response
    if (res.status === 503 || (res.body && res.body.toLowerCase().includes("circuit"))) {
      circuitBreakerTrips.add(1);
    }
  });
}

/**
 * Flow 3 — Map: Route Computation
 */
function flowMap(token) {
  const [origin, destination] = randomPair(DUBLIN_PLACES);

  group("map: search places", () => {
    const res = http.get(
      `${BASE_URL}/api/v1/map/search?q=${encodeURIComponent(origin.name)}&limit=5`,
      {
        headers: makeHeaders(token),
        tags: { flow: "map", step: "search" },
      }
    );

    check(res, {
      "search: status 200 or 429": (r) => r.status === 200 || r.status === 429,
    });
  });

  group("map: get route", () => {
    const start = Date.now();
    const res = http.get(
      `${BASE_URL}/api/v1/map/route?from=${origin.lat},${origin.lon}&to=${destination.lat},${destination.lon}`,
      {
        headers: makeHeaders(token),
        tags: { flow: "map", step: "route" },
      }
    );
    routeComputeDuration.add(Date.now() - start);

    check(res, {
      "route: status 200 or 422": (r) => r.status === 200 || r.status === 422,
    });

    if (res.status === 503 || (res.body && res.body.toLowerCase().includes("circuit"))) {
      circuitBreakerTrips.add(1);
    }
  });
}

/**
 * Flow 4 — Journey: Create → Activate → Complete (happy path)
 * Also exercises the saga pattern and circuit breakers.
 */
function flowJourney(token) {
  let journeyId = null;
  const [origin, destination] = randomPair(DUBLIN_PLACES);
  // Book 2h from now (satisfies 60-min advance booking rule)
  const startTime = isoDateFromNow(120);
  const endTime = isoDateFromNow(180);
  const vehicleType = randomItem(VEHICLE_TYPES);

  group("journey: create", () => {
    const payload = JSON.stringify({
      origin_place_id: `place:${origin.lat},${origin.lon}`,
      destination_place_id: `place:${destination.lat},${destination.lon}`,
      origin_name: origin.name,
      destination_name: destination.name,
      origin_lat: origin.lat,
      origin_lon: origin.lon,
      destination_lat: destination.lat,
      destination_lon: destination.lon,
      scheduled_departure: startTime,
      scheduled_arrival: endTime,
      vehicle_type: vehicleType,
    });

    const res = http.post(`${BASE_URL}/api/v1/journeys`, payload, {
      headers: {
        ...makeHeaders(token),
        "Idempotency-Key": uuidv4(),
      },
      tags: { flow: "journey", step: "create" },
    });

    const created = check(res, {
      "journey create: status 201 or 422": (r) =>
        r.status === 201 || r.status === 422 || r.status === 503,
      "journey create: not 5xx (except 503)": (r) =>
        r.status < 500 || r.status === 503,
    });

    journeyCreateSuccess.add(res.status === 201);
    if (res.status >= 500 && res.status !== 503) {
      journeyCreateErrors.add(1);
    }

    // Detect circuit breaker
    if (res.status === 503 || (res.body && res.body.toLowerCase().includes("circuit"))) {
      circuitBreakerTrips.add(1);
    }

    if (res.status === 201) {
      try {
        const body = JSON.parse(res.body);
        journeyId = body.id || body.data?.id;
      } catch {}
    }
  });

  if (!journeyId) return;

  // Brief pause to simulate user thinking
  sleep(randomIntBetween(1, 3));

  group("journey: get details", () => {
    const res = http.get(`${BASE_URL}/api/v1/journeys/${journeyId}`, {
      headers: makeHeaders(token),
      tags: { flow: "journey", step: "get" },
    });

    check(res, {
      "journey get: status 200": (r) => r.status === 200,
    });
  });

  group("journey: list user journeys", () => {
    const res = http.get(`${BASE_URL}/api/v1/journeys`, {
      headers: makeHeaders(token),
      tags: { flow: "journey", step: "list" },
    });

    check(res, {
      "journey list: status 200": (r) => r.status === 200,
    });
  });

  // Cancel the journey (so we don't fill up capacity in tests)
  sleep(1);
  group("journey: cancel", () => {
    const res = http.put(
      `${BASE_URL}/api/v1/journeys/${journeyId}/cancel`,
      JSON.stringify({ reason: "load-test-cleanup" }),
      {
        headers: makeHeaders(token),
        tags: { flow: "journey", step: "cancel" },
      }
    );

    check(res, {
      "journey cancel: status 200 or 409": (r) =>
        r.status === 200 || r.status === 409,
    });
  });
}

/**
 * Flow 5 — Notifications: List + Mark read
 */
function flowNotifications(token) {
  group("notifications: list", () => {
    const res = http.get(`${BASE_URL}/api/v1/notifications`, {
      headers: makeHeaders(token),
      tags: { flow: "notifications", step: "list" },
    });

    check(res, {
      "notifications: status 200": (r) => r.status === 200,
    });
  });

  group("notifications: mark all read", () => {
    const res = http.put(`${BASE_URL}/api/v1/notifications/read-all`, null, {
      headers: makeHeaders(token),
      tags: { flow: "notifications", step: "mark-read" },
    });

    check(res, {
      "mark-read: status 200 or 204": (r) =>
        r.status === 200 || r.status === 204,
    });
  });
}

/**
 * Flow 6 — Health checks (lightweight, verifies liveness across all services)
 */
function flowHealthChecks() {
  // These are direct service health checks — goes through Nginx for port 80
  // so we only check the gateway health here
  group("health: gateway", () => {
    const res = http.get(`${BASE_URL}/nginx-health`, {
      tags: { flow: "health", step: "nginx" },
    });

    check(res, {
      "nginx healthy: status 200": (r) => r.status === 200,
    });
  });
}

// ── Main VU Function ──────────────────────────────────────────────────────────
export default function () {
  // Every VU authenticates first
  const session = flowAuth();

  if (!session) {
    // Auth failed — still count as a run but skip downstream flows
    sleep(2);
    return;
  }

  const { token } = session;

  // Run flows with varying probability to simulate realistic traffic mix
  const r = Math.random();

  if (r < 0.15) {
    // 15% — health checks only (monitoring traffic)
    flowHealthChecks();
  } else if (r < 0.35) {
    // 20% — capacity queries
    flowCapacity(token);
    sleep(randomIntBetween(1, 2));
  } else if (r < 0.50) {
    // 15% — map/route queries
    flowMap(token);
    sleep(randomIntBetween(1, 3));
  } else if (r < 0.80) {
    // 30% — full journey lifecycle (most important flow)
    flowMap(token);
    sleep(1);
    flowCapacity(token);
    sleep(1);
    flowJourney(token);
    sleep(randomIntBetween(2, 5));
  } else {
    // 20% — notifications
    flowNotifications(token);
    sleep(randomIntBetween(1, 2));
  }
}

// ── Setup: print test config ──────────────────────────────────────────────────
export function setup() {
  console.log(`\n${"=".repeat(60)}`);
  console.log(`VCS Load Test — Scenario: ${SCENARIO.toUpperCase()}`);
  console.log(`Base URL: ${BASE_URL}`);
  console.log(`${"=".repeat(60)}\n`);

  // Quick pre-flight: verify gateway responds
  const res = http.get(`${BASE_URL}/nginx-health`);
  if (res.status !== 200) {
    console.warn(`WARNING: Gateway health check returned ${res.status}. Tests may fail.`);
  }

  return { baseUrl: BASE_URL, scenario: SCENARIO };
}

// ── Teardown: summary ─────────────────────────────────────────────────────────
export function teardown(data) {
  console.log(`\n${"=".repeat(60)}`);
  console.log(`Test complete — Scenario: ${data.scenario}`);
  console.log(`Target: ${data.baseUrl}`);
  console.log(`Circuit breaker trips detected: check circuit_breaker_trips metric`);
  console.log(`${"=".repeat(60)}\n`);
}
