package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newAuthHandler returns a handler with a nil service.
// Because all tested paths return before calling the service
// (input validation errors), the nil service never panics.
func newAuthHandler() *AuthHandler { return NewAuthHandler(nil) }

// ---------------------------------------------------------------------------
// Register — input validation
// ---------------------------------------------------------------------------

func TestRegister_InvalidJSON(t *testing.T) {
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegister_ValidationErrors_LowercaseFieldNames(t *testing.T) {
	// Intentionally bad inputs to trigger validation errors for every field.
	body := `{
		"name": "",
		"email": "not-an-email",
		"password": "short",
		"vehicle_type": "spaceship",
		"license_info": {"license_number": ""}
	}`
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	// Decode the response and verify that validation-error detail objects use
	// lowercase JSON keys ("field", "message") — not the old Go-default
	// capitalised form ("Field", "Message").
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level 'error' object, got: %v", resp)
	}
	details, ok := errObj["details"].([]interface{})
	if !ok || len(details) == 0 {
		t.Fatalf("expected non-empty 'details' array, got: %v", errObj)
	}

	for i, raw := range details {
		item, ok := raw.(map[string]interface{})
		if !ok {
			t.Errorf("details[%d] is not an object: %T", i, raw)
			continue
		}
		if _, has := item["field"]; !has {
			t.Errorf("details[%d]: expected lowercase 'field' key, keys present: %v", i, keys(item))
		}
		if _, has := item["message"]; !has {
			t.Errorf("details[%d]: expected lowercase 'message' key, keys present: %v", i, keys(item))
		}
		// The old bug: capitalised keys must NOT appear.
		if _, has := item["Field"]; has {
			t.Errorf("details[%d]: unexpected capitalised 'Field' key", i)
		}
		if _, has := item["Message"]; has {
			t.Errorf("details[%d]: unexpected capitalised 'Message' key", i)
		}
	}
}

func TestRegister_MissingLicenseNumber(t *testing.T) {
	body := `{
		"name": "Alice",
		"email": "alice@example.com",
		"password": "Secure99",
		"vehicle_type": "car",
		"license_info": {"license_number": ""}
	}`
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing license number, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Login — input validation
// ---------------------------------------------------------------------------

func TestLogin_EmptyCredentials(t *testing.T) {
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty credentials, got %d", w.Code)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{bad}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Refresh — input validation
// ---------------------------------------------------------------------------

func TestRefresh_MissingToken(t *testing.T) {
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing refresh_token, got %d", w.Code)
	}
}

func TestRefresh_InvalidJSON(t *testing.T) {
	h := newAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`notjson`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Refresh(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// validPassword — unit tests for the inline helper
// ---------------------------------------------------------------------------

func TestValidPassword(t *testing.T) {
	cases := []struct {
		pw   string
		want bool
		desc string
	}{
		{"short1", false, "too short (< 8 chars)"},
		{"onlyletters", false, "no digit"},
		{"12345678", false, "no letter"},
		{"Secret1", false, "exactly 7 chars"},
		{"Secret12", true, "8 chars with letter + digit"},
		{"aB3dEfGh", true, "mixed case and digit"},
		{"", false, "empty string"},
	}
	for _, tc := range cases {
		if got := validPassword(tc.pw); got != tc.want {
			t.Errorf("validPassword(%q) [%s] = %v, want %v", tc.pw, tc.desc, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func keys(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
