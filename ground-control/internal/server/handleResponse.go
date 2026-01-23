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

// write JSON response with given status code and data.
func WriteJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	respBytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
