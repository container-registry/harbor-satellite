package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateJoinToken_MissingSatelliteName_Returns400(t *testing.T) {
	server := &Server{}

	reqBody := CreateJoinTokenRequest{
		SatelliteName: "",
		Selectors:     []string{"unix:uid:1000"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/join-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createJoinTokenHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCreateJoinToken_MissingSelectors_Returns400(t *testing.T) {
	server := &Server{}

	reqBody := CreateJoinTokenRequest{
		SatelliteName: "test-satellite",
		Selectors:     []string{},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/join-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createJoinTokenHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCreateJoinToken_InvalidSelectorFormat_Returns400(t *testing.T) {
	server := &Server{}

	reqBody := CreateJoinTokenRequest{
		SatelliteName: "test-satellite",
		Selectors:     []string{"invalid-selector-without-colon"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/join-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createJoinTokenHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCreateJoinToken_NoSpireClient_Returns503(t *testing.T) {
	server := &Server{
		spireClient: nil,
	}

	reqBody := CreateJoinTokenRequest{
		SatelliteName: "test-satellite",
		Selectors:     []string{"unix:uid:1000"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/join-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createJoinTokenHandler(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}
