package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/middleware"
	"github.com/stretchr/testify/require"
)

func TestStack_OrderMetricsAuthenticateAdmission(t *testing.T) {
	var order []string
	mark := func(name string) proxy.MiddlewareFunc {
		return func(next proxy.HandlerFunc) proxy.HandlerFunc {
			return func(r *proxy.Request) error {
				order = append(order, name)
				return next(r)
			}
		}
	}
	terminal := func(*proxy.Request) error {
		order = append(order, "pull")
		return nil
	}

	handler := proxy.New(terminal).WrapAll(middleware.Stack(mark("admission"), mark("authenticate"), mark("metrics"))...).Handler()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/3.20", nil))

	require.Equal(t, []string{"metrics", "authenticate", "admission", "pull"}, order)
}

func TestStack_AllNilKeepsTheHandler(t *testing.T) {
	called := false
	handler := proxy.New(func(*proxy.Request) error { called = true; return nil }).WrapAll(middleware.Stack(nil, nil, nil)...).Handler()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v2/library/alpine/manifests/3.20", nil))
	require.True(t, called)
}
