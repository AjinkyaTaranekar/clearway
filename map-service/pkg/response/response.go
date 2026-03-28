package response

import (
	"encoding/json"
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
)

// Response represents a standard API response
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	TraceID string      `json:"trace_id,omitempty"`
}

// ErrorInfo represents error information in response
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PaginatedResponse represents a paginated API response
type PaginatedResponse struct {
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
	Error      *ErrorInfo  `json:"error,omitempty"`
	TraceID    string      `json:"trace_id,omitempty"`
}

// Pagination represents pagination metadata
type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

// JSON sends a JSON response
func JSON(w http.ResponseWriter, statusCode int, data interface{}, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(statusCode)

	resp := Response{
		Success: statusCode >= 200 && statusCode < 300,
		Data:    data,
		TraceID: traceID,
	}

	json.NewEncoder(w).Encode(resp)
}

// Error sends an error response
func Error(w http.ResponseWriter, err error, traceID string) {
	appErr := errors.GetAppError(err)
	if appErr == nil {
		appErr = errors.InternalError("An unexpected error occurred", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(appErr.Status)

	resp := Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    appErr.Code,
			Message: appErr.Message,
		},
		TraceID: traceID,
	}

	json.NewEncoder(w).Encode(resp)
}

// Paginated sends a paginated response
func Paginated(w http.ResponseWriter, data interface{}, pagination *Pagination, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)

	resp := PaginatedResponse{
		Success:    true,
		Data:       data,
		Pagination: pagination,
		TraceID:    traceID,
	}

	json.NewEncoder(w).Encode(resp)
}

// Success sends a success response with default status 200
func Success(w http.ResponseWriter, data interface{}, traceID string) {
	JSON(w, http.StatusOK, data, traceID)
}

// Created sends a created response with status 201
func Created(w http.ResponseWriter, data interface{}, traceID string) {
	JSON(w, http.StatusCreated, data, traceID)
}

// NoContent sends a no content response with status 204
func NoContent(w http.ResponseWriter, traceID string) {
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusNoContent)
}
