package gcclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

type request struct {
	Method string
	Path   string
	Auth   string
	Body   string
}

// fakeGC records every request and answers each with status and body.
type fakeGC struct {
	server *httptest.Server
	status int
	body   any
	mu     sync.Mutex
	seen   []request
}

func newFakeGC(t *testing.T, status int, body any) *fakeGC {
	t.Helper()
	gc := &fakeGC{status: status, body: body}
	gc.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		gc.mu.Lock()
		gc.seen = append(gc.seen, request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: string(data)})
		gc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(gc.status)
		require.NoError(t, json.NewEncoder(w).Encode(gc.body))
	}))
	t.Cleanup(gc.server.Close)
	return gc
}

func (gc *fakeGC) requests() []request {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return append([]request(nil), gc.seen...)
}

// anyGCResponse satisfies every satellite call, so only the request decides
// whether a call is right.
var anyGCResponse = map[string]any{
	"events": []string{},
	"state":  "http://harbor.test/satellite/satellite-state/edge-01/state:latest",
	"auth":   map[string]string{"url": "http://harbor.test", "username": "robot$edge-01", "password": "secret"},
}

// noRedirects makes a redirect visible as the response instead of a second
// request.
func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func newClient(t *testing.T, baseURL string) *gcclient.Client {
	t.Helper()
	c, err := gcclient.New(gcclient.Options{BaseURL: baseURL, HTTPClient: noRedirects(), Credentials: gcclient.NoCredentials{}, AllowInsecure: true})
	require.NoError(t, err)
	return c
}

func TestClient_PathsWithTrailingSlashAndPrefix(t *testing.T) {
	pending.Plan(t, "D1", "the satellite joins the Ground Control URL and the route with a format string")
	ctx := context.Background()
	calls := []struct {
		name   string
		method string
		path   string
		do     func(*gcclient.Client) error
	}{
		{name: "ztr", method: http.MethodPost, path: "/satellites/ztr", do: func(c *gcclient.Client) error {
			_, err := c.ZeroTouch(ctx, "ztr-token")
			return err
		}},
		{name: "spiffe-ztr", method: http.MethodGet, path: "/satellites/spiffe-ztr", do: func(c *gcclient.Client) error {
			_, err := c.SPIFFEZeroTouch(ctx)
			return err
		}},
		{name: "sync", method: http.MethodPost, path: "/satellites/sync", do: func(c *gcclient.Client) error {
			_, err := c.Sync(ctx, gcclient.StatusReport{Name: "edge-01"})
			return err
		}},
	}
	bases := []struct {
		name   string
		suffix string
		prefix string
	}{
		{name: "plain", suffix: "", prefix: ""},
		{name: "trailing slash", suffix: "/", prefix: ""},
		{name: "path prefix", suffix: "/gc/", prefix: "/gc"},
	}

	for _, base := range bases {
		for _, call := range calls {
			t.Run(base.name+"/"+call.name, func(t *testing.T) {
				gc := newFakeGC(t, http.StatusOK, anyGCResponse)
				c := newClient(t, gc.server.URL+base.suffix)

				require.NoError(t, call.do(c))

				got := gc.requests()
				require.Len(t, got, 1)
				require.Equal(t, call.method, got[0].Method)
				require.Equal(t, base.prefix+call.path, got[0].Path)
				require.NotContains(t, got[0].Path, "//")
				require.NotContains(t, got[0].Path, "/sat/")
			})
		}
	}
}

func TestClientSync_ReturnsEvents(t *testing.T) {
	pending.Plan(t, "D1", "the heartbeat does not go through the generated Ground Control client")
	gc := newFakeGC(t, http.StatusOK, map[string]any{"events": []string{"refresh_credentials"}})
	c := newClient(t, gc.server.URL)

	res, err := c.Sync(context.Background(), gcclient.StatusReport{Name: "edge-01"})

	require.NoError(t, err)
	require.Equal(t, []string{"refresh_credentials"}, res.Events)
}

