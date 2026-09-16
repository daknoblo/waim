package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheckReflectsHTTPAvailability(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   int
	}{
		{"healthy", http.StatusOK, 0},
		{"unavailable", http.StatusServiceUnavailable, 1},
		{"not found", http.StatusNotFound, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" || r.Method != http.MethodGet {
					t.Errorf("unexpected healthcheck request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			t.Setenv("WAIM_ADDR", server.Listener.Addr().String())
			if got := healthcheck(); got != tc.want {
				t.Fatalf("healthcheck exit code = %d, want %d", got, tc.want)
			}
		})
	}
	t.Run("connection refused", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		t.Setenv("WAIM_ADDR", server.Listener.Addr().String())
		server.Close()
		if got := healthcheck(); got != 1 {
			t.Fatalf("closed server exit code = %d, want 1", got)
		}
	})
}

func TestEnvDefaultTrimsAndFallsBack(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"", "fallback"},
		{" \t ", "fallback"},
		{" localhost:9000 ", "localhost:9000"},
	} {
		t.Setenv("WAIM_TEST_ENV", tc.value)
		if got := envDefault("WAIM_TEST_ENV", "fallback"); got != tc.want {
			t.Fatalf("envDefault = %q, want %q", got, tc.want)
		}
	}
}
