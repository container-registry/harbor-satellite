package state

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func testLogger() *zerolog.Logger {
	log := zerolog.Nop()
	return &log
}

func imageWithArtifactJSON(t *testing.T, artifactJSON string) v1.Image {
	t.Helper()
	return imageWithTarEntries(t, tarEntry{name: "artifacts.json", content: artifactJSON})
}

type tarEntry struct {
	name    string
	content string
}

func imageWithTarEntries(t *testing.T, entries ...tarEntry) v1.Image {
	t.Helper()

	var tarContent bytes.Buffer
	tw := tar.NewWriter(&tarContent)
	for _, entry := range entries {
		data := []byte(entry.content)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: entry.name,
			Mode: 0o600,
			Size: int64(len(data)),
		}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())

	tarPath := filepath.Join(t.TempDir(), "layer.tar")
	require.NoError(t, os.WriteFile(tarPath, tarContent.Bytes(), 0o600))

	layer, err := tarball.LayerFromFile(tarPath)
	require.NoError(t, err)

	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	return img
}

func pushStateImage(t *testing.T, addr, ref string, img v1.Image) {
	t.Helper()
	parsed, err := name.ParseReference(addr+"/"+ref, name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(parsed, img))
}

func TestNewURLStateFetcherWithTLS(t *testing.T) {
	tests := []struct {
		name     string
		stateURL string
		insecure bool
		wantURL  string
		wantHTTP bool
	}{
		{
			name:     "strips http prefix and forces http",
			stateURL: "http://example.com/library/state:latest",
			wantURL:  "example.com/library/state:latest",
			wantHTTP: true,
		},
		{
			name:     "strips https prefix and keeps https",
			stateURL: "https://example.com/library/state:latest",
			insecure: true,
			wantURL:  "example.com/library/state:latest",
		},
		{
			name:     "bare url uses insecure flag for http",
			stateURL: "example.com/library/state:latest",
			insecure: true,
			wantURL:  "example.com/library/state:latest",
			wantHTTP: true,
		},
		{
			name:     "bare url keeps https when insecure is false",
			stateURL: "example.com/library/state:latest",
			wantURL:  "example.com/library/state:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsCfg := config.TLSConfig{CAFile: "ca.pem", SkipVerify: true}
			fetcher := NewURLStateFetcherWithTLS(tt.stateURL, "user", "pass", tt.insecure, tlsCfg)
			urlFetcher, ok := fetcher.(*URLStateFetcher)
			require.True(t, ok)
			require.Equal(t, tt.wantURL, urlFetcher.url)
			require.Equal(t, "user", urlFetcher.username)
			require.Equal(t, "pass", urlFetcher.password)
			require.Equal(t, tt.insecure, urlFetcher.insecure)
			require.Equal(t, tt.wantHTTP, urlFetcher.useHTTP)
			require.Equal(t, tlsCfg, urlFetcher.tlsCfg)
		})
	}
}

