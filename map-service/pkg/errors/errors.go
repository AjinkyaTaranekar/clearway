package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError represents an application error with HTTP status code
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
	Err     error  `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// Common error codes
const (
	CodeNotFound           = "NOT_FOUND"
	CodeBadRequest         = "BAD_REQUEST"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeConflict           = "CONFLICT"
	CodeInternalError      = "INTERNAL_ERROR"
	CodeValidationFailed   = "VALIDATION_FAILED"
	CodeDatabaseError      = "DATABASE_ERROR"
	CodeExternalAPIError   = "EXTERNAL_API_ERROR"
	CodeAccountLocked      = "ACCOUNT_LOCKED"
	CodeAccountSuspended   = "ACCOUNT_SUSPENDED"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
)

// NotFound creates a not found error
func NotFound(message string) *AppError {
	return &AppError{
		Code:    CodeNotFound,
		Message: message,
		Status:  http.StatusNotFound,
	}
}

// BadRequest creates a bad request error
func BadRequest(message string) *AppError {
	return &AppError{
		Code:    CodeBadRequest,
		Message: message,
		Status:  http.StatusBadRequest,
	}
}

// Unauthorized creates an unauthorized error
func Unauthorized(message string) *AppError {
	return &AppError{
		Code:    CodeUnauthorized,
		Message: message,
		Status:  http.StatusUnauthorized,
	}
}

// Forbidden creates a forbidden error
func Forbidden(message string) *AppError {
	return &AppError{
		Code:    CodeForbidden,
		Message: message,
		Status:  http.StatusForbidden,
	}
}

// Conflict creates a conflict error
func Conflict(message string) *AppError {
	return &AppError{
		Code:    CodeConflict,
		Message: message,
		Status:  http.StatusConflict,
	}
}

// InternalError creates an internal server error
func InternalError(message string, err error) *AppError {
	return &AppError{
		Code:    CodeInternalError,
		Message: message,
		Status:  http.StatusInternalServerError,
		Err:     err,
	}
}

// ValidationFailed creates a validation failed error
func ValidationFailed(message string) *AppError {
	return &AppError{
		Code:    CodeValidationFailed,
		Message: message,
		Status:  http.StatusBadRequest,
	}
}

// DatabaseError creates a database error
func DatabaseError(message string, err error) *AppError {
	return &AppError{
		Code:    CodeDatabaseError,
		Message: message,
		Status:  http.StatusInternalServerError,
		Err:     err,
	}
}

// ExternalAPIError creates an external API error
func ExternalAPIError(message string, err error) *AppError {
	return &AppError{
		Code:    CodeExternalAPIError,
		Message: message,
		Status:  http.StatusBadGateway,
		Err:     err,
	}
}

// AccountLocked creates an account locked error
func AccountLocked(message string) *AppError {
	return &AppError{
		Code:    CodeAccountLocked,
		Message: message,
		Status:  http.StatusForbidden,
	}
}

// AccountSuspended creates an account suspended error
func AccountSuspended(message string) *AppError {
	return &AppError{
		Code:    CodeAccountSuspended,
		Message: message,
		Status:  http.StatusForbidden,
	}
}

// InvalidCredentials creates an invalid credentials error
func InvalidCredentials(message string) *AppError {
	return &AppError{
		Code:    CodeInvalidCredentials,
		Message: message,
		Status:  http.StatusUnauthorized,
	}
}

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr)
}

// GetAppError extracts AppError from error
func GetAppError(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}
