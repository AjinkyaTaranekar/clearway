package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type gatewayTarget struct {
	BaseURL string
	Region  string
}

type userSession struct {
	Alias       string
	Name        string
	Email       string
	Password    string
	VehicleType string
	Target      gatewayTarget

	Client       *APIClient
	AccessToken  string
	RefreshToken string
	UserID       string
	Role         string
}

type authUserPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	VehicleType string `json:"vehicle_type"`
}

type authPayload struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	User         authUserPayload `json:"user"`
}

type refreshPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type profilePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	VehicleType string `json:"vehicle_type"`
}

type mapNode struct {
	NodeID string  `json:"node_id"`
	Label  string  `json:"label"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

type mapNodesPayload struct {
	Nodes []mapNode `json:"nodes"`
}

type mapRouteSegment struct {
	Sequence             int    `json:"sequence"`
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	FromNodeID           string `json:"from_node_id"`
	ToNodeID             string `json:"to_node_id"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
	Region               string `json:"region"`
}

type mapRoutePayload struct {
	Origin                    mapNode           `json:"origin"`
	Destination               mapNode           `json:"destination"`
	TotalTraversalTimeMinutes int               `json:"total_traversal_time_minutes"`
	Segments                  []mapRouteSegment `json:"segments"`
}

type capacityCheckPayload struct {
	SegmentID      string  `json:"segment_id"`
	MaxCapacity    float64 `json:"max_capacity"`
	ReservedSlots  float64 `json:"reserved_slots"`
	AvailableSlots float64 `json:"available_slots"`
	CanReserve     bool    `json:"can_reserve"`
	IsClosed       bool    `json:"is_closed"`
	ClosureReason  string  `json:"closure_reason"`
}

type journeySegmentPayload struct {
	SegmentID        string    `json:"segment_id"`
	SegmentName      string    `json:"segment_name"`
	SequenceOrder    int       `json:"sequence_order"`
	TimeWindowStart  time.Time `json:"time_window_start"`
	TimeWindowEnd    time.Time `json:"time_window_end"`
	TraversalMinutes int       `json:"traversal_minutes"`
	Region           string    `json:"region"`
}

type journeyPayload struct {
	JourneyID        string                  `json:"journey_id"`
	DriverID         string                  `json:"driver_id"`
	DepartureTime    time.Time               `json:"departure_time"`
	EstimatedArrival time.Time               `json:"estimated_arrival"`
	VehicleType      string                  `json:"vehicle_type"`
	Status           string                  `json:"status"`
	RejectionReason  string                  `json:"rejection_reason"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Segments         []journeySegmentPayload `json:"segments"`
}

type journeyListPayload struct {
	Journeys []journeyPayload `json:"journeys"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	Limit    int              `json:"limit"`
}

type notificationItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Read    bool   `json:"read"`
}

type notificationListPayload struct {
	Notifications []notificationItem `json:"notifications"`
	UnreadCount   int                `json:"unread_count"`
	Total         int                `json:"total"`
	Page          int                `json:"page"`
	Limit         int                `json:"limit"`
}

type regionPayload struct {
	Region string `json:"region"`
}

type routePlan struct {
	Label         string
	OriginID      string
	DestinationID string
}

type segmentWindow struct {
	SegmentID string
	Start     time.Time
	End       time.Time
}

type bookingOutcome struct {
	Alias           string   `json:"alias"`
	BaseURL         string   `json:"base_url"`
	GatewayRegion   string   `json:"gateway_region"`
	Plan            string   `json:"plan"`
	JourneyID       string   `json:"journey_id,omitempty"`
	Status          string   `json:"status,omitempty"`
	HTTPStatus      int      `json:"http_status"`
	RouteRegions    []string `json:"route_regions,omitempty"`
	RejectionReason string   `json:"rejection_reason,omitempty"`
	Error           string   `json:"error,omitempty"`
}

func TestProductionGradeE2ESystemFlow(t *testing.T) {
	cfg := loadConfig()
	logger := newLogger()
	reporter := newReporter(cfg, logger)
	ctx := context.Background()

	targets, err := preflightTargets(ctx, cfg, reporter, logger)
	if err != nil {
		reportPath, writeErr := reporter.writeJSON()
		if writeErr != nil {
			t.Fatalf("preflight failed (%v) and report write failed (%v)", err, writeErr)
		}
		if cfg.SkipIfUnhealthy {
			t.Skipf("preflight skipped execution: %v (report: %s)", err, reportPath)
		}
		t.Fatalf("preflight failed: %v (report: %s)", err, reportPath)
	}

	scenarioErrors := make([]string, 0, 4)
	runScenario := func(name string, fn func(*ScenarioRecorder) error) {
		sr := reporter.beginScenario(name)
		err := fn(sr)
		sr.complete()
		if err != nil {
			scenarioErrors = append(scenarioErrors, fmt.Sprintf("%s: %v", name, err))
			logger.Error("scenario_failed", slog.String("scenario", name), slog.String("error", err.Error()))
		}
	}

	primary := targets[0]
	runScenario("01-auth-lifecycle", func(sr *ScenarioRecorder) error {
		return runAuthLifecycleScenario(ctx, cfg, logger, sr, primary)
	})
	runScenario("02-happy-path-booking-and-final-state", func(sr *ScenarioRecorder) error {
		return runHappyPathScenario(ctx, cfg, logger, sr, primary)
	})
	runScenario("03-sad-path-validation-and-state-guardrails", func(sr *ScenarioRecorder) error {
		return runSadPathScenario(ctx, cfg, logger, sr, primary)
	})
	runScenario("04-concurrency-simulation-multi-user-regional", func(sr *ScenarioRecorder) error {
		return runConcurrencyScenario(ctx, cfg, logger, sr, targets)
	})

	reportPath, writeErr := reporter.writeJSON()
	if writeErr != nil {
		t.Fatalf("failed to write report: %v", writeErr)
	}
	t.Logf("Centralized E2E report written to %s", reportPath)

	if len(scenarioErrors) > 0 {
		t.Fatalf("%d scenarios failed: %s", len(scenarioErrors), strings.Join(scenarioErrors, " | "))
	}
}

