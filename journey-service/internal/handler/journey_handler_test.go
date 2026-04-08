package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/service"
)

const testKeyID = "test-key-001"

// testRSASetup generates a fresh RSA key pair, spins up a local JWKS server
// and returns a JWKSValidator that points at it plus the private key for
// signing test tokens.  The JWKS server is shut down via t.Cleanup.
func testRSASetup(t *testing.T) (*rsa.PrivateKey, *middleware.JWKSValidator) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	// Encode N and E as base64url (no padding) per RFC 7518 §6.3
	nBytes := privateKey.N.Bytes()
	eBytes := big.NewInt(int64(privateKey.E)).Bytes()
	nB64 := base64.RawURLEncoding.EncodeToString(nBytes)
	eB64 := base64.RawURLEncoding.EncodeToString(eBytes)

	jwksBody := fmt.Sprintf(
		`{"keys":[{"kty":"RSA","kid":%q,"use":"sig","alg":"RS256","n":%q,"e":%q}]}`,
		testKeyID, nB64, eB64,
	)

	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, jwksBody)
	}))
	t.Cleanup(jwksSrv.Close)

	validator := middleware.NewJWKSValidator(jwksSrv.URL)
	return privateKey, validator
}

// makeTestToken signs a JWT with the supplied RSA private key.
// The kid header matches the key served by the JWKS server from testRSASetup.
func makeTestToken(privateKey *rsa.PrivateKey, role string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":  "usr_testdriver",
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = testKeyID
	str, _ := token.SignedString(privateKey)
	return str
}

func newTestRouter(h *JourneyHandler, validator *middleware.JWKSValidator) *mux.Router {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.JWTAuth(validator))
	api.Use(middleware.IdempotencyKeyMiddleware)
	api.HandleFunc("/journeys", h.CreateJourney).Methods("POST")
	return r
}

func TestCreateJourney_MissingAuthHeader(t *testing.T) {
	_, validator := testRSASetup(t)
	h := NewJourneyHandler(nil)
	router := newTestRouter(h, validator)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCreateJourney_InvalidBody(t *testing.T) {
	privateKey, validator := testRSASetup(t)
	h := NewJourneyHandler(nil)
	router := newTestRouter(h, validator)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(`not-valid-json`))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(privateKey, "driver"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateJourney_DepartureTooSoon(t *testing.T) {
	// Service with nil deps is safe: departure check fires before any DB/Redis call
	// and there is no Idempotency-Key header, so the idempotency path is skipped.
	privateKey, validator := testRSASetup(t)
	svc := service.NewJourneyService(nil, nil, nil, nil, nil, 60, 30, 30, nil)
	h := NewJourneyHandler(svc)
	router := newTestRouter(h, validator)

	departure := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(
		`{"origin":{"lat":53.3,"lng":-6.2},"destination":{"lat":51.9,"lng":-8.4},"departure_time":%q,"vehicle_type":"car"}`,
		departure,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(privateKey, "driver"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}
