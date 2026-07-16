package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/truepace-io-oss/observability-mcp-server/internal/promapi"
)

func (s *Server) registerMetricsTools(m *mcp.Server) {
	addTool(m, s, "metrics_query", "Run an instant PromQL/MetricsQL query against a metrics datasource (VictoriaMetrics or Prometheus).", s.metricsQuery)
	addTool(m, s, "metrics_query_range", "Run a range PromQL/MetricsQL query (start, end, step) against a metrics datasource.", s.metricsQueryRange)
	addTool(m, s, "metrics_series", "List time series matching one or more selectors (match[]).", s.metricsSeries)
	addTool(m, s, "metrics_labels", "List label names, optionally constrained by selectors and time range.", s.metricsLabels)
	addTool(m, s, "metrics_label_values", "List the values of a single label name.", s.metricsLabelValues)
	addTool(m, s, "metrics_metadata", "List metric metadata (type/help), optionally for a single metric.", s.metricsMetadata)
	addTool(m, s, "rules_list", "List alerting/recording rule groups and their state.", s.rulesList)
	addTool(m, s, "alerts_active", "List currently active alerts as seen by the metrics datasource (VM/Prometheus).", s.alertsActive)
	addTool(m, s, "targets_list", "List scrape targets and their health.", s.targetsList)
}

func (s *Server) metricsClient(name string) (*promapi.Client, error) {
	ds, err := s.resolveDatasource(name)
	if err != nil {
		return nil, err
	}
	if err := assertMetrics(ds); err != nil {
		return nil, err
	}
	return promapi.New(ds), nil
}

func (s *Server) metricsQuery(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Query string `json:"query" jsonschema:"the PromQL/MetricsQL expression to evaluate"`
	Time  string `json:"time,omitempty" jsonschema:"evaluation time: RFC3339 or unix seconds; defaults to now"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	r, err := c.Query(ctx, in.Query, in.Time)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatQueryResult(r)), nil, nil
}

func (s *Server) metricsQueryRange(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Query string `json:"query" jsonschema:"the PromQL/MetricsQL expression to evaluate"`
	Start string `json:"start" jsonschema:"range start: RFC3339 or unix seconds"`
	End   string `json:"end" jsonschema:"range end: RFC3339 or unix seconds"`
	Step  string `json:"step" jsonschema:"resolution step, e.g. '30s', '1m', '5m'"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	r, err := c.QueryRange(ctx, in.Query, in.Start, in.End, in.Step)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatQueryResult(r)), nil, nil
}

func (s *Server) metricsSeries(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Match []string `json:"match" jsonschema:"one or more series selectors, e.g. ['up','process_cpu_seconds_total{job=\"x\"}']"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	series, err := c.Series(ctx, in.Match, in.Start, in.End)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatSeries(series)), nil, nil
}

func (s *Server) metricsLabels(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Match []string `json:"match,omitempty" jsonschema:"optional series selectors to constrain the label names"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	labels, err := c.Labels(ctx, in.Match, in.Start, in.End)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatStringList("label name(s)", labels)), nil, nil
}

func (s *Server) metricsLabelValues(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Label string   `json:"label" jsonschema:"the label name to list values for, e.g. 'job' or 'instance'"`
	Match []string `json:"match,omitempty" jsonschema:"optional series selectors to constrain the values"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	values, err := c.LabelValues(ctx, in.Label, in.Match, in.Start, in.End)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatStringList("value(s) for "+in.Label, values)), nil, nil
}

func (s *Server) metricsMetadata(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Metric string `json:"metric,omitempty" jsonschema:"optional metric name to fetch metadata for; empty returns all"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of metrics to return"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	md, err := c.Metadata(ctx, in.Metric, in.Limit)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatMetadata(md)), nil, nil
}

func (s *Server) rulesList(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Type string `json:"type,omitempty" jsonschema:"filter by rule type: 'alert' or 'record'; empty returns both"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	groups, err := c.Rules(ctx, in.Type)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatRules(groups)), nil, nil
}

func (s *Server) alertsActive(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	alerts, err := c.ActiveAlerts(ctx)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatPromAlerts(alerts)), nil, nil
}

func (s *Server) targetsList(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	State string `json:"state,omitempty" jsonschema:"filter by state: 'active', 'dropped', or 'any'"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.metricsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	targets, err := c.Targets(ctx, in.State)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatTargets(targets)), nil, nil
}