func preflightTargets(ctx context.Context, cfg Config, reporter *Reporter, logger *slog.Logger) ([]gatewayTarget, error) {
	sr := reporter.beginScenario("00-preflight")
	defer sr.complete()

	healthy := make([]gatewayTarget, 0, len(cfg.BaseURLs))

	for _, baseURL := range cfg.BaseURLs {
		target := gatewayTarget{BaseURL: baseURL, Region: "unknown"}
		client := newAPIClient(baseURL, cfg.RequestTimeout, logger, "00-preflight", "preflight")

		if err := runStep(sr, fmt.Sprintf("health-check %s", baseURL), func(step *StepContext) error {
			res, err := client.do(ctx, RequestSpec{
				Name:    "nginx-health",
				Method:  http.MethodGet,
				Path:    "/nginx-health",
				UseAuth: false,
			})
			if err != nil {
				return fmt.Errorf("health probe transport failure: %w", err)
			}
			step.AttachHTTP(res)
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("expected 200 from /nginx-health, got %d", res.StatusCode)
			}
			step.SetDetails("gateway is reachable")
			return nil
		}); err != nil {
			continue
		}

		_ = runStep(sr, fmt.Sprintf("region-detection %s", baseURL), func(step *StepContext) error {
			res, err := client.do(ctx, RequestSpec{
				Name:    "region",
				Method:  http.MethodGet,
				Path:    "/api/v1/region",
				UseAuth: false,
			})
			if err != nil {
				step.SetDetails("region endpoint unreachable; defaulting to unknown")
				return nil
			}
			step.AttachHTTP(res)
			if res.StatusCode != http.StatusOK {
				step.SetDetails(fmt.Sprintf("region endpoint returned %d; defaulting to unknown", res.StatusCode))
				return nil
			}

			var payload regionPayload
			if err := res.decodeJSON(&payload); err != nil {
				step.SetDetails("region JSON parse failed; defaulting to unknown")
				return nil
			}
			region := strings.TrimSpace(payload.Region)
			if region == "" {
				region = "unknown"
			}
			target.Region = region
			step.SetDetails("detected region: " + region)
			return nil
		})

		reporter.setRegion(target.BaseURL, target.Region)
		healthy = append(healthy, target)
	}

	sr.setMetadata("healthy_gateways", healthy)
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy gateway found in E2E_BASE_URLS")
	}
	return healthy, nil
}

func runAuthLifecycleScenario(ctx context.Context, cfg Config, logger *slog.Logger, sr *ScenarioRecorder, target gatewayTarget) error {
	session, err := ensureSession(ctx, cfg, logger, sr, target, "auth-primary", "car")
	if err != nil {
		return err
	}

	if err := runStep(sr, "refresh-access-token", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:   "refresh-token",
			Method: http.MethodPost,
			Path:   "/api/v1/auth/refresh",
			Body: map[string]interface{}{
				"refresh_token": session.RefreshToken,
			},
			UseAuth: false,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 on refresh, got %d (%s)", res.StatusCode, res.errorMessage())
		}

		var payload refreshPayload
		if err := res.decodeEnvelopeData(&payload); err != nil {
			return err
		}
		if !isJWTLike(payload.AccessToken) {
			return fmt.Errorf("refreshed access token is not JWT-like")
		}
		if strings.TrimSpace(payload.RefreshToken) == "" {
			return fmt.Errorf("refresh token missing in refresh response")
		}

		session.AccessToken = payload.AccessToken
		session.RefreshToken = payload.RefreshToken
		session.Client.setToken(payload.AccessToken)
		step.SetDetails("access and refresh tokens rotated successfully")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "profile-read-after-refresh", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "profile-after-refresh",
			Method:  http.MethodGet,
			Path:    "/api/v1/auth/profile",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 on profile fetch, got %d (%s)", res.StatusCode, res.errorMessage())
		}

		var profile profilePayload
		if err := res.decodeEnvelopeData(&profile); err != nil {
			return err
		}
		if profile.Email != session.Email {
			return fmt.Errorf("profile email mismatch: got %s want %s", profile.Email, session.Email)
		}
		step.SetDetails("profile fetched with refreshed bearer token")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "cross-service-token-propagation-smoke", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "map-nodes-with-auth",
			Method:  http.MethodGet,
			Path:    "/api/v1/map/nodes",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 for map nodes, got %d", res.StatusCode)
		}

		var nodes mapNodesPayload
		if err := res.decodeEnvelopeData(&nodes); err != nil {
			return err
		}
		if len(nodes.Nodes) < 2 {
			return fmt.Errorf("expected at least 2 nodes, got %d", len(nodes.Nodes))
		}
		step.SetDetails(fmt.Sprintf("bearer token propagated; map returned %d nodes", len(nodes.Nodes)))
		return nil
	}); err != nil {
		return err
	}

	sr.setMetadata("user", map[string]interface{}{
		"alias":          session.Alias,
		"email":          session.Email,
		"user_id":        session.UserID,
		"gateway_region": session.Target.Region,
	})
	return nil
}

