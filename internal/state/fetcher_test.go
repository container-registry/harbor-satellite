package state

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// nopLogger here returns a zerolog logger that discards all the output
func nopLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// imageWithFile builds a v1.Image whose filesystem contains a single file at the given path with the given content
func imageWithFile(t *testing.T, filename string, content []byte) v1.Image {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: filename,
		Mode: 0o644,
		Size: int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	layer, err := tarball.LayerFromReader(&buf)
	require.NoError(t, err)

	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	return img
}

// NewURLStateFetcherWithTLS
func TestNewURLStateFetcherWithTLS_PrefixStripping(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		insecure    bool
		wantURL     string
		wantUseHTTP bool
	}{
		{
			name:        "http prefix stripped, useHTTP true",
			input:       "http://registry.example.com/repo:tag",
			insecure:    false,
			wantURL:     "registry.example.com/repo:tag",
			wantUseHTTP: true,
		},
		{
			name:        "https prefix stripped, useHTTP false",
			input:       "https://registry.example.com/repo:tag",
			insecure:    false,
			wantURL:     "registry.example.com/repo:tag",
			wantUseHTTP: false,
		},
		{
			name:        "bare URL, insecure false, useHTTP false",
			input:       "registry.example.com/repo:tag",
			insecure:    false,
			wantURL:     "registry.example.com/repo:tag",
			wantUseHTTP: false,
		},
		{
			name:        "bare URL, insecure true, useHTTP true",
			input:       "registry.example.com/repo:tag",
			insecure:    true,
			wantURL:     "registry.example.com/repo:tag",
			wantUseHTTP: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sf := NewURLStateFetcherWithTLS(tc.input, "user", "pass", tc.insecure, config.TLSConfig{})
			f, ok := sf.(*URLStateFetcher)
			require.True(t, ok)
			require.Equal(t, tc.wantURL, f.url)
			require.Equal(t, tc.wantUseHTTP, f.useHTTP)
		})
	}
}

// FetchStateArtifact — unknown type
func TestFetchStateArtifact_UnknownType(t *testing.T) {
	f := &URLStateFetcher{}
	type unknown struct{}
	err := f.FetchStateArtifact(context.Background(), &unknown{}, nopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected state type")
}

// extractArtifactJSON

func TestExtractArtifactJSON_HappyPath(t *testing.T) {
	want := SatelliteState{States: []string{"s1", "s2"}, Config: "cfg"}
	data, err := json.Marshal(want)
	require.NoError(t, err)

	img := imageWithFile(t, "artifacts.json", data)
	f := &URLStateFetcher{}

	var got SatelliteState
	require.NoError(t, f.extractArtifactJSON("test", img, &got, nopLogger()))
	require.Equal(t, want, got)
}

func TestExtractArtifactJSON_MissingFile(t *testing.T) {
	// Image with a different filename — artifacts.json will never be found.
	img := imageWithFile(t, "other.json", []byte(`{}`))
	f := &URLStateFetcher{}

	var got SatelliteState
	err := f.extractArtifactJSON("test", img, &got, nopLogger())
	require.Error(t, err)
	require.Contains(t, err.Error(), "artifacts.json not found")
}

func TestExtractArtifactJSON_InvalidJSON(t *testing.T) {
	img := imageWithFile(t, "artifacts.json", []byte(`not valid json {{{`))
	f := &URLStateFetcher{}

	var got SatelliteState
	err := f.extractArtifactJSON("test", img, &got, nopLogger())
	require.Error(t, err)
}

// FromJSON
func TestFromJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid",
			data:    []byte(`{"registry":"registry.example.com","artifacts":[]}`),
			wantErr: false,
		},
		{
			name:    "missing registry URL",
			data:    []byte(`{"registry":"","artifacts":[]}`),
			wantErr: true,
			errMsg:  "registry URL is required",
		},
		{
			name:    "malformed JSON",
			data:    []byte(`{invalid`),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := FromJSON(tc.data, &State{})
			if tc.wantErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// httpTransport.RoundTrip
func TestHTTPTransport_RoundTrip_SchemeRewrittenToHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	transport := &httpTransport{base: http.DefaultTransport}
	httpsURL := strings.Replace(srv.URL, "http://", "https://", 1) + "/ping"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, httpsURL, nil)
	require.NoError(t, err)
	require.Equal(t, "https", req.URL.Scheme)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Original request here must not be mutated.
	require.Equal(t, "https", req.URL.Scheme)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
