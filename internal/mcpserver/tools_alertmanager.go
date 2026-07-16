package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/truepace-io-oss/observability-mcp-server/internal/alertmanager"
)

func (s *Server) registerAlertmanagerTools(m *mcp.Server) {
	// Read tools.
	addTool(m, s, "alertmanager_alerts", "List alerts from an Alertmanager datasource, filtered by state and label matchers.", s.amAlerts)
	addTool(m, s, "alertmanager_alert_groups", "List alerts grouped by their group labels.", s.amAlertGroups)
	addTool(m, s, "alertmanager_silences_list", "List silences, optionally filtered by matchers.", s.amSilencesList)
	addTool(m, s, "alertmanager_silence_get", "Get a single silence by ID.", s.amSilenceGet)
	addTool(m, s, "alertmanager_status", "Get Alertmanager status and configuration.", s.amStatus)
	addTool(m, s, "alertmanager_receivers", "List configured Alertmanager receivers.", s.amReceivers)
	// Write tools (gated by the read-only guard).
	addTool(m, s, "alertmanager_silence_create", "Create (or update) a silence. Blocked when the instance or datasource is read-only.", s.amSilenceCreate)
	addTool(m, s, "alertmanager_silence_delete", "Delete (expire) a silence by ID. Blocked when the instance or datasource is read-only.", s.amSilenceDelete)
}

func (s *Server) amClient(name string) (*alertmanager.Client, error) {
	ds, err := s.resolveDatasource(name)
	if err != nil {
		return nil, err
	}
	if err := assertAlertmanager(ds); err != nil {
		return nil, err
	}
	return alertmanager.New(ds), nil
}

func (s *Server) amAlerts(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Active    *bool    `json:"active,omitempty" jsonschema:"include active alerts (default true)"`
	Silenced  *bool    `json:"silenced,omitempty" jsonschema:"include silenced alerts (default false)"`
	Inhibited *bool    `json:"inhibited,omitempty" jsonschema:"include inhibited alerts (default false)"`
	Filter    []string `json:"filter,omitempty" jsonschema:"label matchers, e.g. ['severity=\"critical\"','namespace=\"prod\"']"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	alerts, err := c.Alerts(ctx, boolOr(in.Active, true), boolOr(in.Silenced, false), boolOr(in.Inhibited, false), in.Filter)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatAMAlerts(alerts)), nil, nil
}

func (s *Server) amAlertGroups(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Filter []string `json:"filter,omitempty" jsonschema:"label matchers to filter groups"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	groups, err := c.AlertGroups(ctx, in.Filter)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatAMGroups(groups)), nil, nil
}

func (s *Server) amSilencesList(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Filter []string `json:"filter,omitempty" jsonschema:"label matchers to filter silences"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	silences, err := c.Silences(ctx, in.Filter)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatSilences(silences)), nil, nil
}

func (s *Server) amSilenceGet(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	ID string `json:"id" jsonschema:"the silence ID"`
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	sil, err := c.Silence(ctx, in.ID)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatSilence(sil)), nil, nil
}

func (s *Server) amStatus(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	st, err := c.Status(ctx)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatJSONMap("status", st)), nil, nil
}

func (s *Server) amReceivers(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
}) (*mcp.CallToolResult, any, error) {
	c, err := s.amClient(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	rs, err := c.Receivers(ctx)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(formatReceivers(rs)), nil, nil
}

func (s *Server) amSilenceCreate(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	Matchers  []silenceMatcher `json:"matchers" jsonschema:"label matchers this silence applies to"`
	Duration  string           `json:"duration,omitempty" jsonschema:"how long the silence lasts from now, e.g. '2h', '30m' (used when endsAt is empty; default 1h)"`
	StartsAt  string           `json:"startsAt,omitempty" jsonschema:"silence start (RFC3339); defaults to now"`
	EndsAt    string           `json:"endsAt,omitempty" jsonschema:"silence end (RFC3339); overrides duration when set"`
	CreatedBy string           `json:"createdBy" jsonschema:"who is creating the silence"`
	Comment   string           `json:"comment" jsonschema:"reason for the silence"`
}) (*mcp.CallToolResult, any, error) {
	ds, err := s.resolveDatasource(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	if err := assertAlertmanager(ds); err != nil {
		return errorResult(err), nil, nil
	}
	if err := s.assertWritable(ds); err != nil {
		return errorResult(err), nil, nil
	}
	if len(in.Matchers) == 0 {
		return errorResult(fmt.Errorf("at least one matcher is required")), nil, nil
	}

	now := time.Now().UTC()
	start := now
	if in.StartsAt != "" {
		if t, perr := time.Parse(time.RFC3339, in.StartsAt); perr == nil {
			start = t
		} else {
			return errorResult(fmt.Errorf("invalid startsAt: %w", perr)), nil, nil
		}
	}
	var end time.Time
	switch {
	case in.EndsAt != "":
		t, perr := time.Parse(time.RFC3339, in.EndsAt)
		if perr != nil {
			return errorResult(fmt.Errorf("invalid endsAt: %w", perr)), nil, nil
		}
		end = t
	default:
		dur := time.Hour
		if in.Duration != "" {
			d, perr := time.ParseDuration(in.Duration)
			if perr != nil {
				return errorResult(fmt.Errorf("invalid duration: %w", perr)), nil, nil
			}
			dur = d
		}
		end = start.Add(dur)
	}

	ms := make([]alertmanager.Matcher, 0, len(in.Matchers))
	for _, m := range in.Matchers {
		ms = append(ms, alertmanager.Matcher{Name: m.Name, Value: m.Value, IsRegex: m.IsRegex})
	}
	post := alertmanager.PostableSilence{
		Matchers:  ms,
		StartsAt:  start.Format(time.RFC3339),
		EndsAt:    end.Format(time.RFC3339),
		CreatedBy: in.CreatedBy,
		Comment:   in.Comment,
	}
	id, err := alertmanager.New(ds).CreateSilence(ctx, post)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(fmt.Sprintf("created silence %s (until %s)", id, post.EndsAt)), nil, nil
}

func (s *Server) amSilenceDelete(ctx context.Context, _ *mcp.CallToolRequest, in struct {
	datasourceParam
	ID string `json:"id" jsonschema:"the silence ID to expire"`
}) (*mcp.CallToolResult, any, error) {
	ds, err := s.resolveDatasource(in.Datasource)
	if err != nil {
		return errorResult(err), nil, nil
	}
	if err := assertAlertmanager(ds); err != nil {
		return errorResult(err), nil, nil
	}
	if err := s.assertWritable(ds); err != nil {
		return errorResult(err), nil, nil
	}
	if err := alertmanager.New(ds).DeleteSilence(ctx, in.ID); err != nil {
		return errorResult(err), nil, nil
	}
	return textResult(fmt.Sprintf("deleted silence %s", in.ID)), nil, nil
}

// silenceMatcher is the tool-facing matcher shape.
type silenceMatcher struct {
	Name    string `json:"name" jsonschema:"label name"`
	Value   string `json:"value" jsonschema:"label value (or regex when isRegex)"`
	IsRegex bool   `json:"isRegex,omitempty" jsonschema:"treat value as a regular expression"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