func runHappyPathScenario(ctx context.Context, cfg Config, logger *slog.Logger, sr *ScenarioRecorder, target gatewayTarget) error {
	session, err := ensureSession(ctx, cfg, logger, sr, target, "happy-driver", "car")
	if err != nil {
		return err
	}

	if err := cleanupOpenJourneys(ctx, sr, session); err != nil {
		return err
	}

	var nodes mapNodesPayload
	if err := runStep(sr, "load-map-nodes", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "map-nodes",
			Method:  http.MethodGet,
			Path:    "/api/v1/map/nodes",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 for map nodes, got %d", res.StatusCode)
		}
		if err := res.decodeEnvelopeData(&nodes); err != nil {
			return err
		}
		if len(nodes.Nodes) < 4 {
			return fmt.Errorf("expected map to provide at least 4 nodes, got %d", len(nodes.Nodes))
		}
		step.SetDetails(fmt.Sprintf("loaded %d selectable map nodes", len(nodes.Nodes)))
		return nil
	}); err != nil {
		return err
	}

	origin, ok := findNodeByID(nodes.Nodes, "city")
	if !ok {
		return fmt.Errorf("required node city not found")
	}
	destination, ok := findNodeByID(nodes.Nodes, "airport")
	if !ok {
		return fmt.Errorf("required node airport not found")
	}

	var route mapRoutePayload
	if err := runStep(sr, "resolve-route-city-to-airport", func(step *StepContext) error {
		query := url.Values{}
		query.Set("origin_node_id", origin.NodeID)
		query.Set("destination_node_id", destination.NodeID)

		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "map-route",
			Method:  http.MethodGet,
			Path:    "/api/v1/map/route",
			Query:   query,
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 for map route, got %d (%s)", res.StatusCode, res.errorMessage())
		}

		if err := res.decodeEnvelopeData(&route); err != nil {
			return err
		}
		if len(route.Segments) == 0 {
			return fmt.Errorf("route returned zero segments")
		}
		step.SetDetails(fmt.Sprintf("route has %d segments and %d total minutes", len(route.Segments), route.TotalTraversalTimeMinutes))
		return nil
	}); err != nil {
		return err
	}

	departure := nextHalfHourSlot(2 * time.Hour)
	windows := buildSegmentWindows(departure, route.Segments)
	if len(windows) == 0 {
		return fmt.Errorf("failed to build segment windows for capacity checks")
	}

	if err := runStep(sr, "pre-flight-capacity-checks-for-route", func(step *StepContext) error {
		minAvailable := 1e9
		for idx, window := range windows {
			query := url.Values{}
			query.Set("segment_id", window.SegmentID)
			query.Set("time_window_start", window.Start.UTC().Format(time.RFC3339))
			query.Set("time_window_end", window.End.UTC().Format(time.RFC3339))

			res, err := session.Client.do(ctx, RequestSpec{
				Name:    fmt.Sprintf("capacity-check-%d", idx+1),
				Method:  http.MethodGet,
				Path:    "/api/v1/capacity/check",
				Query:   query,
				UseAuth: true,
			})
			if err != nil {
				return err
			}
			step.AttachHTTP(res)
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("capacity check failed with %d for segment %s", res.StatusCode, window.SegmentID)
			}

			var check capacityCheckPayload
			if err := res.decodeJSON(&check); err != nil {
				return err
			}
			if check.AvailableSlots < minAvailable {
				minAvailable = check.AvailableSlots
			}
		}
		step.SetDetails(fmt.Sprintf("validated %d segment windows, minimum available slots %.2f", len(windows), minAvailable))
		return nil
	}); err != nil {
		return err
	}

	var approvedJourney journeyPayload
	if err := runStep(sr, "book-journey-happy-path-with-retry", func(step *StepContext) error {
		candidateSlots := []time.Duration{2 * time.Hour, 150 * time.Minute, 3 * time.Hour}
		var rejectionReasons []string

		for _, slotAhead := range candidateSlots {
			dep := nextHalfHourSlot(slotAhead)
			journey, res, err := createJourney(ctx, session, origin, destination, dep, "car", "normal")
			if err != nil {
				return err
			}
			step.AttachHTTP(res)
			if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected booking status %d: %s", res.StatusCode, res.errorMessage())
			}

			statusUpper := strings.ToUpper(strings.TrimSpace(journey.Status))
			if statusUpper == "APPROVED" {
				approvedJourney = journey
				step.SetDetails(fmt.Sprintf("approved journey %s at departure %s", journey.JourneyID, dep.UTC().Format(time.RFC3339)))
				return nil
			}
			if statusUpper == "REJECTED" {
				rejectionReasons = append(rejectionReasons, journey.RejectionReason)
				continue
			}
			return fmt.Errorf("unexpected journey status %s", journey.Status)
		}

		return fmt.Errorf("failed to obtain APPROVED journey after %d attempts, rejection reasons: %v", len(candidateSlots), rejectionReasons)
	}); err != nil {
		return err
	}

	if approvedJourney.JourneyID == "" {
		return fmt.Errorf("approved journey id missing")
	}

	if err := runStep(sr, "list-journeys-contains-approved-booking", func(step *StepContext) error {
		res, list, err := listJourneys(ctx, session, "")
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if !journeyInList(list.Journeys, approvedJourney.JourneyID) {
			return fmt.Errorf("journey %s not found in list response", approvedJourney.JourneyID)
		}
		step.SetDetails(fmt.Sprintf("journey list contains %d entries including %s", len(list.Journeys), approvedJourney.JourneyID))
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "notification-read-model-smoke", func(step *StepContext) error {
		query := url.Values{}
		query.Set("page", "1")
		query.Set("limit", "20")

		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "notifications-list",
			Method:  http.MethodGet,
			Path:    "/api/v1/notifications",
			Query:   query,
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 from notifications endpoint, got %d", res.StatusCode)
		}

		var payload notificationListPayload
		if err := res.decodeEnvelopeData(&payload); err != nil {
			return err
		}
		step.SetDetails(fmt.Sprintf("notification API healthy; total=%d unread=%d", payload.Total, payload.UnreadCount))
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "cancel-approved-journey", func(step *StepContext) error {
		res, journey, err := cancelJourney(ctx, session, approvedJourney.JourneyID)
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if strings.ToUpper(journey.Status) != "CANCELLED" {
			return fmt.Errorf("expected journey status CANCELLED after cancel, got %s", journey.Status)
		}
		step.SetDetails(fmt.Sprintf("journey %s cancelled", journey.JourneyID))
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "final-state-confirmation-cancelled", func(step *StepContext) error {
		res, journey, err := getJourney(ctx, session, approvedJourney.JourneyID)
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if strings.ToUpper(journey.Status) != "CANCELLED" {
			return fmt.Errorf("expected final state CANCELLED, got %s", journey.Status)
		}
		step.SetDetails("end-to-end flow confirmed from authentication through final journey state")
		return nil
	}); err != nil {
		return err
	}

	sr.setMetadata("happy_path", map[string]interface{}{
		"journey_id":     approvedJourney.JourneyID,
		"gateway_region": session.Target.Region,
		"route_regions":  uniqueSortedRouteRegions(route.Segments),
	})
	return nil
}

func runSadPathScenario(ctx context.Context, cfg Config, logger *slog.Logger, sr *ScenarioRecorder, target gatewayTarget) error {
	session, err := ensureSession(ctx, cfg, logger, sr, target, "sad-driver", "van")
	if err != nil {
		return err
	}

	if err := cleanupOpenJourneys(ctx, sr, session); err != nil {
		return err
	}

	if err := runStep(sr, "sad-invalid-login-password", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:   "invalid-login",
			Method: http.MethodPost,
			Path:   "/api/v1/auth/login",
			Body: map[string]interface{}{
				"email":    session.Email,
				"password": "WrongPassword123",
			},
			UseAuth: false,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected 401 for invalid login, got %d", res.StatusCode)
		}
		step.SetDetails("invalid login correctly rejected")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-missing-token-unauthorized", func(step *StepContext) error {
		anonymousClient := newAPIClient(target.BaseURL, cfg.RequestTimeout, logger, sr.record.Name, "anonymous")
		res, err := anonymousClient.do(ctx, RequestSpec{
			Name:    "journeys-without-auth",
			Method:  http.MethodGet,
			Path:    "/api/v1/journeys",
			UseAuth: false,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected 401 without token, got %d", res.StatusCode)
		}
		step.SetDetails("unauthenticated access blocked")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-invalid-capacity-window", func(step *StepContext) error {
		now := time.Now().UTC()
		query := url.Values{}
		query.Set("segment_id", "seg_city_north")
		query.Set("time_window_start", now.Format(time.RFC3339))
		query.Set("time_window_end", now.Add(-10*time.Minute).Format(time.RFC3339))

		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "capacity-invalid-window",
			Method:  http.MethodGet,
			Path:    "/api/v1/capacity/check",
			Query:   query,
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("expected 400 for invalid window, got %d", res.StatusCode)
		}
		step.SetDetails("capacity window validation triggered correctly")
		return nil
	}); err != nil {
		return err
	}

	origin := mapNode{NodeID: "city", Lat: 53.3498, Lng: -6.2603}
	destination := mapNode{NodeID: "airport", Lat: 53.4264, Lng: -6.2499}

	if err := runStep(sr, "sad-malformed-create-journey", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:   "journey-malformed",
			Method: http.MethodPost,
			Path:   "/api/v1/journeys",
			Body: map[string]interface{}{
				"vehicle_type": "car",
			},
			Headers: map[string]string{
				"Idempotency-Key": newID("idem"),
			},
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("expected 400 for malformed journey request, got %d", res.StatusCode)
		}
		step.SetDetails("journey input validation guards required fields")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-departure-too-soon", func(step *StepContext) error {
		tooSoon := time.Now().UTC().Add(30 * time.Minute)
		_, res, err := createJourney(ctx, session, origin, destination, tooSoon, "van", "normal")
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusUnprocessableEntity {
			return fmt.Errorf("expected 422 for too-soon departure, got %d", res.StatusCode)
		}
		step.SetDetails("1-hour advance booking rule enforced")
		return nil
	}); err != nil {
		return err
	}

	var fixture journeyPayload
	if err := runStep(sr, "sad-setup-approved-fixture", func(step *StepContext) error {
		candidateSlots := []time.Duration{2 * time.Hour, 150 * time.Minute, 3 * time.Hour}
		for _, slotAhead := range candidateSlots {
			journey, res, err := createJourney(ctx, session, origin, destination, nextHalfHourSlot(slotAhead), "van", "normal")
			if err != nil {
				return err
			}
			step.AttachHTTP(res)
			if strings.ToUpper(journey.Status) == "APPROVED" {
				fixture = journey
				step.SetDetails("created approved fixture journey " + fixture.JourneyID)
				return nil
			}
		}
		return fmt.Errorf("could not prepare approved fixture journey for sad-path transition checks")
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-activate-too-early", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "activate-too-early",
			Method:  http.MethodPut,
			Path:    "/api/v1/journeys/" + fixture.JourneyID + "/activate",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusForbidden {
			return fmt.Errorf("expected 403 for early activation, got %d", res.StatusCode)
		}
		step.SetDetails("activation blocked before departure window")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-complete-before-active", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "complete-before-active",
			Method:  http.MethodPut,
			Path:    "/api/v1/journeys/" + fixture.JourneyID + "/complete",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("expected 400 for complete-before-active, got %d", res.StatusCode)
		}
		step.SetDetails("status transition guard rejected invalid complete action")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-duplicate-active-approved-conflict", func(step *StepContext) error {
		_, res, err := createJourney(ctx, session, origin, destination, nextHalfHourSlot(4*time.Hour), "van", "normal")
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusConflict {
			return fmt.Errorf("expected 409 when booking while approved journey exists, got %d", res.StatusCode)
		}
		step.SetDetails("single-active-or-approved journey rule enforced")
		return nil
	}); err != nil {
		return err
	}

	if err := runStep(sr, "sad-cleanup-cancel-fixture", func(step *StepContext) error {
		res, journey, err := cancelJourney(ctx, session, fixture.JourneyID)
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if strings.ToUpper(journey.Status) != "CANCELLED" {
			return fmt.Errorf("expected fixture to cancel cleanly, got %s", journey.Status)
		}
		return nil
	}); err != nil {
		return err
	}

	sr.setMetadata("sad_path_fixture_journey", fixture.JourneyID)
	return nil
}

