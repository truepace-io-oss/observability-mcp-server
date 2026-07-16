package mcpserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/common/model"
	"github.com/truepace-io-oss/observability-mcp-server/internal/alertmanager"
	"github.com/truepace-io-oss/observability-mcp-server/internal/promapi"
)

// maxRows caps how many rows any list-style result prints, to protect the agent
// context window. A trailing note is added when truncated.
const maxRows = 200

// textResult wraps a plain string into an MCP tool result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// errorResult wraps an error as an MCP tool error (IsError), so the model sees
// the backend message instead of a transport failure.
func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

func truncNote(b *strings.Builder, shown, total int) {
	if total > shown {
		fmt.Fprintf(b, "… (%d of %d shown)\n", shown, total)
	}
}

// formatQueryResult renders an instant/range query result compactly.
func formatQueryResult(r *promapi.QueryResult) string {
	var b strings.Builder
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	switch r.Type {
	case model.ValVector:
		fmt.Fprintf(&b, "%d series (instant):\n", len(r.Vector))
		n := len(r.Vector)
		if n > maxRows {
			n = maxRows
		}
		for _, s := range r.Vector[:n] {
			fmt.Fprintf(&b, "- %s => %s @%d\n", metricLabels(s.Metric), s.Value, int64(s.Timestamp/1000))
		}
		truncNote(&b, n, len(r.Vector))
	case model.ValMatrix:
		fmt.Fprintf(&b, "%d series (range):\n", len(r.Matrix))
		n := len(r.Matrix)
		if n > maxRows {
			n = maxRows
		}
		for _, s := range r.Matrix[:n] {
			last := ""
			if len(s.Values) > 0 {
				lp := s.Values[len(s.Values)-1]
				last = fmt.Sprintf("last=%s @%d", lp.Value, int64(lp.Timestamp/1000))
			}
			fmt.Fprintf(&b, "- %s (%d points) %s\n", metricLabels(s.Metric), len(s.Values), last)
		}
		truncNote(&b, n, len(r.Matrix))
	case model.ValScalar:
		fmt.Fprintf(&b, "scalar: %s @%d\n", r.Scalar.Value, int64(r.Scalar.Timestamp/1000))
	case model.ValString:
		fmt.Fprintf(&b, "string: %s\n", r.String.Value)
	default:
		fmt.Fprintf(&b, "(unsupported result type %q)\n", r.Type)
	}
	return b.String()
}

// metricLabels renders a metric's labels as a stable {k="v",...} string.
func metricLabels(m model.Metric) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, string(m[model.LabelName(k)])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// labelSet renders a plain map of labels as {k="v",...}.
func labelSet(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, m[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatStringList(header string, items []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s:\n", len(items), header)
	n := len(items)
	if n > maxRows {
		n = maxRows
	}
	for _, it := range items[:n] {
		fmt.Fprintf(&b, "- %s\n", it)
	}
	truncNote(&b, n, len(items))
	return b.String()
}

func formatSeries(series []map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d series:\n", len(series))
	n := len(series)
	if n > maxRows {
		n = maxRows
	}
	for _, s := range series[:n] {
		fmt.Fprintf(&b, "- %s\n", labelSet(s))
	}
	truncNote(&b, n, len(series))
	return b.String()
}

func formatMetadata(md map[string][]promapi.MetadataEntry) string {
	names := make([]string, 0, len(md))
	for k := range md {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "%d metrics with metadata:\n", len(names))
	n := len(names)
	if n > maxRows {
		n = maxRows
	}
	for _, name := range names[:n] {
		for _, e := range md[name] {
			fmt.Fprintf(&b, "- %s [%s] %s\n", name, e.Type, e.Help)
		}
	}
	truncNote(&b, n, len(names))
	return b.String()
}

func formatRules(groups []promapi.RuleGroup) string {
	var b strings.Builder
	total := 0
	for _, g := range groups {
		total += len(g.Rules)
	}
	fmt.Fprintf(&b, "%d rule group(s), %d rule(s):\n", len(groups), total)
	shown := 0
	for _, g := range groups {
		fmt.Fprintf(&b, "group %s (%s):\n", g.Name, g.File)
		for _, r := range g.Rules {
			if shown >= maxRows {
				fmt.Fprintf(&b, "… (truncated at %d rules)\n", maxRows)
				return b.String()
			}
			state := r.State
			if state == "" {
				state = r.Health
			}
			fmt.Fprintf(&b, "  - [%s] %s %s: %s\n", r.Type, r.Name, state, r.Query)
			shown++
		}
	}
	return b.String()
}

func formatPromAlerts(alerts []promapi.Alert) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d active alert(s):\n", len(alerts))
	n := len(alerts)
	if n > maxRows {
		n = maxRows
	}
	for _, a := range alerts[:n] {
		name := a.Labels["alertname"]
		fmt.Fprintf(&b, "- [%s] %s %s (since %s)\n", a.State, name, labelSet(a.Labels), a.ActiveAt)
	}
	truncNote(&b, n, len(alerts))
	return b.String()
}

func formatTargets(targets []promapi.Target) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d target(s):\n", len(targets))
	n := len(targets)
	if n > maxRows {
		n = maxRows
	}
	for _, t := range targets[:n] {
		msg := ""
		if t.LastError != "" {
			msg = " err=" + t.LastError
		}
		fmt.Fprintf(&b, "- [%s] %s %s%s\n", t.Health, t.ScrapePool, t.ScrapeURL, msg)
	}
	truncNote(&b, n, len(targets))
	return b.String()
}

