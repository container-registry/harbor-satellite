package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
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

func TestCreateJoinToken_DuplicateSatellite_Returns409(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}).
		AddRow(1, "existing-satellite", time.Now(), time.Now(), sql.NullTime{}, sql.NullString{})
	mock.ExpectQuery("SELECT .+ FROM satellites WHERE name").
		WithArgs("existing-satellite").
		WillReturnRows(rows)

	server := &Server{
		spireClient: &spiffe.ServerClient{},
		dbQueries:   database.New(db),
	}

	reqBody := CreateJoinTokenRequest{
		SatelliteName: "existing-satellite",
		Selectors:     []string{"unix:uid:1000"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/join-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.createJoinTokenHandler(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d", http.StatusConflict, rr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled mock expectations: %v", err)
	}
}