func TestFetchStateArtifact(t *testing.T) {
	_, addr := newTestRegistry(t)
	ctx := testContext()
	log := testLogger()

	tests := []struct {
		name      string
		ref       string
		json      string
		out       any
		assertOut func(t *testing.T, out any)
	}{
		{
			name: "satellite state",
			ref:  "library/satellite-state:latest",
			json: `{"states":["group-a:latest"],"config":"config:latest"}`,
			out:  &SatelliteState{},
			assertOut: func(t *testing.T, out any) {
				got := out.(*SatelliteState)
				require.Equal(t, []string{"group-a:latest"}, got.States)
				require.Equal(t, "config:latest", got.Config)
			},
		},
		{
			name: "group state",
			ref:  "library/group-state:latest",
			json: `{"registry":"registry.example.com","artifacts":[{"repository":"library/nginx","tag":["latest"],"digest":"sha256:abc","type":"image"}]}`,
			out:  &State{},
			assertOut: func(t *testing.T, out any) {
				got := out.(*State)
				require.Equal(t, "registry.example.com", got.Registry)
				require.Len(t, got.Artifacts, 1)
				require.Equal(t, "library/nginx", got.Artifacts[0].Repository)
			},
		},
		{
			name: "config state",
			ref:  "library/config-state:latest",
			json: `{"state_config":{"state":"library/satellite-state:latest"},"app_config":{"log_level":"debug"}}`,
			out:  &config.Config{},
			assertOut: func(t *testing.T, out any) {
				got := out.(*config.Config)
				require.Equal(t, "library/satellite-state:latest", got.StateConfig.StateURL)
				require.Equal(t, "debug", got.AppConfig.LogLevel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pushStateImage(t, addr, tt.ref, imageWithArtifactJSON(t, tt.json))

			fetcher := NewURLStateFetcherWithTLS("http://"+addr+"/"+tt.ref, "", "", true, config.TLSConfig{})
			err := fetcher.FetchStateArtifact(ctx, tt.out, log)
			require.NoError(t, err)
			tt.assertOut(t, tt.out)
		})
	}

	t.Run("unknown type returns error", func(t *testing.T) {
		fetcher := NewURLStateFetcherWithTLS("http://"+addr+"/library/unknown:latest", "", "", true, config.TLSConfig{})
		err := fetcher.FetchStateArtifact(ctx, &struct{}{}, log)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected state type")
	})
}

func TestExtractArtifactJSON(t *testing.T) {
	log := testLogger()
	fetcher := &URLStateFetcher{}

	t.Run("valid tar with artifacts json", func(t *testing.T) {
		var out State
		img := imageWithArtifactJSON(t, `{"registry":"registry.example.com","artifacts":[]}`)

		err := fetcher.extractArtifactJSON("example.com/state:latest", img, &out, log)
		require.NoError(t, err)
		require.Equal(t, "registry.example.com", out.Registry)
		require.Empty(t, out.Artifacts)
	})

	t.Run("missing artifacts json", func(t *testing.T) {
		var out State
		img := imageWithTarEntries(t, tarEntry{name: "other.json", content: `{"registry":"registry.example.com"}`})

		err := fetcher.extractArtifactJSON("example.com/state:latest", img, &out, log)
		require.Error(t, err)
		require.Contains(t, err.Error(), "artifacts.json not found")
	})

	t.Run("malformed tar", func(t *testing.T) {
		var out State
		img := imageWithBadLayer(t, []byte("not a tar archive"))

		err := fetcher.extractArtifactJSON("example.com/state:latest", img, &out, log)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to export the state artifact")
	})

	t.Run("invalid json", func(t *testing.T) {
		var out State
		img := imageWithArtifactJSON(t, `{not json}`)

		err := fetcher.extractArtifactJSON("example.com/state:latest", img, &out, log)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid character")
	})
}

func imageWithBadLayer(t *testing.T, content []byte) v1.Image {
	t.Helper()

	layerPath := filepath.Join(t.TempDir(), "bad-layer.tar")
	require.NoError(t, os.WriteFile(layerPath, content, 0o600))

	layer, err := tarball.LayerFromFile(layerPath)
	require.NoError(t, err)

	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	return img
}

func TestFromJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
		assert  func(t *testing.T, got StateReader)
	}{
		{
			name: "valid input",
			data: []byte(`{"registry":"https://registry.example.com/","artifacts":[{"repository":"library/nginx","tag":["latest"]}]}`),
			assert: func(t *testing.T, got StateReader) {
				require.Equal(t, "registry.example.com", got.GetRegistryURL())
				require.Len(t, got.GetArtifacts(), 1)
			},
		},
		{
			name:    "missing registry url",
			data:    []byte(`{"artifacts":[]}`),
			wantErr: "registry URL is required",
		},
		{
			name:    "malformed json",
			data:    []byte(`{not json}`),
			wantErr: "invalid character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromJSON(tt.data, &State{})
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Nil(t, got)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			tt.assert(t, got)
		})
	}
}

func TestHTTPTransportRoundTrip(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "http", req.URL.Scheme)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	transport := &httpTransport{base: base}

	req, err := http.NewRequest(http.MethodGet, "https://example.com/v2/", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "https", req.URL.Scheme, "original request should not be mutated")
	require.Equal(t, "http", resp.Request.URL.Scheme)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
