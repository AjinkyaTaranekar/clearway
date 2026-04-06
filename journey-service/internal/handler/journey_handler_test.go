package handler

import (
	"fmt"
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

const testJWTSecret = "test-secret-for-handler-tests"

func makeTestToken(role string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "usr_testdriver",
		"role": role,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	str, _ := token.SignedString([]byte(testJWTSecret))
	return str
}

func newTestRouter(h *JourneyHandler) *mux.Router {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.JWTAuth(testJWTSecret))
	api.Use(middleware.IdempotencyKeyMiddleware)
	api.HandleFunc("/journeys", h.CreateJourney).Methods("POST")
	return r
}

func TestCreateJourney_MissingAuthHeader(t *testing.T) {
	h := NewJourneyHandler(nil)
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCreateJourney_InvalidBody(t *testing.T) {
	h := NewJourneyHandler(nil)
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(`not-valid-json`))
	req.Header.Set("Authorization", "Bearer "+makeTestToken("driver"))
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
	svc := service.NewJourneyService(nil, nil, nil, nil, nil, nil, 60, 30, 30, nil)
	h := NewJourneyHandler(svc)
	router := newTestRouter(h)

	departure := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	body := fmt.Sprintf(
		`{"origin":{"lat":53.3,"lng":-6.2},"destination":{"lat":51.9,"lng":-8.4},"departure_time":%q,"vehicle_type":"car"}`,
		departure,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/journeys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestToken("driver"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}
