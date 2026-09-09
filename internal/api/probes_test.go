package api

import (
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
		Detail:  "indexer unreachable",
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
}
