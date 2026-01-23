package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// structured error type.
type AppError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *AppError) Error() string {
	return e.Message
}

// write JSON error response with given status code and message.
func WriteJSONError(w http.ResponseWriter, message string, statusCode int) {
	respBytes, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		log.Printf("Failed to marshal JSON error response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"Internal server error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respBytes); err != nil {
		log.Printf("Failed to write JSON error response: %v", err)
	}
}

// write JSON response with given status code and data.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	respBytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal JSON response: %v", err)
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respBytes); err != nil {
		log.Printf("Failed to write JSON response: %v", err)
	}
}

// handle AppError and senda structured JSON response.
func HandleAppError(w http.ResponseWriter, err error) {
	if appErr, ok := err.(*AppError); ok {
		WriteJSONResponse(w, appErr.Code, appErr)
	} else {
		WriteJSONResponse(w, http.StatusInternalServerError, &AppError{
			Message: "Internal Server Error",
			Code:    http.StatusInternalServerError,
		})
	}
}
