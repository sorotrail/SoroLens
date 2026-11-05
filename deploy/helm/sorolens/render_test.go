package sorolens_test

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/sorotrail/sorolens/internal/config"
)

var update = flag.Bool("update", false, "regenerate golden files instead of comparing")

func chartDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func helmPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm binary not found in PATH; skipping Helm chart render tests")
	}
	return p
}

func renderChart(t *testing.T, helm string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{
		"template", "sorolens", chartDir(),
		"--namespace", "sorolens-test",
	}, extraArgs...)
	out, err := exec.Command(helm, args...).CombinedOutput()
	require.NoError(t, err, "helm template failed: %s", out)
	return string(out)
}

// TestRenderGolden_DefaultStandalone pins the default standalone render
// (SOURCE_MODE=rpc with a chart-managed database Secret).
//
// Regenerate with:
//
//	go test ./deploy/helm/sorolens -run TestRenderGolden -update
func TestRenderGolden_DefaultStandalone(t *testing.T) {
	helm := helmPath(t)
	got := renderChart(t, helm,
		"--set", "database.url=postgres://user:pass@host:5432/sorolens",
	)
	golden := filepath.Join(chartDir(), "testdata", "default.golden.yaml")
	assertGolden(t, golden, got)
}

// TestRenderGolden_RepresentativeOverrides pins the standalone render with
// existingSecret, ServiceMonitor, PodDisruptionBudget, and all optional
// standalone settings populated.
func TestRenderGolden_RepresentativeOverrides(t *testing.T) {
	helm := helmPath(t)
	got := renderChart(t, helm,
		"--values", filepath.Join(chartDir(), "testdata", "overrides.yaml"),
	)
	golden := filepath.Join(chartDir(), "testdata", "overrides.golden.yaml")
	assertGolden(t, golden, got)
}

// TestRenderGolden_UpstreamMode pins the upstream mode render
// (SOURCE_MODE=sorotrail), verifying that no Secret is created and standalone
// settings are omitted rather than rendered as empty strings.
func TestRenderGolden_UpstreamMode(t *testing.T) {
	helm := helmPath(t)
	got := renderChart(t, helm,
		"--values", filepath.Join(chartDir(), "testdata", "upstream.yaml"),
	)
	golden := filepath.Join(chartDir(), "testdata", "upstream.golden.yaml")
	assertGolden(t, golden, got)
}

func normalizeManifest(s string) string {
	for strings.Contains(s, "\n\n---\n") {
		s = strings.ReplaceAll(s, "\n\n---\n", "\n---\n")
	}
	return strings.TrimSpace(s) + "\n"
}

func assertGolden(t *testing.T, golden, got string) {
	t.Helper()
	if *update {
		require.NoError(t, os.WriteFile(golden, []byte(normalizeManifest(got)), 0o644))
		t.Logf("updated golden file %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run with -update to create it)", golden, err)
	}
	assert.Equal(t, normalizeManifest(string(want)), normalizeManifest(got),
		"rendered manifests drifted from %s; if the change is intentional, regenerate with:\n\tgo test ./deploy/helm/sorolens -run TestRenderGolden -update", golden)
}

func TestRender_ManifestInvariants(t *testing.T) {
	helm := helmPath(t)

	standalone := renderChart(t, helm,
		"--set", "database.url=postgres://user:pass@host:5432/sorolens",
	)
	deployment := manifest(t, standalone, "Deployment")
	cm := manifest(t, standalone, "ConfigMap")

	t.Run("probes hit /livez and /readyz", func(t *testing.T) {
		assert.Contains(t, deployment, "livenessProbe:")
		assert.Contains(t, deployment, "readinessProbe:")
		assert.Contains(t, deployment, "path: /livez", "livenessProbe must point to /livez")
		assert.Contains(t, deployment, "path: /readyz", "readinessProbe must point to /readyz")
	})

	t.Run("database url is stored in Secret, never in ConfigMap or inlined in Pod", func(t *testing.T) {
		assert.Contains(t, deployment, "secretKeyRef:")
		assert.Contains(t, deployment, "DATABASE_URL")
		assert.NotContains(t, deployment, "postgres://user:pass@host:5432/sorolens")
		assert.NotContains(t, cm, "DATABASE_URL")
		assert.Contains(t, manifest(t, standalone, "Secret"), "stringData:")
	})

	t.Run("standalone mode omits upstream settings", func(t *testing.T) {
		assert.Contains(t, cm, `SOURCE_MODE: "rpc"`)
		assert.NotContains(t, cm, "SOROTRAIL_URL",
			"standalone mode must omit SOROTRAIL_URL rather than setting it to an empty string")
	})

	t.Run("upstream mode omits database secret, DATABASE_URL, and standalone settings", func(t *testing.T) {
		upstream := renderChart(t, helm,
			"--values", filepath.Join(chartDir(), "testdata", "upstream.yaml"),
		)
		upDep := manifest(t, upstream, "Deployment")
		upCM := manifest(t, upstream, "ConfigMap")

		assert.NotContains(t, upstream, "kind: Secret",
			"upstream mode needs no database Secret")
		assert.NotContains(t, upDep, "DATABASE_URL",
			"upstream mode must not mount DATABASE_URL")
		assert.Contains(t, upCM, `SOURCE_MODE: "sorotrail"`)
		assert.Contains(t, upCM, `SOROTRAIL_URL: "http://sorotrail.sorotrail.svc.cluster.local:8080"`)
		for _, standaloneVar := range []string{
			"RPC_URL", "NETWORK", "NETWORK_PASSPHRASE",
			"POLL_INTERVAL", "WATCHED_CONTRACTS", "START_LEDGER", "RETENTION_LEDGERS",
		} {
			assert.NotContains(t, upCM, standaloneVar+":",
				"upstream mode must omit standalone variable %s", standaloneVar)
		}
	})

	t.Run("existingSecret, ServiceMonitor, and PDB render when enabled", func(t *testing.T) {
		overridden := renderChart(t, helm,
			"--values", filepath.Join(chartDir(), "testdata", "overrides.yaml"),
		)
		assert.NotContains(t, overridden, "kind: Secret",
			"existingSecret must suppress the chart-managed Secret")
		assert.Contains(t, manifest(t, overridden, "Deployment"), "existing-db-secret")
		assert.Contains(t, manifest(t, overridden, "ServiceMonitor"), "path: /metrics")
		assert.Contains(t, manifest(t, overridden, "PodDisruptionBudget"), "minAvailable: 1")
	})
}

