package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/ground-control/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestHashRobotCredentials(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{
			name:   "basic secret",
			secret: "s3cret-value-123",
		},
		{
			name:   "empty secret",
			secret: "",
		},
		{
			name:   "long secret",
			secret: strings.Repeat("a", 256),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := crypto.HashSecret(tt.secret)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(hash, "$argon2id$"), "hash should start with $argon2id$")
		})
	}

	t.Run("random salt produces unique hashes", func(t *testing.T) {
		h1, err := crypto.HashSecret("same-secret")
		require.NoError(t, err)
		h2, err := crypto.HashSecret("same-secret")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2, "same secret with random salt should produce different hashes")
	})

	t.Run("different secrets produce different hashes", func(t *testing.T) {
		h1, err := crypto.HashSecret("secret-1")
		require.NoError(t, err)
		h2, err := crypto.HashSecret("secret-2")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2)
	})
}

func TestVerifyRobotCredentials(t *testing.T) {
	secret := "correct-secret"
	storedHash, err := crypto.HashSecret(secret)
	require.NoError(t, err)

	tests := []struct {
		name   string
		secret string
		want   bool
	}{
		{
			name:   "correct secret",
			secret: secret,
			want:   true,
		},
		{
			name:   "wrong secret",
			secret: "wrong-secret",
			want:   false,
		},
		{
			name:   "empty secret",
			secret: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crypto.VerifySecret(tt.secret, storedHash)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("malformed hash returns false", func(t *testing.T) {
		require.False(t, crypto.VerifySecret(secret, "not-a-valid-hash"))
	})
}

func TestDecodeRequestParams_QueryOnly(t *testing.T) {
	type params struct {
		Page   int     `query:"page"`
		Limit  int     `query:"limit"`
		Search string  `query:"search"`
		Score  float64 `query:"score"`
		Active bool    `query:"active"`
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
		want    params
	}{
		{
			name: "all types decoded correctly",
			url:  "/things?page=2&limit=50&search=hello&score=3.14&active=true",
			want: params{Page: 2, Limit: 50, Search: "hello", Score: 3.14, Active: true},
		},
		{
			name: "missing params leave zero values",
			url:  "/things",
			want: params{},
		},
		{
			name:    "invalid int",
			url:     "/things?page=abc",
			wantErr: true,
		},
		{
			name:    "invalid float",
			url:     "/things?score=notafloat",
			wantErr: true,
		},
		{
			name:    "invalid bool",
			url:     "/things?active=notabool",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			var p params
			err := DecodeRequestParams(r, &p, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && p != tt.want {
				t.Errorf("got %+v, want %+v", p, tt.want)
			}
		})
	}

	t.Run("non-pointer dst", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/things", nil)
		if err := DecodeRequestParams(r, params{}, nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("pointer to non-struct", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/things", nil)
		x := 42
		if err := DecodeRequestParams(r, &x, nil); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestDecodeRequestParams_PathOnly(t *testing.T) {
	type params struct {
		ProjectID int    `path:"projectID"`
		Slug      string `path:"slug"`
	}

	tests := []struct {
		name     string
		pathVars map[string]string
		wantErr  bool
		want     params
	}{
		{
			name:     "string and int path params",
			pathVars: map[string]string{"projectID": "42", "slug": "hello"},
			want:     params{ProjectID: 42, Slug: "hello"},
		},
		{
			name:     "missing path vars leave zero values",
			pathVars: map[string]string{},
			want:     params{},
		},
		{
			name:     "invalid int in path",
			pathVars: map[string]string{"projectID": "notanint"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			pv := func(key string) string { return tt.pathVars[key] }
			var p params
			err := DecodeRequestParams(r, &p, pv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && p != tt.want {
				t.Errorf("got %+v, want %+v", p, tt.want)
			}
		})
	}
}

func TestDecodeRequestParams_PathAndQuery(t *testing.T) {
	type params struct {
		ProjectID int    `path:"projectID"`
		Slug      string `path:"slug"`
		Page      int    `query:"page"`
		Limit     int    `query:"limit"`
		Search    string `query:"search"`
	}

	tests := []struct {
		name     string
		url      string
		pathVars map[string]string
		wantErr  bool
		want     params
	}{
		{
			name:     "path and query decoded together",
			url:      "/projects/42/hello?page=3&limit=10&search=world",
			pathVars: map[string]string{"projectID": "42", "slug": "hello"},
			want:     params{ProjectID: 42, Slug: "hello", Page: 3, Limit: 10, Search: "world"},
		},
		{
			name:     "missing query params leave zero values",
			url:      "/projects/42/hello",
			pathVars: map[string]string{"projectID": "42", "slug": "hello"},
			want:     params{ProjectID: 42, Slug: "hello"},
		},
		{
			name:     "missing path vars leave zero values",
			url:      "/projects?page=1",
			pathVars: map[string]string{},
			want:     params{Page: 1},
		},
		{
			name:     "invalid query param",
			url:      "/projects/42/hello?page=abc",
			pathVars: map[string]string{"projectID": "42", "slug": "hello"},
			wantErr:  true,
		},
		{
			name:     "invalid path param",
			url:      "/projects/notanint/hello",
			pathVars: map[string]string{"projectID": "notanint", "slug": "hello"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			pv := func(key string) string { return tt.pathVars[key] }
			var p params
			err := DecodeRequestParams(r, &p, pv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && p != tt.want {
				t.Errorf("got %+v, want %+v", p, tt.want)
			}
		})
	}
}

func TestSetField(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		field   any
		want    any
		wantErr bool
	}{
		{name: "string", raw: "hello", field: new(string), want: "hello"},
		{name: "int", raw: "42", field: new(int), want: int64(42)},
		{name: "int64", raw: "-99", field: new(int64), want: int64(-99)},
		{name: "uint", raw: "7", field: new(uint), want: uint64(7)},
		{name: "uint64", raw: "100", field: new(uint64), want: uint64(100)},
		{name: "float32", raw: "1.5", field: new(float32), want: float64(1.5)},
		{name: "float64", raw: "3.14", field: new(float64), want: float64(3.14)},
		{name: "bool true", raw: "true", field: new(bool), want: true},
		{name: "bool false", raw: "false", field: new(bool), want: false},
		{name: "invalid int", raw: "abc", field: new(int), wantErr: true},
		{name: "invalid uint", raw: "-1", field: new(uint), wantErr: true},
		{name: "invalid float", raw: "abc", field: new(float64), wantErr: true},
		{name: "invalid bool", raw: "abc", field: new(bool), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fv := reflect.ValueOf(tt.field).Elem()
			err := setField(fv, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			var got any
			switch fv.Kind() {
			case reflect.String:
				got = fv.String()
			case reflect.Int, reflect.Int64:
				got = fv.Int()
			case reflect.Uint, reflect.Uint64:
				got = fv.Uint()
			case reflect.Float32, reflect.Float64:
				got = fv.Float()
			case reflect.Bool:
				got = fv.Bool()
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
