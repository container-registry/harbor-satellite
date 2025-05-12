package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/google/uuid"
)

type ErrorCategory string

const (
	CategoryValidation  ErrorCategory = "validation"
	CategoryDatabase    ErrorCategory = "database"
	CategoryExternalAPI ErrorCategory = "external_api"
	CategorySecurity    ErrorCategory = "security"
	CategoryInternal    ErrorCategory = "internal"
	CategoryNotFound    ErrorCategory = "not_found"
)

// structured error type.
type AppError struct {
	ID            string        `json:"id"`
	Timestamp     time.Time     `json:"timestamp"`
	Code          int           `json:"code"`
	Message       string        `json:"message"`
	Category      ErrorCategory `json:"category"`
	Details       string        `json:"details,omitempty"`
	Source        string        `json:"source,omitempty"`
	RequestID     string        `json:"request_id,omitempty"`
	Suggestion    string        `json:"suggestion,omitempty"`
	originalError error         `json:"-"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Category, e.Message, e.Details)
}

// NewAppError creates a new application error with the given parameters
func NewAppError(message string, code int, category ErrorCategory, originalErr error) *AppError {
	// Generate source file and line information
	_, file, line, _ := runtime.Caller(1)

	details := ""
	if originalErr != nil {
		details = originalErr.Error()
	}

	return &AppError{
		ID:            uuid.New().String(),
		Timestamp:     time.Now(),
		Code:          code,
		Message:       message,
		Category:      category,
		Details:       details,
		Source:        fmt.Sprintf("%s:%d", file, line),
		originalError: originalErr,
	}
}

func (e *AppError) WithSuggestion(suggestion string) *AppError {
	e.Suggestion = suggestion
	return e
}

// write JSON response with given status code and data.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

// HandleAppError handles an AppError and sends a structured JSON response
func HandleAppError(w http.ResponseWriter, err error) {
	if appErr, ok := err.(*AppError); ok {
		WriteJSONResponse(w, appErr.Code, appErr)
	} else {
		// For non-AppError errors, create a generic internal server error
		genericErr := NewAppError(
			"Internal Server Error",
			http.StatusInternalServerError,
			CategoryInternal,
			err,
		)
		WriteJSONResponse(w, genericErr.Code, genericErr)
	}
}
