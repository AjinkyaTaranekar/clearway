# Manual & User Testing Guide — Clearway (Distributed Vehicle Capacity System)

> **Version:** 1.0  
> **Date:** 2026-04-10  
> **Author:** Deepika Nag  
> **Regions under test:** EU (35.187.121.12) · US (34.138.242.217) · APAC (34.80.180.64)  
> **App URL (EU):** http://35.187.121.12

---

## How to Use This Guide

Each test case has:
- **Prerequisites** — what must be true before you start
- **Steps** — numbered, click-by-click actions
- **Expected result** — what a passing test looks like
- **Actual result** — fill in when running; mark PASS / FAIL / BLOCKED
- **Known issues** — known bugs that will affect this test

Run all sections in order for a full regression pass. Sections marked *(Admin only)* require admin credentials.

---

## Test Credentials

| Role | Email | Password |
|------|-------|----------|
| Admin | `admin@vcs.local` | `admin123` |
| Driver (existing) | `ajinkyataranekar26@gmail.com` | `test1234` |
| Driver (new) | create during TC-REG-01 | set during test |

---

## Section 1 — Authentication

### TC-AUTH-01: Login page loads and shows region badge

**Prerequisites:** None  
**Steps:**
1. Open `http://35.187.121.12` in browser
2. Observe the page that loads

**Expected:**
- Redirects to `/auth`
- "Clearway" heading visible
- Region badge (e.g., `EU`) displayed below heading
- "Sign in" / "Create account" tabs visible
- "Quick demo access" panel with two buttons: *Sign in as driver*, *Sign in as admin*

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-02: Login form validation

**Prerequisites:** On `/auth` page  
**Steps:**
1. Click "Sign in" (submit empty form)
2. Note error messages
3. Type `notanemail` in Email field, click Sign in
4. Type valid email, type `abc` (< 8 chars) in Password, click Sign in

**Expected:**
- Empty email → "Email is required."
- Empty password → "Password is required."
- Bad email format → "Enter a valid email address."
- Short password → "Password must be at least 8 characters."
- All errors shown inline below each field in red

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-03: Driver login via credentials

**Prerequisites:** On `/auth` page  
**Steps:**
1. Enter email `ajinkyataranekar26@gmail.com`
2. Enter password `test1234`
3. Click "Sign in"

**Expected:**
- Loading spinner shows while request is in flight
- Redirects to `/driver` dashboard
- Driver dashboard header visible ("Welcome back" or similar)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-04: Admin login via demo button

**Prerequisites:** On `/auth` page (or logged out)  
**Steps:**
1. Click "Sign in as admin" demo button

**Expected:**
- Credentials auto-fill and login fires immediately
- Redirects to `/admin` dashboard
- Admin sidebar shows admin-specific items (Analytics, Enforcement, Closures, Traffic Map)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-05: Admin login via credentials

**Prerequisites:** On `/auth` page  
**Steps:**
1. Enter email `admin@vcs.local`
2. Enter password `admin123`
3. Toggle password visibility using eye icon
4. Click "Sign in"

**Expected:**
- Eye icon toggles password field between `password` and `text` type
- Redirects to `/admin` dashboard

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-06: Invalid credentials

**Prerequisites:** On `/auth` page  
**Steps:**
1. Enter email `anyone@example.com`
2. Enter password `wrongpass`
3. Click "Sign in"

**Expected:**
- Red error banner appears: "Login failed. Please check your credentials." (or similar)
- Stays on login page
- Form remains editable

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-07: Auth guard — unauthenticated direct URL access

**Prerequisites:** Not logged in (clear storage / incognito)  
**Steps:**
1. Navigate directly to `http://35.187.121.12/admin`
2. Navigate directly to `http://35.187.121.12/driver`

**Expected:**
- Both redirect immediately to `/auth`
- Admin dashboard is NOT rendered

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-08: Role guard — driver cannot access admin routes

**Prerequisites:** Logged in as driver  
**Steps:**
1. Manually navigate to `http://35.187.121.12/admin`