func runConcurrencyScenario(ctx context.Context, cfg Config, logger *slog.Logger, sr *ScenarioRecorder, targets []gatewayTarget) error {
	plans := []routePlan{
		{Label: "north-corridor", OriginID: "city", DestinationID: "airport"},
		{Label: "west-port-corridor", OriginID: "city", DestinationID: "port"},
		{Label: "cross-city-east-west", OriginID: "east", DestinationID: "west"},
	}

	sessions := make([]*userSession, 0, 3)
	for i := 0; i < 3; i++ {
		target := targets[i%len(targets)]
		alias := fmt.Sprintf("conc-user-%d", i+1)
		vehicleType := []string{"car", "van", "truck"}[i]
		session, err := ensureSession(ctx, cfg, logger, sr, target, alias, vehicleType)
		if err != nil {
			return err
		}
		if err := cleanupOpenJourneys(ctx, sr, session); err != nil {
			return err
		}
		sessions = append(sessions, session)
	}

	departDifferentRegions := nextHalfHourSlot(150 * time.Minute)
	var outcomesDifferent []bookingOutcome
	if err := runStep(sr, "concurrency-phase-1-different-regions", func(step *StepContext) error {
		outcomesDifferent = runConcurrentBookings(ctx, sessions, plans, departDifferentRegions)
		if len(outcomesDifferent) != 3 {
			return fmt.Errorf("expected 3 booking outcomes, got %d", len(outcomesDifferent))
		}
		for _, outcome := range outcomesDifferent {
			if outcome.Error != "" {
				return fmt.Errorf("%s failed: %s", outcome.Alias, outcome.Error)
			}
			if outcome.HTTPStatus != http.StatusCreated && outcome.HTTPStatus != http.StatusOK {
				return fmt.Errorf("%s unexpected http status %d", outcome.Alias, outcome.HTTPStatus)
			}
			status := strings.ToUpper(outcome.Status)
			if status != "APPROVED" && status != "REJECTED" {
				return fmt.Errorf("%s returned unexpected journey status %s", outcome.Alias, outcome.Status)
			}
		}

		regionSet := make(map[string]struct{})
		for _, outcome := range outcomesDifferent {
			for _, region := range outcome.RouteRegions {
				regionSet[region] = struct{}{}
			}
		}
		if len(regionSet) < 2 {
			return fmt.Errorf("expected bookings to span at least 2 route regions, got %d", len(regionSet))
		}
		if hasDuplicateJourneyID(outcomesDifferent) {
			return fmt.Errorf("duplicate journey_id detected across concurrent responses")
		}

		step.SetDetails(fmt.Sprintf("phase-1 complete: %d outcomes across %d unique route regions", len(outcomesDifferent), len(regionSet)))
		return nil
	}); err != nil {
		return err
	}

	_ = runStep(sr, "concurrency-phase-1-cleanup", func(step *StepContext) error {
		cleaned := 0
		for _, outcome := range outcomesDifferent {
			if strings.ToUpper(outcome.Status) != "APPROVED" || outcome.JourneyID == "" {
				continue
			}
			session := sessionByAlias(sessions, outcome.Alias)
			if session == nil {
				continue
			}
			if _, _, err := cancelJourney(ctx, session, outcome.JourneyID); err == nil {
				cleaned++
			}
		}
		step.SetDetails(fmt.Sprintf("cancelled %d approved journeys from phase-1", cleaned))
		return nil
	})

	departContention := nextHalfHourSlot(180 * time.Minute)
	sharedPlan := routePlan{Label: "shared-north-race", OriginID: "city", DestinationID: "airport"}
	sharedPlans := []routePlan{sharedPlan, sharedPlan, sharedPlan}
	var outcomesRace []bookingOutcome
	if err := runStep(sr, "concurrency-phase-2-shared-route-race", func(step *StepContext) error {
		outcomesRace = runConcurrentBookings(ctx, sessions, sharedPlans, departContention)
		if len(outcomesRace) != 3 {
			return fmt.Errorf("expected 3 race outcomes, got %d", len(outcomesRace))
		}

		approvedCount := 0
		rejectedCount := 0
		for _, outcome := range outcomesRace {
			if outcome.Error != "" {
				return fmt.Errorf("race booking error for %s: %s", outcome.Alias, outcome.Error)
			}
			status := strings.ToUpper(outcome.Status)
			switch status {
			case "APPROVED":
				approvedCount++
			case "REJECTED":
				rejectedCount++
			default:
				return fmt.Errorf("unexpected race status %s for %s", outcome.Status, outcome.Alias)
			}
		}

		step.SetDetails(fmt.Sprintf("race outcomes: approved=%d rejected=%d", approvedCount, rejectedCount))
		return nil
	}); err != nil {
		return err
	}

	_ = runStep(sr, "concurrency-phase-2-cleanup", func(step *StepContext) error {
		cleaned := 0
		for _, outcome := range outcomesRace {
			if strings.ToUpper(outcome.Status) != "APPROVED" || outcome.JourneyID == "" {
				continue
			}
			session := sessionByAlias(sessions, outcome.Alias)
			if session == nil {
				continue
			}
			if _, _, err := cancelJourney(ctx, session, outcome.JourneyID); err == nil {
				cleaned++
			}
		}
		step.SetDetails(fmt.Sprintf("cancelled %d approved journeys from race phase", cleaned))
		return nil
	})

	sr.setMetadata("phase1_outcomes", outcomesDifferent)
	sr.setMetadata("phase2_outcomes", outcomesRace)
	return nil
}

