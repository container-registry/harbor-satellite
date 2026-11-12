//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type seen struct {
	called       bool
	id           string
	name, region string
	hasName      bool
	hasRegion    bool
}

func recordingHandler(s *seen) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.called = true
		if id, ok := GetSPIFFEID(r.Context()); ok {
			s.id = id.String()
		}
		s.name, s.hasName = GetSatelliteName(r.Context())
		s.region, s.hasRegion = GetRegion(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func requestWithPeer(t *testing.T, uri string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	if uri != "" {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certWithURI(t, uri)}}
	}
	return r
}

func TestSPIFFEMiddlewares(t *testing.T) {
	tests := []struct {
		name       string
		peer       string
		want       seen
		requireErr bool
	}{
		{
			name: "full satellite ID",
			peer: "spiffe://harbor-satellite.test/satellite/region/eu/edge-01",
			want: seen{called: true, id: "spiffe://harbor-satellite.test/satellite/region/eu/edge-01", name: "edge-01", hasName: true, region: "eu", hasRegion: true},
		},
		{
			name: "satellite without region",
			peer: "spiffe://harbor-satellite.test/satellite/edge-02",
			want: seen{called: true, id: "spiffe://harbor-satellite.test/satellite/edge-02", name: "edge-02", hasName: true},
		},
		{
			name: "not a satellite keeps only the ID",
			peer: "spiffe://harbor-satellite.test/gc/main",
			want: seen{called: true, id: "spiffe://harbor-satellite.test/gc/main"},
		},
		{
			name:       "no client certificate",
			want:       seen{called: true},
			requireErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got seen
			rec := httptest.NewRecorder()
			AuthMiddleware(recordingHandler(&got)).ServeHTTP(rec, requestWithPeer(t, tt.peer))
			require.Equal(t, tt.want, got, "AuthMiddleware")
			require.Equal(t, http.StatusNoContent, rec.Code, "AuthMiddleware never rejects")

			got = seen{}
			rec = httptest.NewRecorder()
			RequireSPIFFEAuth(recordingHandler(&got)).ServeHTTP(rec, requestWithPeer(t, tt.peer))
			if tt.requireErr {
				require.False(t, got.called, "RequireSPIFFEAuth passed a request without identity")
				require.Equal(t, http.StatusUnauthorized, rec.Code)
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				var body map[string]string
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				require.Equal(t, "SPIFFE authentication required", body["error"])
				return
			}
			require.Equal(t, tt.want, got, "RequireSPIFFEAuth")
		})
	}
}

func TestContextWithSatelliteName(t *testing.T) {
	_, ok := GetSatelliteName(context.Background())
	require.False(t, ok)
	name, ok := GetSatelliteName(ContextWithSatelliteName(context.Background(), "edge-01"))
	require.True(t, ok)
	require.Equal(t, "edge-01", name)
}

func TestTokenAuthMiddleware(t *testing.T) {
	validator := func(token string) (int64, error) {
		if token == "Bearer good" {
			return 42, nil
		}
		return 0, errors.New("unknown token")
	}
	tests := []struct {
		name      string
		validator func(string) (int64, error)
		header    string
		code      int
		wantName  string
		errBody   string
	}{
		{name: "no validator", header: "Bearer good", code: http.StatusInternalServerError, errBody: "token validator not configured"},
		{name: "no token", validator: validator, code: http.StatusUnauthorized, errBody: "authorization token required"},
		{name: "bad token", validator: validator, header: "Bearer bad", code: http.StatusUnauthorized, errBody: "invalid token"},
		{name: "good token", validator: validator, header: "Bearer good", code: http.StatusNoContent, wantName: "satellite-42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTokenAuthMiddleware()
			if tt.validator != nil {
				m.SetTokenValidator(tt.validator)
			}
			var got seen
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			m.Wrap(recordingHandler(&got)).ServeHTTP(rec, r)
			require.Equal(t, tt.code, rec.Code)
			if tt.errBody != "" {
				require.False(t, got.called)
				var body map[string]string
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				require.Equal(t, tt.errBody, body["error"])
				return
			}
			require.Equal(t, tt.wantName, got.name)
		})
	}
}