**Expected:**
- Redirects to `/driver` (driver's own dashboard)
- Admin UI is not shown

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-09: Driver registration — full flow

**Prerequisites:** On `/auth` page, click "Create account" tab  
**Steps:**
1. Fill in: Full name = `Test Driver`, Email = `testdriver+<timestamp>@example.com`, Password = `TestPass1`
2. Select Vehicle type = `van`
3. Enter License region = `CA`
4. Enter License number = `DL-999888`
5. Observe license preview panel
6. Click "Create account"

**Expected:**
- License preview shows Region: CA, Number: DL-999888 as you type
- Loading spinner during request
- Redirects to `/driver` dashboard on success

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-AUTH-10: Registration validation

**Prerequisites:** On "Create account" tab  
**Steps:**
1. Submit form with all fields empty
2. Submit form with password = `abc1` (< 8 chars)
3. Submit form with password = `abcdefgh` (no digit)

**Expected:**
- Empty name → "Full name is required."
- Short password → "Min 8 characters, at least 1 letter and 1 digit."
- No digit → same message

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 2 — Driver: Dashboard

### TC-DRV-DASH-01: Dashboard loads with stats and recent journeys

**Prerequisites:** Logged in as driver  
**Steps:**
1. Navigate to `/driver` (dashboard)

**Expected:**
- Page loads without errors
- Shows stat cards (total journeys, upcoming, etc.)
- Recent journeys list or empty state shown
- No console errors visible (check DevTools)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 3 — Driver: Book a Journey

### TC-BK-01: Book journey page loads

**Prerequisites:** Logged in as driver  
**Steps:**
1. Click "Book journey" in sidebar or navigate to `/driver/book`

**Expected:**
- Form with origin, destination fields
- Vehicle type pre-selected
- Departure time picker
- "Find route" or "Check route" button visible

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-BK-02: Route search — valid origin and destination

**Prerequisites:** On `/driver/book`  
**Steps:**
1. Type a valid origin address (e.g., "London Bridge, London")
2. Select from autocomplete dropdown if shown
3. Type a valid destination (e.g., "Canary Wharf, London")
4. Select departure time (any future time)
5. Click "Find route" / "Check route"

**Expected:**
- Route appears on map or segment list is shown
- Estimated travel time and distance displayed
- "Book journey" button becomes active

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-BK-03: Submit booking

**Prerequisites:** Route selected in TC-BK-02  
**Steps:**
1. With route displayed, click "Book journey" / "Submit"

**Expected:**
- Redirects to `/driver/booking-result`
- Result page shows booking status (pending / approved / rejected)
- Journey ID visible
- "View journey" link available

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-BK-04: Booking result for a capacity-rejected journey

**Prerequisites:** Repeat booking to a segment at or near capacity  
**Steps:**
1. Book the same origin-destination route multiple times until capacity is reached

**Expected:**
- At least one booking returns with status `rejected`
- Rejection reason is shown on booking result page
- StatusChip shows red "Rejected" label

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Known issues:** Requires segment near capacity; hard to force in manual test

---

## Section 4 — Driver: My Journeys

### TC-JRN-01: Journey list loads

**Prerequisites:** Logged in as driver, at least one booking made  
**Steps:**
1. Navigate to `/driver/journeys`

**Expected:**
- List of journeys with status chips
- Each row shows origin, destination, departure time, status
- StatusChip colors: green (active), grey (pending), red (rejected), etc.

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-JRN-02: Journey detail page

**Prerequisites:** At least one journey exists  
**Steps:**
1. Click any journey in the list
2. Navigates to `/driver/journeys/:id`

**Expected:**
- Journey detail shows full info: status, vehicle type, segment list
- Timeline events visible (created, approved/rejected, etc.)
- Map path shown if available
- No crash for any status (pending / approved / rejected / expired / cancelled)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-JRN-03: Expired journey status renders correctly

**Prerequisites:** A journey with status `expired` exists  
**Steps:**
1. View any journey with status `expired`
2. Check StatusChip renders

**Expected:**
- StatusChip shows "Expired" with grey/muted styling
- No crash (`Cannot read properties of undefined (reading 'bg')`)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Note:** This was a known crash — fix was deployed; verify it holds.

---

## Section 5 — Driver: Notifications

### TC-NOTIF-01: Notifications page loads

**Prerequisites:** Logged in as driver  
**Steps:**
1. Navigate to `/driver/notifications`

**Expected:**
- Notification list or empty state
- Read / unread distinction visible
- No 401 or server error banner (requires valid auth token)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-NOTIF-02: Notification appears after booking

**Prerequisites:** Make a booking (TC-BK-03), then check notifications  
**Steps:**
1. Book a journey
2. Navigate to notifications page

**Expected:**
- Notification about booking created or status change is visible

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 6 — Driver: Settings

### TC-SET-01: Settings page loads

**Prerequisites:** Logged in as driver  
**Steps:**
1. Navigate to `/driver/settings`

**Expected:**
- Profile information displayed (name, email, vehicle type, license)
- Form fields editable or read-only display
- No crash or error

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 7 — Admin: Dashboard

### TC-ADM-DASH-01: Admin dashboard loads with KPIs

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin`

**Expected:**
- KPI cards: total journeys, active, pending, approved, rejected
- Recent journeys table or list
- Region badge in header shows which region this node serves
- No console errors

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Known issues:** US and APAC nodes may display "EU" region badge (BUG-1 — pipeline not re-deployed for those regions)

---

### TC-ADM-DASH-02: Dashboard auto-refreshes or shows live data

**Prerequisites:** Logged in as admin  
**Steps:**
1. In a separate tab, log in as driver and submit a new booking
2. Return to admin dashboard
3. Refresh or wait for auto-refresh

**Expected:**
- New booking appears in pending journeys list or counter increments

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 8 — Admin: All Journeys

### TC-ADM-JRN-01: All journeys list

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/journeys`

**Expected:**
- Tabular list of all journeys in the system
- Columns: driver name, origin, destination, status, departure time
- Status chips correctly colored for each status

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-ADM-JRN-02: Approve a pending journey

**Prerequisites:** At least one journey with status `pending` exists  
**Steps:**
1. Open a pending journey from `/admin/journeys`
2. Click "Approve" button

**Expected:**
- Journey status changes to `approved`
- StatusChip updates to green "Approved"
- Toast notification confirms action
- Driver receives a notification (check `/driver/notifications`)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-ADM-JRN-03: Reject a pending journey with reason

**Prerequisites:** At least one journey with status `pending`  
**Steps:**
1. Open a pending journey
2. Click "Reject"
3. Enter rejection reason: "Road segment at capacity"
4. Confirm

**Expected:**
- Status changes to `rejected`
- Rejection reason visible on the journey detail page
- Driver's journey detail also shows rejection reason

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-ADM-JRN-04: Journey detail — all status chip colors render

**Prerequisites:** Have at least one journey in each status  
**Steps:**
1. View journeys with statuses: pending, approved, rejected, active, completed, cancelled, expired
2. Check StatusChip for each

**Expected:**
- Each status has distinct color and correct label
- No undefined error / crash

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 9 — Admin: Analytics

### TC-ADM-ANA-01: Analytics page loads with data

**Prerequisites:** Logged in as admin, some journeys exist  
**Steps:**
1. Navigate to `/admin/analytics`

**Expected:**
- 6 KPI stat cards: Total bookings, Approval rate, Rejection rate, Active journeys, Cancellations, Completed
- Values load (not stuck at "Loading…")
- Pie chart and area chart render
- Default time window is "Last 24 hours"

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-ADM-ANA-02: Time window filter works

**Prerequisites:** On analytics page with data  
**Steps:**
1. Click "Last hour" button
2. Note KPI values
3. Click "Last 7 days"
4. Note KPI values

**Expected:**
- Values change when time window changes (or show 0 if no data in that period)
- Charts re-render
- No crash or stale data

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-ADM-ANA-03: Analytics error state

**Prerequisites:** Analytics page, simulate network error (DevTools → Network → Offline)  
**Steps:**
1. Set browser offline
2. Toggle time window (triggers refetch)

**Expected:**
- Error banner shown in red: "Analytics unavailable"
- Toast notification appears
- KPI cards show "-" instead of numbers

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 10 — Admin: Enforcement

### TC-ENF-01: Enforcement page loads

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/enforcement`

**Expected:**
- Page renders with enforcement/verification tools
- No 401 or crash

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 11 — Admin: Segment Closures

### TC-CLS-01: Closures page loads with segments and active closures

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/closures`

**Expected:**
- "Create closure" form on left: segment dropdown populated, duration input (default 60), reason textarea
- "Active closures" list on right (empty state if none)
- No error banner on initial load

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-CLS-02: Create a segment closure

**Prerequisites:** On closures page, segments loaded in dropdown  
**Steps:**
1. Select any segment from the dropdown
2. Set duration to `30` minutes
3. Enter reason: "Emergency road maintenance"
4. Click "Create closure"

**Expected:**
- Toast: "Segment closure created — Segment [id] blocked for 30 minutes"
- New closure appears in "Active closures" list with correct segment name, reason, and time range
- Reason field clears after submission

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-CLS-03: Closure form validation

**Prerequisites:** On closures page  
**Steps:**
1. Leave reason empty, click "Create closure"

**Expected:**
- Toast error: "Reason is required for closures."
- No closure created

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-CLS-04: Closed segment rejects new journey bookings

**Prerequisites:** A segment closure has been created for a known segment  
**Steps:**
1. Log in as driver
2. Book a journey that uses the closed segment
3. Check booking result

**Expected:**
- Booking is rejected with a reason referencing the closure or capacity
- No journey approved through a closed segment

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-CLS-05: Refresh button reloads closure data

**Prerequisites:** On closures page  
**Steps:**
1. Click the "Refresh" button (top right)

**Expected:**
- Spinning icon animates briefly
- Closure list reloads
- No error

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 12 — Admin: Traffic Map

### TC-MAP-01: Traffic map page loads

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/map`

**Expected:**
- Map renders (Google Maps or custom map component)
- Road segments visible with colour-coded traffic levels (low = green, critical = red)
- No crash or blank screen

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-MAP-02: Segment occupancy data visible

**Prerequisites:** On traffic map  
**Steps:**
1. Click or hover a segment on the map

**Expected:**
- Tooltip or sidebar shows segment name, region, occupancy %, traffic level

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 13 — Admin: Notifications

### TC-ADM-NOTIF-01: Admin notifications page loads

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/notifications`

**Expected:**
- Notifications list or empty state
- System notifications visible (e.g., new booking submitted)
- Auth token accepted (no 401 error)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 14 — Admin: Settings

### TC-ADM-SET-01: Admin settings page loads

**Prerequisites:** Logged in as admin  
**Steps:**
1. Navigate to `/admin/settings`

**Expected:**
- Admin profile or system settings displayed
- No crash

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 15 — Cross-Cutting / Security

### TC-SEC-01: JWKS endpoint returns JSON

**Prerequisites:** None (no auth required)  
**Steps:**
1. Open `http://35.187.121.12/.well-known/jwks.json` in browser
2. Check Content-Type header (DevTools → Network)

**Expected:**
- `200 OK`
- `Content-Type: application/json`
- Body is a valid JWKS with `keys` array containing an RSA public key

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-SEC-02: Admin auth endpoints routed to IAM (not journey service)

**Prerequisites:** None  
**Steps:**
1. `GET http://35.187.121.12/api/v1/admin/auth/users` (no auth header)

**Expected:**
- `401 Unauthorized` from IAM service
- NOT a 404 from journey service

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-SEC-03: Security headers on API responses

**Prerequisites:** None  
**Steps:**
1. `GET http://35.187.121.12/api/v1/capacity/segments`
2. Check response headers

**Expected:**
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-SEC-04: Security headers on SPA (HTML response)

**Prerequisites:** None  
**Steps:**
1. `GET http://35.187.121.12/`
2. Check response headers

**Expected:**
- Same security headers as TC-SEC-03 above

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Known issues:** ISSUE-04 — nginx `add_header` inheritance bug. Security headers are currently MISSING from SPA responses. This test is expected to FAIL until fixed.

---

### TC-SEC-05: Rate limiting on auth endpoints

**Prerequisites:** None (can use curl)  
**Steps:**
1. Send 15+ rapid POST requests to `/api/v1/auth/login`

**Expected:**
- After ~10 requests, responses return `429 Too Many Requests`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-SEC-06: Unauthenticated access to protected API routes

**Prerequisites:** None  
**Steps:**
1. `GET /api/v1/journeys` — no Authorization header
2. `GET /api/v1/notifications` — no Authorization header
3. `GET /api/v1/enforcement/verify` — no Authorization header

**Expected:**
- All return `401 Unauthorized`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 16 — Multi-Region

### TC-REGION-01: EU region badge

**Prerequisites:** None  
**Steps:**
1. Open `http://35.187.121.12/auth`
2. Check region badge

**Expected:**
- Badge shows `EU`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-REGION-02: US region badge

**Prerequisites:** None  
**Steps:**
1. Open `http://34.138.242.217/auth`
2. Check region badge

**Expected:**
- Badge shows `US`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Known issues:** BUG-1 — US node currently shows `EU` until pipeline re-deploys with `REGION=US`

---

### TC-REGION-03: APAC region badge

**Prerequisites:** None  
**Steps:**
1. Open `http://34.80.180.64/auth`
2. Check region badge

**Expected:**
- Badge shows `APAC`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED  
**Known issues:** BUG-1 — APAC node currently shows `EU`

---

### TC-REGION-04: All regions serve the SPA

**Steps:**
1. `GET http://34.138.242.217/` → expect `200 text/html`
2. `GET http://34.80.180.64/` → expect `200 text/html`

**Expected:** Both return HTML with `<title>` containing "Clearway" or app name

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 17 — 404 and Error Pages

### TC-404-01: Unknown route shows custom 404 page

**Prerequisites:** Logged in or out  
**Steps:**
1. Navigate to `http://35.187.121.12/nonexistent-route`

**Expected:**
- Custom 404 page: "Page not found" heading, map emoji, "Go to sign in" link
- Link navigates to `/auth`

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 18 — Navigation and Layout

### TC-NAV-01: Driver sidebar navigation

**Prerequisites:** Logged in as driver  
**Steps:**
1. Click each sidebar link: Dashboard, Book journey, My journeys, Notifications, Settings
2. Verify each page loads without crash

**Expected:**
- All 5 pages render
- Active link is highlighted in sidebar
- No crash on any page

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-NAV-02: Admin sidebar navigation

**Prerequisites:** Logged in as admin  
**Steps:**
1. Click each sidebar link: Dashboard, Journeys, Analytics, Enforcement, Closures, Traffic Map, Notifications, Settings

**Expected:**
- All 8 pages render
- Active link highlighted
- No crash on any page

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-NAV-03: Logout

**Prerequisites:** Logged in (either role)  
**Steps:**
1. Find and click "Logout" / "Sign out" in sidebar or header
2. Observe redirect

**Expected:**
- Redirected to `/auth`
- Attempting to navigate back to `/admin` or `/driver` redirects to `/auth` again (token cleared)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Section 19 — Mobile / Responsive

### TC-RESP-01: Login page on mobile viewport (375 × 812)

**Prerequisites:** DevTools → Device emulation (iPhone 13)  
**Steps:**
1. Open `/auth` at 375 px width
2. Check form layout, demo buttons

**Expected:**
- Form fills screen without horizontal scroll
- Demo buttons stack vertically (single column)
- Inputs and submit button fully tappable (≥ 44 px height)

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

### TC-RESP-02: Admin analytics on tablet viewport (768 × 1024)

**Prerequisites:** DevTools → iPad  
**Steps:**
1. Open `/admin/analytics` at 768 px

**Expected:**
- KPI cards collapse to 2-column grid
- Charts render without clipping
- No horizontal overflow

**Actual:** ______  
**Result:** PASS / FAIL / BLOCKED

---

## Summary Checklist

| # | Test Case | Result |
|---|-----------|--------|
| AUTH-01 | Login page loads, region badge | |
| AUTH-02 | Login form validation | |
| AUTH-03 | Driver login via credentials | |
| AUTH-04 | Admin login via demo button | |
| AUTH-05 | Admin login via credentials + eye toggle | |
| AUTH-06 | Invalid credentials error | |
| AUTH-07 | Auth guard — unauthenticated direct access | |
| AUTH-08 | Role guard — driver blocked from /admin | |
| AUTH-09 | Driver registration full flow | |
| AUTH-10 | Registration validation | |
| DRV-DASH-01 | Driver dashboard loads | |
| BK-01 | Book journey page loads | |
| BK-02 | Route search valid input | |
| BK-03 | Submit booking | |
| BK-04 | Rejected booking result | |
| JRN-01 | Journey list loads | |
| JRN-02 | Journey detail page | |
| JRN-03 | Expired status renders (no crash) | |
| NOTIF-01 | Driver notifications loads | |
| NOTIF-02 | Notification after booking | |
| SET-01 | Driver settings page | |
| ADM-DASH-01 | Admin dashboard loads with KPIs | |
| ADM-DASH-02 | Dashboard shows new booking | |
| ADM-JRN-01 | All journeys list | |
| ADM-JRN-02 | Approve a journey | |
| ADM-JRN-03 | Reject a journey with reason | |
| ADM-JRN-04 | All status chip colors | |
| ADM-ANA-01 | Analytics loads with data | |
| ADM-ANA-02 | Time window filter works | |
| ADM-ANA-03 | Analytics error state | |
| ENF-01 | Enforcement page loads | |
| CLS-01 | Closures page loads | |
| CLS-02 | Create segment closure | |
| CLS-03 | Closure form validation | |
| CLS-04 | Closed segment rejects bookings | |
| CLS-05 | Refresh reloads data | |
| MAP-01 | Traffic map loads | |
| MAP-02 | Segment occupancy visible | |
| ADM-NOTIF-01 | Admin notifications loads | |
| ADM-SET-01 | Admin settings loads | |
| SEC-01 | JWKS returns JSON | |
| SEC-02 | Admin auth → IAM (not journey) | |
| SEC-03 | Security headers on API | |
| SEC-04 | Security headers on SPA *(expected FAIL)* | |
| SEC-05 | Rate limiting on auth | |
| SEC-06 | 401 on unauthenticated API calls | |
| REGION-01 | EU badge correct | |
| REGION-02 | US badge correct *(expected FAIL — BUG-1)* | |
| REGION-03 | APAC badge correct *(expected FAIL — BUG-1)* | |
| REGION-04 | All regions serve SPA | |
| 404-01 | Custom 404 page | |
| NAV-01 | Driver sidebar navigation | |
| NAV-02 | Admin sidebar navigation | |
| NAV-03 | Logout clears session | |
| RESP-01 | Mobile login page | |
| RESP-02 | Tablet analytics page | |

---

## Known Failures (Do Not Raise as New Bugs)

| Test | Expected Failure | Tracking |
|------|-----------------|----------|
| TC-SEC-04 | Security headers missing from SPA — nginx `add_header` inheritance bug | ISSUE-04 in FULL_ISSUES_REPORT.md |
| TC-REGION-02 | US shows `EU` — REGION env var not set in pipeline for US deploy | BUG-1 |
| TC-REGION-03 | APAC shows `EU` — same root cause | BUG-1 |
| TC-BK-02 | Map route API may fail if `POST /map/route` returns 405 | ISSUE-05 |

---

## How to Report New Bugs Found During Testing

Include in the bug report:

1. **Test case ID** that uncovered it
2. **Region** (EU / US / APAC)
3. **Browser and version**
4. **Steps to reproduce** (copy from test case, add specifics)
5. **Expected vs actual result**
6. **Screenshot or network log** if relevant
7. **Severity**: Critical / High / Medium / Low
