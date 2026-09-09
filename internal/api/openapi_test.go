package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// openapiSchema is the slice of the spec the coverage test needs.
type openapiSchema struct {
	Paths map[string]map[string]any `json:"paths"`
}

type route struct {
	Method string
	Path   string
}

// collectRoutes walks a chi router and returns every registered
// method/path pair, with subroute trailing slashes normalized away.
func collectRoutes(t *testing.T, r chi.Router) []route {
	var out []route
	walk := func(method string, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if len(path) > 1 {
			path = strings.TrimRight(path, "/")
		}
		out = append(out, route{Method: method, Path: path})
		return nil
	}
	if err := chi.Walk(r, walk); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return out
}

var methodKeys = map[string]string{
	http.MethodGet:    "get",
	http.MethodPost:   "post",
	http.MethodPatch:  "patch",
	http.MethodPut:    "put",
	http.MethodDelete: "delete",
}

// TestOpenAPISpecCoversAllRoutes fails when a route exists in the router
// but not in the spec, or vice versa. This is the drift check: adding a
// handler without documenting it, or documenting an endpoint that does not
// exist, both fail the build.
func TestOpenAPISpecCoversAllRoutes(t *testing.T) {
	var doc openapiSchema
	if err := json.Unmarshal(openapiSpec, &doc); err != nil {
		t.Fatalf("openapi.json does not parse: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("openapi.json has no paths")
	}

	s := New(&fakeSource{}, discardLogger())
	routes := collectRoutes(t, s.Routes())

	for _, rt := range routes {
		ops, ok := doc.Paths[rt.Path]
		if !ok {
			t.Errorf("route %s %s is registered but missing from openapi.json", rt.Method, rt.Path)
			continue
		}
		key, ok := methodKeys[rt.Method]
		if !ok {
			continue // framework-added methods (HEAD, OPTIONS)
		}
		if _, ok := ops[key]; !ok {
			t.Errorf("route %s %s is registered but openapi.json has no %s entry", rt.Method, rt.Path, key)
		}
	}

	for path, ops := range doc.Paths {
		for method := range ops {
			if method == "parameters" || method == "summary" || method == "description" || method == "servers" {
				continue
			}
			want, ok := map[string]string{
				"get": http.MethodGet, "post": http.MethodPost, "patch": http.MethodPatch,
				"put": http.MethodPut, "delete": http.MethodDelete,
			}[method]
			if !ok {
				t.Errorf("openapi.json has unexpected key %q under %q", method, path)
				continue
			}
			found := false
			for _, rt := range routes {
				if rt.Path == path && rt.Method == want {
					found = true
				}
			}
			if !found {
				t.Errorf("openapi.json documents %s %q but no such route is registered", method, path)
			}
		}
	}
}