func TestClientZeroTouch_DecodesStateAndRobot(t *testing.T) {
	pending.Plan(t, "D1", "token ZTR does not go through the generated Ground Control client")
	gc := newFakeGC(t, http.StatusOK, anyGCResponse)
	c := newClient(t, gc.server.URL)

	res, err := c.ZeroTouch(context.Background(), "ztr-token")

	require.NoError(t, err)
	require.Equal(t, gcclient.ZTRResult{
		StateURL: "http://harbor.test/satellite/satellite-state/edge-01/state:latest",
		Robot:    gcclient.RobotAccount{RegistryURL: "http://harbor.test", Username: "robot$edge-01", Password: "secret"},
	}, res)
	got := gc.requests()
	require.Len(t, got, 1)
	require.JSONEq(t, `{"token":"ztr-token"}`, got[0].Body)
}

func TestClient_Credentials(t *testing.T) {
	cases := []struct {
		name  string
		creds gcclient.Credentials
		want  string
	}{
		{
			name:  "robot basic",
			creds: gcclient.RobotBasic(func() (string, string) { return "alice", "s3cret" }),
			want:  "Basic YWxpY2U6czNjcmV0",
		},
		{name: "no credentials", creds: gcclient.NoCredentials{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://gc.test/satellites/sync", nil)

			tc.creds.Authorize(req)

			require.Equal(t, tc.want, req.Header.Get("Authorization"))
		})
	}
}

func TestClient_MapsErrorStatuses(t *testing.T) {
	pending.Plan(t, "D1", "the heartbeat reports every non-200 status as one opaque error")
	cases := []struct {
		status int
		want   error
	}{
		{status: http.StatusUnauthorized, want: gcclient.ErrUnauthorized},
		{status: http.StatusForbidden, want: gcclient.ErrForbidden},
		{status: http.StatusTooManyRequests, want: gcclient.ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			gc := newFakeGC(t, tc.status, map[string]any{"message": "no", "code": tc.status})
			c := newClient(t, gc.server.URL)

			_, err := c.Sync(context.Background(), gcclient.StatusReport{Name: "edge-01"})

			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("other status", func(t *testing.T) {
		gc := newFakeGC(t, http.StatusInternalServerError, map[string]any{"message": "database down", "code": http.StatusInternalServerError})
		c := newClient(t, gc.server.URL)

		_, err := c.Sync(context.Background(), gcclient.StatusReport{Name: "edge-01"})

		var statusErr *gcclient.StatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusInternalServerError, statusErr.StatusCode)
	})
}

func TestNew_RequiresHTTPSUnlessInsecure(t *testing.T) {
	pending.Plan(t, "D1", "the https requirement lives in the status report only")
	robot := gcclient.RobotBasic(func() (string, string) { return "robot$edge-01", "secret" })
	cases := []struct {
		name     string
		baseURL  string
		creds    gcclient.Credentials
		insecure bool
		wantErr  bool
	}{
		{name: "https", baseURL: "https://gc.test", creds: robot},
		{name: "http without use_unsecure", baseURL: "http://gc.test", creds: robot, wantErr: true},
		{name: "http with use_unsecure", baseURL: "http://gc.test", creds: robot, insecure: true},
		{name: "http for a SPIFFE satellite", baseURL: "http://gc.test", creds: gcclient.NoCredentials{}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gcclient.New(gcclient.Options{BaseURL: tc.baseURL, HTTPClient: noRedirects(), Credentials: tc.creds, AllowInsecure: tc.insecure})

			require.False(t, errors.Is(err, notimpl.ErrNotImplemented), "%v", err)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClientRefreshCredentials_PostsToSatellitesRefresh(t *testing.T) {
	pending.Plan(t, "D2", "the credential refresh goes to /sat/refresh outside the generated client")
	gc := newFakeGC(t, http.StatusOK, map[string]string{"secret": "n3w"})
	c := newClient(t, gc.server.URL)

	res, err := c.RefreshCredentials(context.Background())

	require.NoError(t, err)
	require.Equal(t, "n3w", res.Secret)
	got := gc.requests()
	require.Len(t, got, 1)
	require.Equal(t, http.MethodPost, got[0].Method)
	require.Equal(t, "/satellites/refresh", got[0].Path)
}
