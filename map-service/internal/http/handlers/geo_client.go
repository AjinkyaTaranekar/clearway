package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
)

const (
	defaultNominatimBaseURL = "https://nominatim.openstreetmap.org"
	defaultOSRMBaseURL      = "https://router.project-osrm.org"
)

var segmentIDSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// GeoClient wraps Nominatim (search) and OSRM (routing) APIs.
type GeoClient struct {
	nominatimBaseURL string
	osrmBaseURL      string
	userAgent        string
	httpCli          *http.Client
	log              *logger.Logger
}

// NewGeoClient creates a new GeoClient.
func NewGeoClient(nominatimBaseURL, osrmBaseURL, userAgent string, log *logger.Logger) *GeoClient {
	if strings.TrimSpace(nominatimBaseURL) == "" {
		nominatimBaseURL = defaultNominatimBaseURL
	}
	if strings.TrimSpace(osrmBaseURL) == "" {
		osrmBaseURL = defaultOSRMBaseURL
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "vcs-map-service/1.0"
	}

	return &GeoClient{
		nominatimBaseURL: strings.TrimRight(nominatimBaseURL, "/"),
		osrmBaseURL:      strings.TrimRight(osrmBaseURL, "/"),
		userAgent:        userAgent,
		httpCli: &http.Client{
			Timeout: 55 * time.Second,
		},
		log: log,
	}
}

