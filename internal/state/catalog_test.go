package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func testContext() context.Context {
	log := zerolog.Nop()
	return context.WithValue(context.Background(), logger.LoggerKey, &log)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestComputeManifestSize(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantSize int64
		wantErr  bool
	}{
		{
			name: "valid manifest with config and layers",
			manifest: `{
				"schemaVersion": 2,
				"config": {"size": 1500},
				"layers": [
					{"size": 10000},
					{"size": 20000}
				]
			}`,
			wantSize: 31500,
			wantErr:  false,
		},
		{
			name: "empty layers",
			manifest: `{
				"schemaVersion": 2,
				"config": {"size": 500},
				"layers": []
			}`,
			wantSize: 500,
			wantErr:  false,
		},
		{
			name: "single layer",
			manifest: `{
				"schemaVersion": 2,
				"config": {"size": 0},
				"layers": [{"size": 42}]
			}`,
			wantSize: 42,
			wantErr:  false,
		},
		{
			name:     "invalid json",
			manifest: `not json`,
			wantSize: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := computeManifestSize([]byte(tt.manifest))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSize, size)
		})
	}
}

func TestFetchCatalog(t *testing.T) {
	t.Run("returns repository list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v2/_catalog", r.URL.Path)
			writeJSON(t, w, catalogResponse{Repositories: []string{"library/nginx", "library/alpine"}})
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		repos, err := fetchCatalog(context.Background(), addr)
		require.NoError(t, err)
		require.Equal(t, []string{"library/nginx", "library/alpine"}, repos)
	})

	t.Run("empty catalog", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, catalogResponse{Repositories: []string{}})
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		repos, err := fetchCatalog(context.Background(), addr)
		require.NoError(t, err)
		require.Empty(t, repos)
	})

	t.Run("server error returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		_, err := fetchCatalog(context.Background(), addr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "500")
	})

	t.Run("unreachable server returns error", func(t *testing.T) {
		_, err := fetchCatalog(context.Background(), "127.0.0.1:1")
		require.Error(t, err)
	})
}

func TestFetchTags(t *testing.T) {
	t.Run("returns tag list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v2/library/nginx/tags/list", r.URL.Path)
			writeJSON(t, w, tagsResponse{Tags: []string{"latest", "1.25"}})
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		tags, err := fetchTags(context.Background(), addr, "library/nginx")
		require.NoError(t, err)
		require.Equal(t, []string{"latest", "1.25"}, tags)
	})

	t.Run("empty tags", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, tagsResponse{Tags: []string{}})
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		tags, err := fetchTags(context.Background(), addr, "library/nginx")
		require.NoError(t, err)
		require.Empty(t, tags)
	})

	t.Run("server error returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		_, err := fetchTags(context.Background(), addr, "nonexistent")
		require.Error(t, err)
	})
}

func TestCollectCachedImages(t *testing.T) {
	t.Run("empty catalog returns empty slice", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, catalogResponse{Repositories: []string{}})
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		ctx := testContext()
		images, err := collectCachedImages(ctx, addr, true)
		require.NoError(t, err)
		require.Empty(t, images)
		require.NotNil(t, images)
	})

	t.Run("skips repos with failed tag fetch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/_catalog" {
				writeJSON(t, w, catalogResponse{Repositories: []string{"badrepo"}})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "http://")
		ctx := testContext()
		images, err := collectCachedImages(ctx, addr, true)
		require.NoError(t, err)
		require.Empty(t, images)
	})

	t.Run("unreachable registry returns error", func(t *testing.T) {
		ctx := testContext()
		_, err := collectCachedImages(ctx, "127.0.0.1:1", true)
		require.Error(t, err)
	})
}

func TestCollectCachedImages_SkipsFailedImages(t *testing.T) {
	t.Run("skips images where manifest is missing", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2/":
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		addr := strings.TrimPrefix(srv.URL, "https://")
		ctx := testContext()
		_, err := collectImageInfo(ctx, addr+"/library/nginx:latest", true)
		require.Error(t, err)
	})
}

func TestCollectCachedImages_FullFlowWithTLS(t *testing.T) {
	configDigest := "sha256:aabbccddee00112233445566778899aabbccddee00112233445566778899aabb"
	layer1Digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	layer2Digest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	manifestDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	manifest := fmt.Sprintf(`{
		"schemaVersion": 2,
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": {"mediaType": "application/vnd.oci.image.config.v1+json", "size": 1000, "digest": "%s"},
		"layers": [
			{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "size": 5000, "digest": "%s"},
			{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "size": 3000, "digest": "%s"}
		]
	}`, configDigest, layer1Digest, layer2Digest)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/library/nginx/manifests/latest":
			if r.Method == http.MethodHead {
				w.Header().Set("Docker-Content-Digest", manifestDigest)
				w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(manifest)))
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, err := w.Write([]byte(manifest))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "https://")
	ctx := testContext()

	img, err := collectImageInfo(ctx, addr+"/library/nginx:latest", true)
	require.NoError(t, err)
	require.Contains(t, img.Reference, "library/nginx:latest@"+manifestDigest)
	require.Equal(t, int64(9000), img.SizeBytes)
}
