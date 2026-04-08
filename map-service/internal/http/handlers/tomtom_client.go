package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
)

const (
	tomtomSearchBase  = "https://api.tomtom.com/search/2/search"
	tomtomRoutingBase = "https://api.tomtom.com/routing/1/calculateRoute"
)

// TomTomClient wraps the TomTom Search and Routing APIs.
// The API key is kept server-side; it is never sent to the browser.
type TomTomClient struct {
	apiKey  string
	httpCli *http.Client
	log     *logger.Logger
}

// NewTomTomClient creates a new TomTomClient.
func NewTomTomClient(apiKey string, log *logger.Logger) *TomTomClient {
	return &TomTomClient{
		apiKey: apiKey,
		httpCli: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// PlaceResult represents a single location returned by the TomTom Fuzzy Search.
type PlaceResult struct {
	// PlaceID is the TomTom entity ID (stable across searches for the same POI).
	PlaceID string  `json:"place_id"`
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

// tomtomSearchResponse mirrors the TomTom fuzzy-search JSON envelope.
type tomtomSearchResponse struct {
	Results []struct {
		ID      string `json:"id"`
		Address struct {
			FreeformAddress string `json:"freeformAddress"`
			Municipality    string `json:"municipality"`
			Country         string `json:"country"`
		} `json:"address"`
		POI *struct {
			Name string `json:"name"`
		} `json:"poi"`
		Position struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"position"`
	} `json:"results"`
}

// FuzzySearch calls the TomTom Fuzzy Search endpoint and returns up to limit results.
func (c *TomTomClient) FuzzySearch(ctx context.Context, query string, limit int) ([]PlaceResult, error) {
	requestLog := logWithTrace(ctx)
	if c != nil && c.log != nil {
		requestLog = c.log
	}
	requestLog.Debug().
		Str("client", "TomTomClient.FuzzySearch").
		Str("query", query).
		Int("limit", limit).
		Msg("calling TomTom fuzzy search")

	if c.apiKey == "" {
		return nil, fmt.Errorf("TOMTOM_API_KEY not configured")
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}

	encoded := url.PathEscape(query)
	rawURL := fmt.Sprintf(
		"%s/%s.json?key=%s&limit=%d&countrySet=IE,GB&language=en-GB",
		tomtomSearchBase, encoded, c.apiKey, limit,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		requestLog.Error().
			Str("client", "TomTomClient.FuzzySearch").
			Err(err).
			Msg("TomTom fuzzy search request failed")
		return nil, fmt.Errorf("tomtom search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestLog.Error().
			Str("client", "TomTomClient.FuzzySearch").
			Int("status_code", resp.StatusCode).
			Msg("TomTom fuzzy search returned non-200 status")
		return nil, fmt.Errorf("tomtom search HTTP %d", resp.StatusCode)
	}

	var raw tomtomSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		requestLog.Error().
			Str("client", "TomTomClient.FuzzySearch").
			Err(err).
			Msg("failed to decode TomTom fuzzy search response")
		return nil, fmt.Errorf("decode tomtom search: %w", err)
	}

	out := make([]PlaceResult, 0, len(raw.Results))
	for _, r := range raw.Results {
		name := r.Address.Municipality
		if name == "" {
			name = r.Address.FreeformAddress
		}
		if r.POI != nil && r.POI.Name != "" {
			name = r.POI.Name
		}
		addr := r.Address.FreeformAddress
		out = append(out, PlaceResult{
			PlaceID: r.ID,
			Name:    name,
			Address: addr,
			Lat:     r.Position.Lat,
			Lng:     r.Position.Lon,
		})
	}
	requestLog.Debug().
		Str("client", "TomTomClient.FuzzySearch").
		Str("query", query).
		Int("result_count", len(out)).
		Msg("TomTom fuzzy search completed")
	return out, nil
}

// RoutePoint is a lat/lng pair used in routing requests.
type RoutePoint struct {
	Lat float64
	Lng float64
}

// RouteResult holds the summary of a TomTom routing response.
type RouteResult struct {
	DistanceMeters  int
	DurationSeconds int
	// Points is the decoded polyline as [lat, lng] pairs.
	Points [][2]float64
}

// tomtomRouteResponse mirrors the relevant parts of the TomTom routing JSON.
type tomtomRouteResponse struct {
	Routes []struct {
		Summary struct {
			LengthInMeters      int `json:"lengthInMeters"`
			TravelTimeInSeconds int `json:"travelTimeInSeconds"`
		} `json:"summary"`
		Legs []struct {
			Points []struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"points"`
		} `json:"legs"`
	} `json:"routes"`
}

// GetRoute calls the TomTom Calculate Route endpoint and returns the best route.
func (c *TomTomClient) GetRoute(ctx context.Context, origin, dest RoutePoint) (*RouteResult, error) {
	requestLog := logWithTrace(ctx)
	if c != nil && c.log != nil {
		requestLog = c.log
	}
	requestLog.Debug().
		Str("client", "TomTomClient.GetRoute").
		Float64("origin_lat", origin.Lat).
		Float64("origin_lng", origin.Lng).
		Float64("destination_lat", dest.Lat).
		Float64("destination_lng", dest.Lng).
		Msg("calling TomTom route API")

	if c.apiKey == "" {
		return nil, fmt.Errorf("TOMTOM_API_KEY not configured")
	}

	coords := fmt.Sprintf("%.6f,%.6f:%.6f,%.6f",
		origin.Lat, origin.Lng, dest.Lat, dest.Lng)
	rawURL := fmt.Sprintf(
		"%s/%s/json?key=%s&travelMode=car&routeType=fastest",
		tomtomRoutingBase, coords, c.apiKey,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		requestLog.Error().
			Str("client", "TomTomClient.GetRoute").
			Err(err).
			Msg("TomTom route request failed")
		return nil, fmt.Errorf("tomtom route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestLog.Error().
			Str("client", "TomTomClient.GetRoute").
			Int("status_code", resp.StatusCode).
			Msg("TomTom route returned non-200 status")
		return nil, fmt.Errorf("tomtom route HTTP %d", resp.StatusCode)
	}

	var raw tomtomRouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		requestLog.Error().
			Str("client", "TomTomClient.GetRoute").
			Err(err).
			Msg("failed to decode TomTom route response")
		return nil, fmt.Errorf("decode tomtom route: %w", err)
	}

	if len(raw.Routes) == 0 {
		requestLog.Warn().Str("client", "TomTomClient.GetRoute").Msg("TomTom route response had no routes")
		return nil, fmt.Errorf("no routes returned")
	}

	best := raw.Routes[0]
	result := &RouteResult{
		DistanceMeters:  best.Summary.LengthInMeters,
		DurationSeconds: best.Summary.TravelTimeInSeconds,
	}
	for _, leg := range best.Legs {
		for _, pt := range leg.Points {
			result.Points = append(result.Points, [2]float64{pt.Latitude, pt.Longitude})
		}
	}
	requestLog.Debug().
		Str("client", "TomTomClient.GetRoute").
		Int("distance_meters", result.DistanceMeters).
		Int("duration_seconds", result.DurationSeconds).
		Int("point_count", len(result.Points)).
		Msg("TomTom route call completed")
	return result, nil
}