// PlaceResult represents a single location returned by geocoding search.
type PlaceResult struct {
	PlaceID string  `json:"place_id"`
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

type nominatimSearchResult struct {
	OSMType     string `json:"osm_type"`
	OSMID       int64  `json:"osm_id"`
	DisplayName string `json:"display_name"`
	Name        string `json:"name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Address     struct {
		Road          string `json:"road"`
		City          string `json:"city"`
		Town          string `json:"town"`
		Village       string `json:"village"`
		Hamlet        string `json:"hamlet"`
		County        string `json:"county"`
		State         string `json:"state"`
		Country       string `json:"country"`
		CountryCode   string `json:"country_code"`
		Postcode      string `json:"postcode"`
		Neighbourhood string `json:"neighbourhood"`
	} `json:"address"`
}

// SearchPlaces calls Nominatim search and returns up to limit results.
func (c *GeoClient) SearchPlaces(ctx context.Context, query string, limit int) ([]PlaceResult, error) {
	requestLog := logWithTrace(ctx)
	if c != nil && c.log != nil {
		requestLog = c.log
	}
	requestLog.Debug().
		Str("client", "GeoClient.SearchPlaces").
		Str("query", query).
		Int("limit", limit).
		Msg("calling Nominatim search")

	if c == nil {
		return nil, fmt.Errorf("geo client not configured")
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}

	v := url.Values{}
	v.Set("q", query)
	v.Set("format", "jsonv2")
	v.Set("addressdetails", "1")
	v.Set("limit", strconv.Itoa(limit))

	rawURL := fmt.Sprintf("%s/search?%s", c.nominatimBaseURL, v.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept-Language", "en")

	resp, err := c.httpCli.Do(req)
	if err != nil {
		requestLog.Error().
			Str("client", "GeoClient.SearchPlaces").
			Err(err).
			Msg("Nominatim search request failed")
		return nil, fmt.Errorf("nominatim search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestLog.Error().
			Str("client", "GeoClient.SearchPlaces").
			Int("status_code", resp.StatusCode).
			Msg("Nominatim search returned non-200 status")
		return nil, fmt.Errorf("nominatim search HTTP %d", resp.StatusCode)
	}

	var raw []nominatimSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		requestLog.Error().
			Str("client", "GeoClient.SearchPlaces").
			Err(err).
			Msg("failed to decode Nominatim search response")
		return nil, fmt.Errorf("decode nominatim search: %w", err)
	}

	out := make([]PlaceResult, 0, len(raw))
	for _, r := range raw {
		lat, err := strconv.ParseFloat(r.Lat, 64)
		if err != nil {
			continue
		}
		lng, err := strconv.ParseFloat(r.Lon, 64)
		if err != nil {
			continue
		}

		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = firstNonEmpty(r.Address.City, r.Address.Town, r.Address.Village, r.Address.Hamlet, r.Address.Road, r.DisplayName)
		}

		placeID := fmt.Sprintf("osm:%s:%d", strings.TrimSpace(r.OSMType), r.OSMID)
		out = append(out, PlaceResult{
			PlaceID: placeID,
			Name:    name,
			Address: strings.TrimSpace(r.DisplayName),
			Lat:     lat,
			Lng:     lng,
		})
	}
	requestLog.Debug().
		Str("client", "GeoClient.SearchPlaces").
		Str("query", query).
		Int("result_count", len(out)).
		Msg("Nominatim search completed")
	return out, nil
}

// RoutePoint is a lat/lng pair used in routing requests.
type RoutePoint struct {
	Lat float64
	Lng float64
}

// RouteStep holds one logical segment of a routed path.
type RouteStep struct {
	SegmentID       string
	SegmentName     string
	DurationSeconds float64
}

// RouteResult holds the summary of an OSRM routing response.
type RouteResult struct {
	DistanceMeters  float64
	DurationSeconds float64
	Segments        []RouteStep
	Points          [][2]float64
}

type osrmRouteResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
		Legs []struct {
			Steps []struct {
				Name     string  `json:"name"`
				Ref      string  `json:"ref"`
				Duration float64 `json:"duration"`
				Distance float64 `json:"distance"`
			} `json:"steps"`
		} `json:"legs"`
	} `json:"routes"`
}

// GetRoute calls OSRM and returns the best route.
func (c *GeoClient) GetRoute(ctx context.Context, origin, dest RoutePoint) (*RouteResult, error) {
	requestLog := logWithTrace(ctx)
	if c != nil && c.log != nil {
		requestLog = c.log
	}
	requestLog.Debug().
		Str("client", "GeoClient.GetRoute").
		Float64("origin_lat", origin.Lat).
		Float64("origin_lng", origin.Lng).
		Float64("destination_lat", dest.Lat).
		Float64("destination_lng", dest.Lng).
		Msg("calling OSRM route API")

	if c == nil {
		return nil, fmt.Errorf("geo client not configured")
	}

	coords := fmt.Sprintf("%.6f,%.6f;%.6f,%.6f",
		origin.Lng, origin.Lat, dest.Lng, dest.Lat)
	rawURL := fmt.Sprintf(
		"%s/route/v1/driving/%s?overview=full&geometries=geojson&steps=true",
		c.osrmBaseURL, coords,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		requestLog.Error().
			Str("client", "GeoClient.GetRoute").
			Err(err).
			Msg("OSRM route request failed")
		return nil, fmt.Errorf("osrm route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestLog.Error().
			Str("client", "GeoClient.GetRoute").
			Int("status_code", resp.StatusCode).
			Msg("OSRM route returned non-200 status")
		return nil, fmt.Errorf("osrm route HTTP %d", resp.StatusCode)
	}

	var raw osrmRouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		requestLog.Error().
			Str("client", "GeoClient.GetRoute").
			Err(err).
			Msg("failed to decode OSRM route response")
		return nil, fmt.Errorf("decode osrm route: %w", err)
	}

	if raw.Code != "Ok" {
		requestLog.Warn().Str("client", "GeoClient.GetRoute").Str("code", raw.Code).Msg("OSRM route response code not Ok")
		return nil, fmt.Errorf("osrm route failed with code %q", raw.Code)
	}
	if len(raw.Routes) == 0 {
		requestLog.Warn().Str("client", "GeoClient.GetRoute").Msg("OSRM route response had no routes")
		return nil, fmt.Errorf("no routes returned")
	}

	best := raw.Routes[0]
	steps := collapseSteps(best.Legs)
	result := &RouteResult{
		DistanceMeters:  best.Distance,
		DurationSeconds: best.Duration,
		Segments:        steps,
	}
	for _, coord := range best.Geometry.Coordinates {
		if len(coord) < 2 {
			continue
		}
		result.Points = append(result.Points, [2]float64{coord[1], coord[0]})
	}
	requestLog.Debug().
		Str("client", "GeoClient.GetRoute").
		Float64("distance_meters", result.DistanceMeters).
		Float64("duration_seconds", result.DurationSeconds).
		Int("segment_count", len(result.Segments)).
		Int("point_count", len(result.Points)).
		Msg("OSRM route call completed")
	return result, nil
}

func collapseSteps(legs []struct {
	Steps []struct {
		Name     string  `json:"name"`
		Ref      string  `json:"ref"`
		Duration float64 `json:"duration"`
		Distance float64 `json:"distance"`
	} `json:"steps"`
}) []RouteStep {
	collapsed := make([]RouteStep, 0)
	for _, leg := range legs {
		for _, step := range leg.Steps {
			segmentName := firstNonEmpty(strings.TrimSpace(step.Ref), strings.TrimSpace(step.Name), "Unnamed Road")
			segmentID := deriveSegmentID(segmentName)
			if len(collapsed) > 0 && collapsed[len(collapsed)-1].SegmentID == segmentID {
				collapsed[len(collapsed)-1].DurationSeconds += step.Duration
				continue
			}
			collapsed = append(collapsed, RouteStep{
				SegmentID:       segmentID,
				SegmentName:     segmentName,
				DurationSeconds: step.Duration,
			})
		}
	}
	return collapsed
}

func deriveSegmentID(roadName string) string {
	normalized := strings.ToLower(strings.TrimSpace(roadName))
	normalized = segmentIDSanitizer.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		normalized = "unnamed"
	}
	return "osrm_" + normalized
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func durationSecondsToMinutes(durationSeconds float64) int {
	minutes := int(math.Round(durationSeconds / 60.0))
	if minutes < 1 {
		return 1
	}
	return minutes
}
