package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
defaultDatasource: vm
datasources:
  - name: vm
    type: victoriametrics
    url: http://vm:8481
    pathPrefix: /select/0/prometheus
  - name: logs
    type: victorialogs
    url: http://logs:9471
  - name: am
    type: alertmanager
    url: http://am:9093
    readOnly: true
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Datasources) != 3 {
		t.Fatalf("want 3 datasources, got %d", len(cfg.Datasources))
	}
	if cfg.DefaultDatasource != "vm" {
		t.Fatalf("default = %q", cfg.DefaultDatasource)
	}
	if cfg.Datasources[0].Timeout != "30s" {
		t.Fatalf("default timeout not applied: %q", cfg.Datasources[0].Timeout)
	}
}

func TestSingleDatasourceDefaultInferred(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
datasources:
  - name: only
    type: prometheus
    url: http://p:9090
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultDatasource != "only" {
		t.Fatalf("default not inferred: %q", cfg.DefaultDatasource)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"no datasources": `defaultDatasource: x
datasources: []`,
		"unknown type": `datasources:
  - name: x
    type: mysql
    url: http://x`,
		"dup name": `defaultDatasource: x
datasources:
  - name: x
    type: prometheus
    url: http://a
  - name: x
    type: prometheus
    url: http://b`,
		"bad url scheme": `datasources:
  - name: x
    type: prometheus
    url: ftp://x`,
		"basic+bearer": `datasources:
  - name: x
    type: prometheus
    url: http://x
    auth:
      basic: {username: u, password: p}
      bearer: {token: t}`,
		"insecure+ca": `datasources:
  - name: x
    type: prometheus
    url: https://x
    tls: {insecureSkipTLSVerify: true, caFile: /ca}`,
		"missing default": `datasources:
  - name: a
    type: prometheus
    url: http://a
  - name: b
    type: prometheus
    url: http://b`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body)); err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("OMCP_READ_ONLY", "true")
	t.Setenv("OMCP_LOG_LEVEL", "debug")
	cfg, err := Load(writeTemp(t, `
datasources:
  - name: only
    type: prometheus
    url: http://p:9090
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReadOnly || cfg.LogLevel != "debug" {
		t.Fatalf("env overrides not applied: readOnly=%v log=%s", cfg.ReadOnly, cfg.LogLevel)
	}
}
