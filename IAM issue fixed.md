# IAM Service — Issues Fixed by Deepika Nag

> **Audit Reference:** AUDIT_REPORT.md  
> **Issues:** F-11, U-01, U-02, U-03, U-11  
> **Date:** 2026-04-08

---

## F-11 — Enforcement Role: IAM Cannot Issue Enforcement Tokens

**Audit finding:** `enforcement` role is referenced in journey middleware but IAM only issues `driver` and `admin`.

**Resolution:** `EnforcementOnly` at `journey-service/internal/middleware/auth.go:236` already checks `role != "enforcement" && role != "admin"` — admin tokens pass through. IAM issuing only `driver`/`admin` is correct and sufficient. No IAM code change needed.

**Status:** ✅ Resolved — no code change required on IAM side.

---

## U-01 — Only One Vehicle Per Driver; No Way to Update It

**Audit finding:** No backend path to change vehicle type; booking picker always starts blank.

**Changes made:**

- `frontend/src/app/pages/driver/SettingsPage.tsx:15` — `vehicleType` state initialised from `user?.vehicle_type`
- `frontend/src/app/pages/driver/SettingsPage.tsx:31–32` — `handleSave` builds `fields` object and includes `vehicle_type` only when it has changed
- `frontend/src/app/context/AppContext.tsx:397–404` — `updateProfile({ name, vehicle_type })` calls `iamUpdateProfile`, updates `user` state and `localStorage`
- IAM backend `UserRepo.UpdateProfile` (`iam-service/internal/repository/user_repo.go:235`) builds a dynamic `SET vehicle_type = $N` clause and writes to the master DB (pre-existing, confirmed wired)

**Result:** Drivers can now change their registered vehicle type from the Settings page — it persists to PostgreSQL via `PUT /api/v1/auth/profile`.

**Status:** ✅ Fixed.

---

## U-02 — Licence Number Never Validated

**Audit finding:** Any string passes registration; no format or length check on the licence number field.

**Changes made:** `iam-service/internal/http/handlers/auth_handler.go:20–22` and `86–92`

```go
// licenseRegex permits 2–30 alphanumeric characters with optional hyphens/spaces.
// Covers common formats: "DL-123456", "AB 1234567", "CDL123456".
var licenseRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\- ]{0,28}[A-Za-z0-9]$|^[A-Za-z0-9]{1}$`)

// In Register handler:
licenseNum := strings.TrimSpace(req.LicenseInfo.LicenseNumber)
req.LicenseInfo.LicenseNumber = licenseNum
if licenseNum == "" {
    errs = append(errs, fe{"license_info.license_number", "Required."})
} else if len(licenseNum) > 30 || !licenseRegex.MatchString(licenseNum) {
    errs = append(errs, fe{"license_info.license_number", "Must be 1–30 alphanumeric characters (hyphens and spaces allowed)."})
}
```

- Trims whitespace before validation
- Rejects empty, whitespace-only, special-character, and >30-char values
- Returns a field-level error in the same `VALIDATION_ERROR` response envelope as all other register validation checks

**Status:** ✅ Fixed.

---

## U-03 — Vehicle Type Blank in Booking Form on Every Visit

**Audit finding:** `BookJourneyPage` never reads `user.vehicle_type`; the vehicle type picker always starts empty, forcing drivers to re-select their vehicle on every booking.

**Changes made:** `frontend/src/app/pages/driver/BookJourneyPage.tsx:30–53`

```typescript
// Map IAM vehicle type values (lowercase) to booking page display values.
// The booking page uses 'HGV' for what IAM calls 'truck'.
const IAM_TO_BOOKING_VEHICLE: Record<string, string> = {
  car: 'Car',
  van: 'Van',
  motorcycle: 'Motorcycle',
  truck: 'HGV',
};

const defaultVehicleType = user?.vehicle_type
  ? (IAM_TO_BOOKING_VEHICLE[user.vehicle_type.toLowerCase()] ?? '')
  : '';

const [form, setForm] = useState<FormData>({
  origin: '',
  destination: '',
  departureTime: '',
  vehicleType: defaultVehicleType,  // pre-populated from registered vehicle
});
```

- Handles the `truck` → `HGV` naming mismatch between IAM model and booking UI
- Falls back to empty string if `vehicle_type` is unset (safe for admin users)
- Browser verified: Car tile renders with green border/background for a user with `vehicle_type: 'car'`

**Status:** ✅ Fixed.

---

## U-11 — Settings Profile Save Button Was Entirely Fake

**Audit finding:** `SettingsPage.handleSave` only ran a `setTimeout(500ms)` and showed a fake "Saved ✓" tick. `PUT /api/v1/auth/profile` was never called. Changes were discarded on navigation.

**Changes made — full call chain:**

| Layer | File | Change |
|-------|------|--------|
| IAM endpoint | `iam-service/internal/http/router.go:55` | `PUT /api/v1/auth/profile` → `UpdateProfile` (pre-existing, confirmed wired) |
| IAM handler | `iam-service/internal/http/handlers/profile_handler.go:63` | Validates name/vehicle_type/license_info, calls service (pre-existing) |
| IAM service | `iam-service/internal/service/profile_service.go:56` | Calls `UserRepo.UpdateProfile` (pre-existing) |
| IAM repo | `iam-service/internal/repository/user_repo.go:235` | Dynamic `SET` clause, `RETURNING` updated row, writes to master DB (pre-existing) |
| Frontend API | `frontend/src/app/services/iamApi.ts:135` | **New** `iamUpdateProfile(token, params)` → `PUT /api/v1/auth/profile` with Bearer token |
| AppContext | `frontend/src/app/context/AppContext.tsx:397` | **New** `updateProfile(fields)` — calls IAM API, syncs `user` React state + `localStorage` |
| SettingsPage | `frontend/src/app/pages/driver/SettingsPage.tsx:23` | **Rewritten** `handleSave` — calls `updateProfile({ name, vehicle_type })`, shows spinner during save, displays server error inline on failure |
| Email field | `frontend/src/app/pages/driver/SettingsPage.tsx` | Made `readOnly` with greyed styling and "Contact support to change your email address." note — IAM has no email-change endpoint |

**Behaviour after fix:**
- "Save changes" button calls the real IAM API
- Spinner shown while the request is in flight
- "Saved ✓" only appears after a successful server response
- If the API returns an error, it is shown inline with an alert icon
- Name and vehicle type changes persist to the database and survive page refresh
- User state and localStorage are updated immediately on success so the navbar reflects the new name without requiring a re-login

**Status:** ✅ Fixed.

---

## Summary

| Issue | Severity | Description | Status |
|-------|----------|-------------|--------|
| F-11 | P1 | Enforcement role unissuable by IAM | ✅ No IAM change needed — journey middleware already accepts admin role |
| U-01 | P1 | No way to change registered vehicle type | ✅ Vehicle type selector added to Settings; persists via profile API |
| U-02 | P2 | Licence number accepted without validation | ✅ Format + length validation added to Register handler |
| U-03 | P1 | Booking form vehicle type always blank | ✅ Pre-populated from `user.vehicle_type` with truck→HGV mapping |
| U-11 | P0 | Settings save button was a fake setTimeout | ✅ Wired to real `PUT /api/v1/auth/profile`; proper loading/error states |