// TestRender_ConfigValuesReachThePod guards against configuration drift between
// internal/config and deploy/helm/sorolens: every environment variable read by
// internal/config must be settable through the chart, and every config.* key in
// values.yaml must be wired into templates/configmap.yaml.
func TestRender_ConfigValuesReachThePod(t *testing.T) {
	configMapTemplate := readChartFile(t, "templates", "configmap.yaml")
	secretTemplate := readChartFile(t, "templates", "secret.yaml")
	values := readChartFile(t, "values.yaml")

	var valuesDoc struct {
		Config map[string]any `yaml:"config"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(values), &valuesDoc))
	require.NotEmpty(t, valuesDoc.Config)

	t.Run("every config value in values.yaml is wired into ConfigMap", func(t *testing.T) {
		for key := range valuesDoc.Config {
			assert.Contains(t, configMapTemplate, ".Values.config."+key,
				"values.yaml declares config.%s, but templates/configmap.yaml never reads it", key)
		}
	})

	t.Run("every environment variable internal/config reads is settable via the chart", func(t *testing.T) {
		appVars := applicationEnvVars(t)
		chartVars := map[string]bool{}
		for _, name := range templateEnvVars(configMapTemplate) {
			chartVars[name] = true
		}
		for _, name := range templateEnvVars(secretTemplate) {
			chartVars[name] = true
		}

		for envName := range appVars {
			assert.True(t, chartVars[envName],
				"internal/config reads %s, but deploy/helm/sorolens does not wire it", envName)
		}
		for envName := range chartVars {
			assert.True(t, appVars[envName],
				"deploy/helm/sorolens renders %s, which internal/config does not read", envName)
		}
	})
}

var (
	templateEnvLine = regexp.MustCompile(`^\s+([A-Z][A-Z0-9_]+):`)
	getenvCallRegex = regexp.MustCompile(`\b(?:os\.Getenv|getenv)\("([A-Z][A-Z0-9_]+)"`)
)

func applicationEnvVars(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	var collectTags func(reflect.Type)
	collectTags = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if name := f.Tag.Get("env"); name != "" {
				out[name] = true
			}
			if f.Type.Kind() == reflect.Struct {
				collectTags(f.Type)
			}
		}
	}
	collectTags(reflect.TypeOf(config.Config{}))

	// Also scan internal/config/*.go for direct Getenv/getenv calls so a new
	// variable read without a struct tag is still caught immediately.
	cfgDir := filepath.Join(chartDir(), "..", "..", "..", "internal", "config")
	entries, err := os.ReadDir(cfgDir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(cfgDir, entry.Name()))
		require.NoError(t, err)
		for _, m := range getenvCallRegex.FindAllStringSubmatch(string(src), -1) {
			out[m[1]] = true
		}
	}

	require.NotEmpty(t, out)
	return out
}

func templateEnvVars(tmpl string) []string {
	var names []string
	for _, line := range strings.Split(tmpl, "\n") {
		if m := templateEnvLine.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

func readChartFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{chartDir()}, parts...)...)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func manifest(t *testing.T, rendered, kind string) string {
	t.Helper()
	for _, block := range strings.Split(rendered, "\n---\n") {
		if strings.Contains(block, "kind: "+kind) {
			return block
		}
	}
	t.Fatalf("rendered output contains no %s manifest", kind)
	return ""
}