func ensureSession(ctx context.Context, cfg Config, logger *slog.Logger, sr *ScenarioRecorder, target gatewayTarget, alias, vehicleType string) (*userSession, error) {
	session := &userSession{
		Alias:       alias,
		Name:        "E2E " + alias,
		Email:       cfg.emailFor(alias),
		Password:    cfg.Password,
		VehicleType: vehicleType,
		Target:      target,
		Client:      newAPIClient(target.BaseURL, cfg.RequestTimeout, logger, sr.record.Name, alias),
	}

	registered := false
	if err := runStep(sr, alias+" register-attempt", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:   "register",
			Method: http.MethodPost,
			Path:   "/api/v1/auth/register",
			Body: map[string]interface{}{
				"name":         session.Name,
				"email":        session.Email,
				"password":     session.Password,
				"vehicle_type": session.VehicleType,
				"license_info": map[string]interface{}{
					"license_number": strings.ToUpper(newID("dl")),
				},
			},
			UseAuth: false,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		switch res.StatusCode {
		case http.StatusCreated:
			payload, err := parseAuthResponse(res)
			if err != nil {
				return err
			}
			applyAuthPayload(session, payload)
			registered = true
			step.SetDetails("registered new dummy user")
			return nil
		case http.StatusConflict:
			step.SetDetails("user already exists; will fallback to login")
			return nil
		default:
			msg := strings.ToLower(res.errorMessage())
			if strings.Contains(msg, "already") || strings.Contains(msg, "exists") {
				step.SetDetails("register reported existing account; fallback to login")
				return nil
			}
			return fmt.Errorf("unexpected register status %d: %s", res.StatusCode, res.errorMessage())
		}
	}); err != nil {
		return nil, err
	}

	if !registered {
		if err := runStep(sr, alias+" login-fallback", func(step *StepContext) error {
			res, err := session.Client.do(ctx, RequestSpec{
				Name:   "login",
				Method: http.MethodPost,
				Path:   "/api/v1/auth/login",
				Body: map[string]interface{}{
					"email":    session.Email,
					"password": session.Password,
				},
				UseAuth: false,
			})
			if err != nil {
				return err
			}
			step.AttachHTTP(res)
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("login fallback expected 200, got %d (%s)", res.StatusCode, res.errorMessage())
			}

			payload, err := parseAuthResponse(res)
			if err != nil {
				return err
			}
			applyAuthPayload(session, payload)
			step.SetDetails("logged in existing user")
			return nil
		}); err != nil {
			return nil, err
		}
	}

	session.Client.setToken(session.AccessToken)
	if err := runStep(sr, alias+" token-profile-verification", func(step *StepContext) error {
		res, err := session.Client.do(ctx, RequestSpec{
			Name:    "profile",
			Method:  http.MethodGet,
			Path:    "/api/v1/auth/profile",
			UseAuth: true,
		})
		if err != nil {
			return err
		}
		step.AttachHTTP(res)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("profile expected 200, got %d (%s)", res.StatusCode, res.errorMessage())
		}

		var profile profilePayload
		if err := res.decodeEnvelopeData(&profile); err != nil {
			return err
		}
		if profile.Email != session.Email {
			return fmt.Errorf("profile mismatch: got %s want %s", profile.Email, session.Email)
		}
		if strings.TrimSpace(session.AccessToken) == "" || !isJWTLike(session.AccessToken) {
			return fmt.Errorf("missing or malformed access token after auth lifecycle")
		}
		step.SetDetails("bearer token is active and profile assertion passed")
		return nil
	}); err != nil {
		return nil, err
	}

	return session, nil
}

