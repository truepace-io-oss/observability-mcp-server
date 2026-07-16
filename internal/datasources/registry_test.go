package datasources

import (
	"testing"

	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
)

func TestBuildAndGet(t *testing.T) {
	cfg := &config.Config{
		DefaultDatasource: "vm",
		Datasources: []config.DatasourceConfig{
			{Name: "vm", Type: "victoriametrics", URL: "http://vm:8481", PathPrefix: "/select/0/prometheus", Timeout: "30s"},
			{Name: "logs", Type: "victorialogs", URL: "http://logs:9471", Timeout: "30s"},
		},
	}
	reg, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.DefaultName() != "vm" {
		t.Fatalf("default = %s", reg.DefaultName())
	}
	// Empty name resolves to default.
	ds, err := reg.Get("")
	if err != nil || ds.Name != "vm" {
		t.Fatalf("default get err=%v ds=%v", err, ds)
	}
	if ds.BaseURL != "http://vm:8481/select/0/prometheus" {
		t.Fatalf("baseURL = %s", ds.BaseURL)
	}
	if _, err := reg.Get("nope"); err == nil {
		t.Fatal("expected unknown-datasource error")
	}
	if got := reg.All(); len(got) != 2 || got[0].Name != "logs" {
		t.Fatalf("All() not sorted: %+v", got)
	}
}
