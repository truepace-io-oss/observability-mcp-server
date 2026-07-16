package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/truepace-io-oss/observability-mcp-server/internal/logsql"
)

func (s *Server) registerLogsTools(m *mcp.Server) {
	addTool(m, s, "logs_query", "Run a LogsQL query against a VictoriaLogs datasource and return matching log lines (capped).", s.logsQuery)
	addTool(m, s, "logs_hits", "Return per-bucket hit counts over time for a LogsQL query.", s.logsHits)
	addTool(m, s, "logs_stats", "Run a LogsQL stats query at an instant.", s.logsStats)
	addTool(m, s, "logs_field_names", "List field names present in the results of a LogsQL query.", s.logsFieldNames)
	addTool(m, s, "logs_field_values", "List the values of a field across the results of a LogsQL query.", s.logsFieldValues)
	addTool(m, s, "logs_streams", "List log streams matching a LogsQL query.", s.logsStreams)
}

func (s *Server) logsClient(name string) (*logsql.Client, error) {
	ds, err := s.resolveDatasource(name)
	if err != nil {
		return nil, err
	}
	if err := assertLogs(ds); err != nil {
		return nil, err
	}
	return logsql.New(ds), nil
}

func (s *Server) logsQuery(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Query string `json:"query" jsonschema:"the LogsQL query, e.g. '_time:5m error' or 'level:error | stats count()'"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of log lines to return (default 100, max 1000)"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	recs, err := c.Query(ctx, in.Query, in.Start, in.End, in.Limit)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatLogRecords(recs)), nil, nil
}

func (s *Server) logsHits(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Query string `json:"query" jsonschema:"the LogsQL query to count hits for"`
	Step  string `json:"step,omitempty" jsonschema:"bucket width, e.g. '1m', '5m', '1h'"`
	Field string `json:"field,omitempty" jsonschema:"optional field to group hits by"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	recs, err := c.Hits(ctx, in.Query, in.Start, in.End, in.Step, in.Field)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatLogRecords(recs)), nil, nil
}

func (s *Server) logsStats(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Query string `json:"query" jsonschema:"the LogsQL stats query, e.g. '* | stats by (level) count()'"`
	Time  string `json:"time,omitempty" jsonschema:"evaluation time: RFC3339 or unix seconds; defaults to now"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	res, err := c.StatsQuery(ctx, in.Query, in.Time)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatJSONMap("stats", res)), nil, nil
}

func (s *Server) logsFieldNames(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Query string `json:"query,omitempty" jsonschema:"optional LogsQL query to scope the field names (default '*')"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	names, err := c.FieldNames(ctx, in.Query, in.Start, in.End)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatStringList("field name(s)", names)), nil, nil
}

func (s *Server) logsFieldValues(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Field string `json:"field" jsonschema:"the field to list values for, e.g. 'level' or 'namespace'"`
	Query string `json:"query,omitempty" jsonschema:"optional LogsQL query to scope the values (default '*')"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of values to return"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	values, err := c.FieldValues(ctx, in.Field, in.Query, in.Start, in.End, in.Limit)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatStringList("value(s) for "+in.Field, values)), nil, nil
}

func (s *Server) logsStreams(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	timeRangeParam
	Query string `json:"query,omitempty" jsonschema:"optional LogsQL query to scope the streams (default '*')"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of streams to return"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.logsClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	streams, err := c.Streams(ctx, in.Query, in.Start, in.End, in.Limit)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatStringList("stream(s)", streams)), nil, nil
}