// formatLogRecords renders NDJSON log records with common fields promoted.
func formatLogRecords(recs []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d log line(s):\n", len(recs))
	for _, r := range recs {
		ts := firstString(r, "_time", "time", "timestamp")
		msg := firstString(r, "_msg", "message", "msg")
		if msg == "" {
			msg = compactJSON(r)
		}
		fmt.Fprintf(&b, "- %s %s\n", ts, msg)
	}
	return b.String()
}

func formatJSONMap(header string, m map[string]any) string {
	return header + ":\n" + compactJSON(m) + "\n"
}

func formatAMAlerts(alerts []alertmanager.GettableAlert) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d alert(s):\n", len(alerts))
	n := len(alerts)
	if n > maxRows {
		n = maxRows
	}
	for _, a := range alerts[:n] {
		name := a.Labels["alertname"]
		fmt.Fprintf(&b, "- [%s] %s %s\n", a.Status.State, name, labelSet(a.Labels))
	}
	truncNote(&b, n, len(alerts))
	return b.String()
}

func formatAMGroups(groups []alertmanager.AlertGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d group(s):\n", len(groups))
	for _, g := range groups {
		fmt.Fprintf(&b, "- %s → receiver %s (%d alerts)\n", labelSet(g.Labels), g.Receiver.Name, len(g.Alerts))
	}
	return b.String()
}

func formatSilences(silences []alertmanager.GettableSilence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d silence(s):\n", len(silences))
	n := len(silences)
	if n > maxRows {
		n = maxRows
	}
	for _, s := range silences[:n] {
		fmt.Fprintf(&b, "- %s [%s] by %s until %s: %s\n", s.ID, s.Status.State, s.CreatedBy, s.EndsAt, s.Comment)
	}
	truncNote(&b, n, len(silences))
	return b.String()
}

func formatSilence(s *alertmanager.GettableSilence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "silence %s [%s]\n", s.ID, s.Status.State)
	fmt.Fprintf(&b, "  by: %s\n  from: %s\n  to: %s\n  comment: %s\n", s.CreatedBy, s.StartsAt, s.EndsAt, s.Comment)
	for _, m := range s.Matchers {
		op := "="
		if m.IsRegex {
			op = "=~"
		}
		fmt.Fprintf(&b, "  match: %s%s%q\n", m.Name, op, m.Value)
	}
	return b.String()
}

func formatReceivers(rs []alertmanager.Receiver) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d receiver(s):\n", len(rs))
	for _, r := range rs {
		fmt.Fprintf(&b, "- %s\n", r.Name)
	}
	return b.String()
}

// compactJSON marshals a value to a single-line JSON string (HTML escaping off).
func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(b)
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
