package server

import (
	"encoding/json"
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
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
