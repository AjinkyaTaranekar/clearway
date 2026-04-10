package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	appErrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
)

// SearchHandler proxies place searches to Nominatim so the browser talks to
// our backend and stays decoupled from provider-specific payloads.
type SearchHandler struct {
	geo *GeoClient
	log *logger.Logger
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(geo *GeoClient, log *logger.Logger) *SearchHandler {
	return &SearchHandler{geo: geo, log: log}
}

// SearchPlaces godoc
// @Summary      Proxy Nominatim place search
// @Description  Searches for places matching the query string via Nominatim.
// @Tags         map
// @Produce      json
// @Param        q      query  string  true   "Search query"
// @Param        limit  query  int     false  "Max results (1-10, default 5)"
// @Success      200    {object}  response.Response
// @Failure      400    {object}  response.Response
// @Failure      502    {object}  response.Response
// @Router       /api/v1/map/search [get]
func (h *SearchHandler) SearchPlaces(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "SearchHandler.SearchPlaces").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("search places request received")

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		log.Warn().
			Str("handler", "SearchHandler.SearchPlaces").
			Msg("search validation failed: query missing")
		response.Error(w, appErrors.BadRequest("query parameter 'q' is required"), traceID)
		return
	}

	limit := 5
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 10 {
			limit = n
		}
	}

	log.Info().
		Str("handler", "SearchHandler.SearchPlaces").
		Str("query", query).
		Int("limit", limit).
		Msg("calling geocoding search provider")

	places, err := h.geo.SearchPlaces(r.Context(), query, limit)
	if err != nil {
		if errors.Is(err, ErrNominatimRateLimited) {
			log.Warn().
				Str("handler", "SearchHandler.SearchPlaces").
				Str("query", query).
				Msg("Nominatim rate limited; propagating 429 to client")
			response.Error(w, appErrors.RateLimited("search rate limited — please retry after a moment"), traceID)
			return
		}
		log.Error().
			Str("handler", "SearchHandler.SearchPlaces").
			Err(err).
			Str("query", query).
			Msg("search provider request failed")
		response.Error(w, appErrors.InternalError("place search failed", err), traceID)
		return
	}

	log.Info().
		Str("handler", "SearchHandler.SearchPlaces").
		Str("query", query).
		Int("result_count", len(places)).
		Msg("search places request completed")

	response.Success(w, map[string]interface{}{
		"places": places,
	}, traceID)
}
