package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

func (e *AppError) Error() string {
	return e.Message
}

// write JSON error response with given status code and message.
func WriteJSONError(w http.ResponseWriter, message string, statusCode int) {
	respBytes, err := json.Marshal(AppError{Message: message, Code: int64(statusCode)})
	if err != nil {
		log.Printf("Failed to marshal JSON error response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"message":"Internal server error","code":500}`)); err != nil {
			log.Printf("Failed to write fallback JSON error response: %v", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respBytes); err != nil {
		log.Printf("Failed to write JSON error response: %v", err)
	}
}

// write JSON response with given status code and data.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, data any) {
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
	var appErr *AppError
	if errors.As(err, &appErr) {
		WriteJSONResponse(w, int(appErr.Code), appErr)
	} else {
		WriteJSONResponse(w, http.StatusInternalServerError, &AppError{
			Message: "Internal Server Error",
			Code:    http.StatusInternalServerError,
		})
	}
}
