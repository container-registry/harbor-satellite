package peer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"
)

const distributionAPIVersion = "registry/2.0"

// Layout is the digest-addressable subset of an ORAS OCI layout.
type Layout interface {
	Resolve(ctx context.Context, reference string) (ocispec.Descriptor, error)
	Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error)
}

// Handler returns the replica-mode OCI Distribution handler over layout.
// GET/HEAD serve retained content; mutations are rejected. This is the
// ADR-0009 proxy bound as this satellite's registry, not a second server.
func Handler(layout Layout) http.Handler {
	return proxy.New(serveLayout(layout)).Handler()
}

// NewHTTPServer listens on addr and serves the replica proxy.
func NewHTTPServer(addr string, layout Layout) *http.Server {
	return &http.Server{
		Addr:              NormalizeAddr(addr),
		Handler:           Handler(layout),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// NormalizeAddr prepends a colon when addr is a bare port.
func NormalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	if !strings.Contains(addr, ":") {
		return ":" + addr
	}
	return addr
}

// SplitURLs splits a comma-separated peer URL list.
func SplitURLs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			urls = append(urls, part)
		}
	}
	return urls
}

func serveLayout(layout Layout) proxy.Process {
	return func(request *proxy.Request) error {
		switch request.Operation {
		case proxy.CheckRegistry:
			request.ResponseHeader().Set("Docker-Distribution-API-Version", distributionAPIVersion)
			return request.Write(http.StatusOK, nil)
		case proxy.PullManifest, proxy.CheckManifest:
			return serveDigest(request, layout, proxy.ErrorCodeManifestUnknown)
		case proxy.PullBlob, proxy.CheckBlob:
			return serveDigest(request, layout, proxy.ErrorCodeBlobUnknown)
		default:
			return proxy.NewError(proxy.ErrorCodeUnsupported, "replica proxy is read-only", nil)
		}
	}
}

func serveDigest(request *proxy.Request, layout Layout, unknown proxy.ErrorCode) error {
	if request.Digest == "" {
		return proxy.NewError(unknown, "", nil)
	}

	desc, err := layout.Resolve(request.Context(), request.Digest.String())
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return proxy.NewError(unknown, "", nil)
		}
		return err
	}

	header := request.ResponseHeader()
	header.Set("Content-Type", desc.MediaType)
	header.Set("Docker-Content-Digest", desc.Digest.String())
	header.Set("Content-Length", strconv.FormatInt(desc.Size, 10))

	if request.Operation == proxy.CheckManifest || request.Operation == proxy.CheckBlob {
		return request.Write(http.StatusOK, nil)
	}

	payload, err := fetchAll(request.Context(), layout, desc)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return proxy.NewError(unknown, "", nil)
		}
		return err
	}
	return request.Write(http.StatusOK, payload)
}

func fetchAll(ctx context.Context, layout Layout, desc ocispec.Descriptor) ([]byte, error) {
	reader, err := layout.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if desc.Size > 0 && int64(len(payload)) != desc.Size {
		return nil, fmt.Errorf("blob size mismatch: got %d want %d", len(payload), desc.Size)
	}
	return payload, nil
}
