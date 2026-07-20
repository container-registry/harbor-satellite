package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppError_Error(t *testing.T) {
	err := &AppError{Message: "something went wrong", Code: 400}
	if err.Error() != "something went wrong" {
		t.Errorf("expected 'something went wrong', got '%s'", err.Error())
	}
}

func TestWriteJSONError(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		statusCode int
	}{
		{"bad request", "invalid input", http.StatusBadRequest},
		{"not found", "resource not found", http.StatusNotFound},
		{"internal error", "server error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSONError(w, tt.message, tt.statusCode)

			if w.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, w.Code)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to unmarshal response body: %v", err)
			}
			if body["error"] != tt.message {
				t.Errorf("expected error message '%s', got '%s'", tt.message, body["error"])
			}
		})
	}
}

func TestWriteJSONResponse(t *testing.T) {
	t.Run("marshallable data", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := map[string]string{"key": "value"}
		WriteJSONResponse(w, http.StatusOK, data)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if body["key"] != "value" {
			t.Errorf("expected value, got %s", body["key"])
		}
	})
	t.Run("unmarshallable data fallback", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteJSONResponse(w, http.StatusOK, make(chan int))
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "error") {
			t.Errorf("expected error in body, got %s", w.Body.String())
		}
	})
}

func TestHandleAppError(t *testing.T) {
	t.Run("with AppError", func(t *testing.T) {
		w := httptest.NewRecorder()
		HandleAppError(w, &AppError{Message: "not found", Code: http.StatusNotFound})

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
		var body AppError
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if body.Message != "not found" {
			t.Errorf("expected 'not found', got '%s'", body.Message)
		}
	})

	t.Run("with plain error", func(t *testing.T) {
		w := httptest.NewRecorder()
		HandleAppError(w, fmt.Errorf("some plain error"))

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Internal Server Error") {
			t.Errorf("expected error message in body, got %s", w.Body.String())
		}
	})
}