func parseAuthResponse(res *HTTPResult) (*authPayload, error) {
	var payload authPayload
	if err := res.decodeEnvelopeData(&payload); err != nil {
		return nil, err
	}
	if !isJWTLike(payload.AccessToken) {
		return nil, fmt.Errorf("access token is not JWT-like")
	}
	if strings.TrimSpace(payload.RefreshToken) == "" {
		return nil, fmt.Errorf("refresh token missing")
	}
	if strings.TrimSpace(payload.User.ID) == "" {
		return nil, fmt.Errorf("user id missing in auth payload")
	}
	return &payload, nil
}

func applyAuthPayload(session *userSession, payload *authPayload) {
	session.AccessToken = payload.AccessToken
	session.RefreshToken = payload.RefreshToken
	session.UserID = payload.User.ID
	session.Role = payload.User.Role
}

func createJourney(ctx context.Context, session *userSession, origin, destination mapNode, departure time.Time, vehicleType, priority string) (journeyPayload, *HTTPResult, error) {
	res, err := session.Client.do(ctx, RequestSpec{
		Name:   "create-journey",
		Method: http.MethodPost,
		Path:   "/api/v1/journeys",
		Body: map[string]interface{}{
			"origin": map[string]interface{}{
				"lat": origin.Lat,
				"lng": origin.Lng,
			},
			"destination": map[string]interface{}{
				"lat": destination.Lat,
				"lng": destination.Lng,
			},
			"departure_time": departure.UTC().Format(time.RFC3339),
			"vehicle_type":   vehicleType,
			"priority_level": priority,
		},
		Headers: map[string]string{
			"Idempotency-Key": newID("idem"),
		},
		UseAuth: true,
	})
	if err != nil {
		return journeyPayload{}, nil, err
	}

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusUnprocessableEntity && res.StatusCode != http.StatusConflict {
		return journeyPayload{}, res, fmt.Errorf("unexpected create journey status %d (%s)", res.StatusCode, res.errorMessage())
	}

	if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusOK {
		var journey journeyPayload
		if err := res.decodeEnvelopeData(&journey); err != nil {
			return journeyPayload{}, res, err
		}
		return journey, res, nil
	}

	return journeyPayload{}, res, nil
}

