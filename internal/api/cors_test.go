package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowedOrigin(t *testing.T) {
	h := CORSMiddleware(CORSConfig{Origins: []string{"https://explorer.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Origin", "https://explorer.example")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "https://explorer.example" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := res.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q, want Origin", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := CORSMiddleware(CORSConfig{Origins: []string{"*"}})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodOptions, "/api/events", nil)
	req.Header.Set("Origin", "https://any.example")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", res.Code)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "https://any.example" {
		t.Fatalf("allow-origin = %q", got)
	}
}

func TestCORSDisallowedOriginGetsNoHeaders(t *testing.T) {
	h := CORSMiddleware(CORSConfig{Origins: []string{"https://explorer.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Origin", "https://evil.example")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin got CORS headers: %q", got)
	}
	if res.Code != http.StatusOK {
		t.Fatalf("non-preflight request must still be served, got %d", res.Code)
	}
}

func TestCORSEmptyConfigIsNoop(t *testing.T) {
	h := CORSMiddleware(CORSConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Origin", "https://any.example")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("empty config should emit no headers, got %q", got)
	}
}
