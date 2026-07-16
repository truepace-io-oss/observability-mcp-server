// Package alertmanager is a small client for the Alertmanager v2 REST API
// (also implemented by VictoriaMetrics' vmalertmanager).
package alertmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

// Client talks to one Alertmanager datasource.
type Client struct{ ds *datasources.Datasource }

// New returns a client for the given datasource.
func New(ds *datasources.Datasource) *Client { return &Client{ds: ds} }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.ds.URL(path, q), rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.ds.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("alertmanager HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Alerts returns alerts, filtered by state flags and label matchers (filter[]).
func (c *Client) Alerts(ctx context.Context, active, silenced, inhibited bool, filter []string) ([]GettableAlert, error) {
	q := url.Values{
		"active":    {fmt.Sprint(active)},
		"silenced":  {fmt.Sprint(silenced)},
		"inhibited": {fmt.Sprint(inhibited)},
	}
	if len(filter) > 0 {
		q["filter"] = filter
	}
	var out []GettableAlert
	return out, c.do(ctx, http.MethodGet, "/api/v2/alerts", q, nil, &out)
}

// AlertGroups returns alerts grouped by their group labels.
func (c *Client) AlertGroups(ctx context.Context, filter []string) ([]AlertGroup, error) {
	q := url.Values{}
	if len(filter) > 0 {
		q["filter"] = filter
	}
	var out []AlertGroup
	return out, c.do(ctx, http.MethodGet, "/api/v2/alerts/groups", q, nil, &out)
}

// Silences lists silences, optionally filtered by matchers.
func (c *Client) Silences(ctx context.Context, filter []string) ([]GettableSilence, error) {
	q := url.Values{}
	if len(filter) > 0 {
		q["filter"] = filter
	}
	var out []GettableSilence
	return out, c.do(ctx, http.MethodGet, "/api/v2/silences", q, nil, &out)
}

// Silence returns a single silence by ID.
func (c *Client) Silence(ctx context.Context, id string) (*GettableSilence, error) {
	var out GettableSilence
	return &out, c.do(ctx, http.MethodGet, "/api/v2/silence/"+url.PathEscape(id), nil, nil, &out)
}

// Status returns Alertmanager status/config.
func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	return out, c.do(ctx, http.MethodGet, "/api/v2/status", nil, nil, &out)
}

// Receivers lists configured receivers.
func (c *Client) Receivers(ctx context.Context) ([]Receiver, error) {
	var out []Receiver
	return out, c.do(ctx, http.MethodGet, "/api/v2/receivers", nil, nil, &out)
}

// CreateSilence creates (or updates) a silence and returns its ID.
func (c *Client) CreateSilence(ctx context.Context, s PostableSilence) (string, error) {
	var out struct {
		SilenceID string `json:"silenceID"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v2/silences", nil, s, &out); err != nil {
		return "", err
	}
	return out.SilenceID, nil
}

// DeleteSilence expires (deletes) a silence by ID.
func (c *Client) DeleteSilence(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v2/silence/"+url.PathEscape(id), nil, nil, nil)
}