func listJourneys(ctx context.Context, session *userSession, status string) (*HTTPResult, journeyListPayload, error) {
	query := url.Values{}
	query.Set("page", "1")
	query.Set("limit", "50")
	if strings.TrimSpace(status) != "" {
		query.Set("status", status)
	}

	res, err := session.Client.do(ctx, RequestSpec{
		Name:    "list-journeys",
		Method:  http.MethodGet,
		Path:    "/api/v1/journeys",
		Query:   query,
		UseAuth: true,
	})
	if err != nil {
		return nil, journeyListPayload{}, err
	}
	if res.StatusCode != http.StatusOK {
		return res, journeyListPayload{}, fmt.Errorf("list journeys expected 200, got %d (%s)", res.StatusCode, res.errorMessage())
	}

	var payload journeyListPayload
	if err := res.decodeEnvelopeData(&payload); err != nil {
		return res, journeyListPayload{}, err
	}
	return res, payload, nil
}

func getJourney(ctx context.Context, session *userSession, journeyID string) (*HTTPResult, journeyPayload, error) {
	res, err := session.Client.do(ctx, RequestSpec{
		Name:    "get-journey",
		Method:  http.MethodGet,
		Path:    "/api/v1/journeys/" + journeyID,
		UseAuth: true,
	})
	if err != nil {
		return nil, journeyPayload{}, err
	}
	if res.StatusCode != http.StatusOK {
		return res, journeyPayload{}, fmt.Errorf("get journey expected 200, got %d (%s)", res.StatusCode, res.errorMessage())
	}

	var payload journeyPayload
	if err := res.decodeEnvelopeData(&payload); err != nil {
		return res, journeyPayload{}, err
	}
	return res, payload, nil
}

func cancelJourney(ctx context.Context, session *userSession, journeyID string) (*HTTPResult, journeyPayload, error) {
	res, err := session.Client.do(ctx, RequestSpec{
		Name:    "cancel-journey",
		Method:  http.MethodPut,
		Path:    "/api/v1/journeys/" + journeyID + "/cancel",
		UseAuth: true,
	})
	if err != nil {
		return nil, journeyPayload{}, err
	}
	if res.StatusCode != http.StatusOK {
		return res, journeyPayload{}, fmt.Errorf("cancel journey expected 200, got %d (%s)", res.StatusCode, res.errorMessage())
	}

	var payload journeyPayload
	if err := res.decodeEnvelopeData(&payload); err != nil {
		return res, journeyPayload{}, err
	}
	return res, payload, nil
}

func cleanupOpenJourneys(ctx context.Context, sr *ScenarioRecorder, session *userSession) error {
	return runStep(sr, session.Alias+" cleanup-open-journeys", func(step *StepContext) error {
		cleanedCancelled := 0
		cleanedCompleted := 0

		if res, list, err := listJourneys(ctx, session, "APPROVED"); err == nil {
			step.AttachHTTP(res)
			for _, journey := range list.Journeys {
				if _, _, err := cancelJourney(ctx, session, journey.JourneyID); err == nil {
					cleanedCancelled++
				}
			}
		}

		if res, list, err := listJourneys(ctx, session, "ACTIVE"); err == nil {
			step.AttachHTTP(res)
			for _, journey := range list.Journeys {
				completeRes, err := session.Client.do(ctx, RequestSpec{
					Name:    "complete-active-cleanup",
					Method:  http.MethodPut,
					Path:    "/api/v1/journeys/" + journey.JourneyID + "/complete",
					UseAuth: true,
				})
				if err == nil && completeRes.StatusCode == http.StatusOK {
					cleanedCompleted++
				}
			}
		}

		step.SetDetails(fmt.Sprintf("cleanup completed: cancelled=%d completed=%d", cleanedCancelled, cleanedCompleted))
		return nil
	})
}

