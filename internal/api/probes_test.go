package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sorotrail/sorolens/internal/source"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// probeServer mirrors cmd/sorolens: the probes live at the root, outside
// the /api tree that Routes() returns.
func probeServer(status source.Status) http.Handler {
	s := New(&fakeSource{status: status}, discardLogger())
	mux := http.NewServeMux()
	mux.Handle("/health", s.HealthHandler())
	mux.Handle("/livez", s.LivezHandler())
	mux.Handle("/readyz", s.ReadyzHandler())
	return mux
}

// livez must stay 200 while the backend is down: a liveness probe that
// fails on dependencies makes an orchestrator restart-loop a recoverable
// instance instead of pulling it from the load balancer.
func TestLivezHealthyWhenBackendDown(t *testing.T) {
	srv := httptest.NewServer(probeServer(source.Status{
		Mode:    "rpc",
		Healthy: false,
		Detail:  "connection refused",
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("livez with backend down = %d, want 200", res.StatusCode)
	}
}

func TestReadyzHealthy(t *testing.T) {
	srv := httptest.NewServer(probeServer(source.Status{
		Mode:         "rpc",
		Healthy:      true,
		LatestLedger: 1234,
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz with healthy source = %d, want 200", res.StatusCode)
	}
}

func TestReadyzReportsBackendFailure(t *testing.T) {
	srv := httptest.NewServer(probeServer(source.Status{
		Mode:    "sorotrail",
		Healthy: false,
		State:   source.StateDown,
		Detail:  "indexer unreachable",
		Dependencies: map[string]source.DependencyStatus{
			"sorotrail": {Healthy: false, Status: source.StateDown, Detail: "indexer unreachable"},
		},
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz with unhealthy source = %d, want 503", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "not ready" {
		t.Fatalf("readyz status = %v, want not ready", body["status"])
	}
	if body["state"] != "down" {
		t.Fatalf("readyz state = %v, want down", body["state"])
	}
	deps, ok := body["dependencies"].(map[string]any)
	if !ok || deps["sorotrail"] == nil {
		t.Fatalf("readyz dependencies missing sorotrail: %+v", body)
	}
}

func TestHealthReportsPerDependencyDetail(t *testing.T) {
	srv := httptest.NewServer(probeServer(source.Status{
		Mode:    "rpc",
		Healthy: false,
		State:   source.StateDegraded,
		Detail:  "stellar rpc unreachable: timeout",
		Dependencies: map[string]source.DependencyStatus{
			"database": {Healthy: true, Status: source.StateOK},
			"rpc":      {Healthy: false, Status: source.StateDown, Detail: "timeout"},
		},
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("health with degraded source = %d, want 503", res.StatusCode)
	}
	var got source.Status
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.State != source.StateDegraded {
		t.Fatalf("state = %q, want %q", got.State, source.StateDegraded)
	}
	if !got.Dependencies["database"].Healthy || got.Dependencies["rpc"].Healthy {
		t.Fatalf("unexpected dependencies: %+v", got.Dependencies)
	}
}

func TestVersionEndpoint(t *testing.T) {
	// Built through the api tree like the other endpoints; pins the real
	// values, so a broken ldflags path shows up as "dev"/"none".
	srv := httptest.NewServer(New(&fakeSource{}, discardLogger()).Routes())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/version")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("version = %d, want 200", res.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "commit", "build_date"} {
		if body[key] == "" {
			t.Fatalf("version response missing %q: %+v", key, body)
		}
	}
}
