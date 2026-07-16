package mcpserver

import (
	"fmt"

	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
	"github.com/truepace-io-oss/observability-mcp-server/internal/metrics"
)

// datasourceParam is embedded by every tool input to select the target datasource.
type datasourceParam struct {
	Datasource string `json:"datasource,omitempty" jsonschema:"the configured datasource to target; defaults to the server's default datasource when omitted"`
}

// metricDatasource reports the requested datasource for metric labels; promoted
// to every input struct that embeds datasourceParam.
func (d datasourceParam) metricDatasource() string { return d.Datasource }

// timeRangeParam is embedded by range-capable tools.
type timeRangeParam struct {
	Start string `json:"start,omitempty" jsonschema:"start time: RFC3339, unix seconds, or a relative offset like '1h'/'5m'; backend-dependent default when omitted"`
	End   string `json:"end,omitempty" jsonschema:"end time: RFC3339, unix seconds, or 'now'; defaults to now"`
}

// resolveDatasource picks the target datasource from the argument or the default.
func (s *Server) resolveDatasource(name string) (*datasources.Datasource, error) {
	return s.reg.Get(name)
}

// assertMetrics returns an error unless the datasource serves the Prometheus query API.
func assertMetrics(ds *datasources.Datasource) error {
	if !ds.IsMetrics() {
		return fmt.Errorf("datasource %q is type %q; this tool requires a metrics datasource (victoriametrics|prometheus)", ds.Name, ds.Type)
	}
	return nil
}

// assertLogs returns an error unless the datasource is VictoriaLogs.
func assertLogs(ds *datasources.Datasource) error {
	if !ds.IsLogs() {
		return fmt.Errorf("datasource %q is type %q; this tool requires a victorialogs datasource", ds.Name, ds.Type)
	}
	return nil
}

// assertAlertmanager returns an error unless the datasource is Alertmanager.
func assertAlertmanager(ds *datasources.Datasource) error {
	if !ds.IsAlertmanager() {
		return fmt.Errorf("datasource %q is type %q; this tool requires an alertmanager datasource", ds.Name, ds.Type)
	}
	return nil
}

// assertWritable blocks mutating operations when writes are disabled globally or
// for the target datasource.
func (s *Server) assertWritable(ds *datasources.Datasource) error {
	if s.readOnly {
		metrics.RecordWriteBlocked(ds.Name, "global_readonly")
		return fmt.Errorf("this MCP instance is configured read-only (writes disabled globally)")
	}
	if ds.ReadOnly {
		metrics.RecordWriteBlocked(ds.Name, "datasource_readonly")
		return fmt.Errorf("writes are disabled for datasource %q (readOnly)", ds.Name)
	}
	return nil
}