func runConcurrentBookings(ctx context.Context, sessions []*userSession, plans []routePlan, departure time.Time) []bookingOutcome {
	outcomes := make([]bookingOutcome, len(sessions))
	var wg sync.WaitGroup

	for i := range sessions {
		i := i
		session := sessions[i]
		plan := plans[i]

		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome := bookingOutcome{
				Alias:         session.Alias,
				BaseURL:       session.Target.BaseURL,
				GatewayRegion: session.Target.Region,
				Plan:          plan.Label,
			}

			routeRes, route, err := fetchRouteByPlan(ctx, session, plan)
			if err != nil {
				outcome.Error = err.Error()
				if routeRes != nil {
					outcome.HTTPStatus = routeRes.StatusCode
				}
				outcomes[i] = outcome
				return
			}
			outcome.RouteRegions = uniqueSortedRouteRegions(route.Segments)

			origin := route.Origin
			destination := route.Destination
			journey, bookingRes, err := createJourney(ctx, session, origin, destination, departure, session.VehicleType, "normal")
			if err != nil {
				outcome.Error = err.Error()
				if bookingRes != nil {
					outcome.HTTPStatus = bookingRes.StatusCode
				}
				outcomes[i] = outcome
				return
			}
			outcome.HTTPStatus = bookingRes.StatusCode
			outcome.JourneyID = journey.JourneyID
			outcome.Status = journey.Status
			outcome.RejectionReason = journey.RejectionReason
			outcomes[i] = outcome
		}()
	}

	wg.Wait()
	return outcomes
}

func fetchRouteByPlan(ctx context.Context, session *userSession, plan routePlan) (*HTTPResult, mapRoutePayload, error) {
	query := url.Values{}
	query.Set("origin_node_id", plan.OriginID)
	query.Set("destination_node_id", plan.DestinationID)

	res, err := session.Client.do(ctx, RequestSpec{
		Name:    "map-route-" + plan.Label,
		Method:  http.MethodGet,
		Path:    "/api/v1/map/route",
		Query:   query,
		UseAuth: true,
	})
	if err != nil {
		return nil, mapRoutePayload{}, err
	}
	if res.StatusCode != http.StatusOK {
		return res, mapRoutePayload{}, fmt.Errorf("route fetch failed with %d (%s)", res.StatusCode, res.errorMessage())
	}

	var route mapRoutePayload
	if err := res.decodeEnvelopeData(&route); err != nil {
		return res, mapRoutePayload{}, err
	}
	if len(route.Segments) == 0 {
		return res, mapRoutePayload{}, fmt.Errorf("route %s returned zero segments", plan.Label)
	}
	return res, route, nil
}

func buildSegmentWindows(departure time.Time, segments []mapRouteSegment) []segmentWindow {
	windows := make([]segmentWindow, 0, len(segments))
	cursor := departure.UTC()
	for _, segment := range segments {
		minutes := segment.TraversalTimeMinutes
		if minutes <= 0 {
			minutes = 1
		}
		start := cursor
		end := cursor.Add(time.Duration(minutes) * time.Minute)
		windows = append(windows, segmentWindow{
			SegmentID: segment.SegmentID,
			Start:     start,
			End:       end,
		})
		cursor = end
	}
	return windows
}

func nextHalfHourSlot(minAhead time.Duration) time.Time {
	now := time.Now().UTC().Add(minAhead)
	trimmed := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, time.UTC)
	minuteMod := trimmed.Minute() % 30
	if minuteMod != 0 {
		trimmed = trimmed.Add(time.Duration(30-minuteMod) * time.Minute)
	} else {
		trimmed = trimmed.Add(30 * time.Minute)
	}
	return trimmed
}

func findNodeByID(nodes []mapNode, id string) (mapNode, bool) {
	for _, node := range nodes {
		if node.NodeID == id {
			return node, true
		}
	}
	return mapNode{}, false
}

func journeyInList(journeys []journeyPayload, journeyID string) bool {
	for _, journey := range journeys {
		if journey.JourneyID == journeyID {
			return true
		}
	}
	return false
}

func uniqueSortedRouteRegions(segments []mapRouteSegment) []string {
	set := make(map[string]struct{})
	for _, segment := range segments {
		region := strings.TrimSpace(strings.ToLower(segment.Region))
		if region == "" {
			continue
		}
		set[region] = struct{}{}
	}
	regions := make([]string, 0, len(set))
	for region := range set {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	return regions
}

func hasDuplicateJourneyID(outcomes []bookingOutcome) bool {
	seen := make(map[string]struct{})
	for _, outcome := range outcomes {
		id := strings.TrimSpace(outcome.JourneyID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

func sessionByAlias(sessions []*userSession, alias string) *userSession {
	for _, session := range sessions {
		if session.Alias == alias {
			return session
		}
	}
	return nil
}

func runStep(sr *ScenarioRecorder, name string, fn func(*StepContext) error) error {
	var stepErr error
	sr.runStep(name, func(step *StepContext) error {
		stepErr = fn(step)
		return stepErr
	})
	return stepErr
}
