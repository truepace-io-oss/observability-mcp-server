// Package logsql is a small client for the VictoriaLogs HTTP API (LogsQL),
// served under /select/logsql/* by vlselect (cluster) or a single-node instance.
package logsql

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

// Client queries one VictoriaLogs datasource.
type Client struct{ ds *datasources.Datasource }

// New returns a client for the given datasource.
func New(ds *datasources.Datasource) *Client { return &Client{ds: ds} }

// DefaultLimit / MaxLimit bound how many log lines a query may return, to protect
// the agent's context window.
const (
	DefaultLimit = 100
	MaxLimit     = 1000
)

// post issues a form POST to a LogsQL endpoint and returns the response body reader.
// The caller must close it.
func (c *Client) post(ctx context.Context, path string, form url.Values) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ds.URL(path, nil), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.ds.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("logs API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func timeForm(query, start, end string) url.Values {
	f := url.Values{"query": {query}}
	if start != "" {
		f.Set("start", start)
	}
	if end != "" {
		f.Set("end", end)
	}
	return f
}

// Query runs a LogsQL query and returns up to limit decoded log records (each a
// JSON object of fields). Results stream as NDJSON.
func (c *Client) Query(ctx context.Context, query, start, end string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	form := timeForm(query, start, end)
	form.Set("limit", fmt.Sprint(limit))
	body, err := c.post(ctx, "/select/logsql/query", form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return decodeNDJSON(body, limit)
}

// Hits returns per-bucket hit counts over time for a query.
func (c *Client) Hits(ctx context.Context, query, start, end, step, field string) ([]map[string]any, error) {
	form := timeForm(query, start, end)
	if step != "" {
		form.Set("step", step)
	}
	if field != "" {
		form.Set("field", field)
	}
	body, err := c.post(ctx, "/select/logsql/hits", form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return decodeSingleJSONAsList(body)
}

// StatsQuery runs a LogsQL stats query at an instant.
func (c *Client) StatsQuery(ctx context.Context, query, atTime string) (map[string]any, error) {
	form := url.Values{"query": {query}}
	if atTime != "" {
		form.Set("time", atTime)
	}
	body, err := c.post(ctx, "/select/logsql/stats_query", form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var out map[string]any
	return out, json.NewDecoder(body).Decode(&out)
}

// FieldNames lists field names present in the results of a query.
func (c *Client) FieldNames(ctx context.Context, query, start, end string) ([]string, error) {
	if query == "" {
		query = "*"
	}
	body, err := c.post(ctx, "/select/logsql/field_names", timeForm(query, start, end))
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return decodeValuesList(body)
}

// FieldValues lists values of a field across the results of a query.
func (c *Client) FieldValues(ctx context.Context, field, query, start, end string, limit int) ([]string, error) {
	if query == "" {
		query = "*"
	}
	form := timeForm(query, start, end)
	form.Set("field", field)
	if limit > 0 {
		form.Set("limit", fmt.Sprint(limit))
	}
	body, err := c.post(ctx, "/select/logsql/field_values", form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return decodeValuesList(body)
}

// Streams lists log streams matching a query.
func (c *Client) Streams(ctx context.Context, query, start, end string, limit int) ([]string, error) {
	if query == "" {
		query = "*"
	}
	form := timeForm(query, start, end)
	if limit > 0 {
		form.Set("limit", fmt.Sprint(limit))
	}
	body, err := c.post(ctx, "/select/logsql/streams", form)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return decodeValuesList(body)
}

// decodeNDJSON reads up to limit newline-delimited JSON objects.
func decodeNDJSON(r io.Reader, limit int) ([]map[string]any, error) {
	out := make([]map[string]any, 0, limit)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() && len(out) < limit {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue // skip malformed lines rather than fail the whole query
		}
		out = append(out, m)
	}
	return out, sc.Err()
}

// decodeValuesList decodes VictoriaLogs "values" endpoints, which return NDJSON
// objects like {"value":"x","hits":N} or {"<name>":"x"}; extract the first string.
func decodeValuesList(r io.Reader) ([]string, error) {
	recs, err := decodeNDJSON(r, MaxLimit)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(recs))
	for _, m := range recs {
		if v, ok := m["value"].(string); ok {
			out = append(out, v)
			continue
		}
		for _, v := range m {
			if s, ok := v.(string); ok {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

// decodeSingleJSONAsList decodes a single JSON object into a one-element list (used
// by /hits which returns one aggregate object).
func decodeSingleJSONAsList(r io.Reader) ([]map[string]any, error) {
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, err
	}
	return []map[string]any{m}, nil
}
