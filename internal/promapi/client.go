// Package promapi is a small client for the Prometheus HTTP query API, which
// VictoriaMetrics (vmselect) and Prometheus both implement under /api/v1.
package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/prometheus/common/model"
	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

// Client queries one metrics datasource.
type Client struct{ ds *datasources.Datasource }

// New returns a client for the given datasource.
func New(ds *datasources.Datasource) *Client { return &Client{ds: ds} }

type apiEnvelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
}

// get issues a GET and returns the decoded envelope data, enforcing status=success.
func (c *Client) get(ctx context.Context, path string, q url.Values) (json.RawMessage, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ds.URL(path, q), nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.ds.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, err
	}
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	if env.Status != "success" {
		return nil, nil, fmt.Errorf("query API error (%s): %s", env.ErrorType, env.Error)
	}
	return env.Data, env.Warnings, nil
}

// Query runs an instant query at an optional time (RFC3339 / unix / "" = now).
func (c *Client) Query(ctx context.Context, expr, atTime string) (*QueryResult, error) {
	q := url.Values{"query": {expr}}
	if atTime != "" {
		q.Set("time", atTime)
	}
	data, warns, err := c.get(ctx, "/api/v1/query", q)
	if err != nil {
		return nil, err
	}
	return decodeResult(data, warns)
}

// QueryRange runs a range query.
func (c *Client) QueryRange(ctx context.Context, expr, start, end, step string) (*QueryResult, error) {
	q := url.Values{"query": {expr}, "start": {start}, "end": {end}, "step": {step}}
	data, warns, err := c.get(ctx, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	return decodeResult(data, warns)
}

// Series lists series matching the given selectors.
func (c *Client) Series(ctx context.Context, matches []string, start, end string) ([]map[string]string, error) {
	q := url.Values{"match[]": matches}
	setIf(q, "start", start)
	setIf(q, "end", end)
	data, _, err := c.get(ctx, "/api/v1/series", q)
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	return out, json.Unmarshal(data, &out)
}

// Labels lists label names, optionally constrained by selectors and time range.
func (c *Client) Labels(ctx context.Context, matches []string, start, end string) ([]string, error) {
	q := url.Values{}
	if len(matches) > 0 {
		q["match[]"] = matches
	}
	setIf(q, "start", start)
	setIf(q, "end", end)
	data, _, err := c.get(ctx, "/api/v1/labels", q)
	if err != nil {
		return nil, err
	}
	var out []string
	return out, json.Unmarshal(data, &out)
}

// LabelValues lists values for a label name.
func (c *Client) LabelValues(ctx context.Context, label string, matches []string, start, end string) ([]string, error) {
	q := url.Values{}
	if len(matches) > 0 {
		q["match[]"] = matches
	}
	setIf(q, "start", start)
	setIf(q, "end", end)
	data, _, err := c.get(ctx, "/api/v1/label/"+url.PathEscape(label)+"/values", q)
	if err != nil {
		return nil, err
	}
	var out []string
	return out, json.Unmarshal(data, &out)
}

// Metadata returns metric metadata, optionally for a single metric.
func (c *Client) Metadata(ctx context.Context, metric string, limit int) (map[string][]MetadataEntry, error) {
	q := url.Values{}
	setIf(q, "metric", metric)
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	data, _, err := c.get(ctx, "/api/v1/metadata", q)
	if err != nil {
		return nil, err
	}
	var out map[string][]MetadataEntry
	return out, json.Unmarshal(data, &out)
}

// Rules returns alerting/recording rule groups. ruleType is "alert", "record", or "".
func (c *Client) Rules(ctx context.Context, ruleType string) ([]RuleGroup, error) {
	q := url.Values{}
	setIf(q, "type", ruleType)
	data, _, err := c.get(ctx, "/api/v1/rules", q)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Groups []RuleGroup `json:"groups"`
	}
	return wrap.Groups, json.Unmarshal(data, &wrap)
}

// ActiveAlerts returns the currently active alerts.
func (c *Client) ActiveAlerts(ctx context.Context) ([]Alert, error) {
	data, _, err := c.get(ctx, "/api/v1/alerts", nil)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Alerts []Alert `json:"alerts"`
	}
	return wrap.Alerts, json.Unmarshal(data, &wrap)
}

// Targets returns scrape targets. state is "active", "dropped", "any", or "".
func (c *Client) Targets(ctx context.Context, state string) ([]Target, error) {
	q := url.Values{}
	setIf(q, "state", state)
	data, _, err := c.get(ctx, "/api/v1/targets", q)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		ActiveTargets []Target `json:"activeTargets"`
	}
	return wrap.ActiveTargets, json.Unmarshal(data, &wrap)
}

// decodeResult decodes the {resultType,result} envelope into a typed QueryResult.
func decodeResult(data json.RawMessage, warns []string) (*QueryResult, error) {
	var head struct {
		ResultType model.ValueType `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	res := &QueryResult{Type: head.ResultType, Warnings: warns}
	switch head.ResultType {
	case model.ValVector:
		if err := json.Unmarshal(head.Result, &res.Vector); err != nil {
			return nil, err
		}
	case model.ValMatrix:
		if err := json.Unmarshal(head.Result, &res.Matrix); err != nil {
			return nil, err
		}
	case model.ValScalar:
		var s model.Scalar
		if err := json.Unmarshal(head.Result, &s); err != nil {
			return nil, err
		}
		res.Scalar = &s
	case model.ValString:
		var s model.String
		if err := json.Unmarshal(head.Result, &s); err != nil {
			return nil, err
		}
		res.String = &s
	default:
		return nil, fmt.Errorf("unsupported result type %q", head.ResultType)
	}
	return res, nil
}

func setIf(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func snippet(b []byte) string {
	const max = 300
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
