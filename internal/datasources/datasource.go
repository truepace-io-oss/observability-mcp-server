// Package datasources holds the ready-to-use HTTP clients for the observability
// backends this MCP instance can query, and a registry to look them up by name.
package datasources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
)

// Datasource is a single configured observability backend with a ready HTTP client.
type Datasource struct {
	Name     string
	Type     string
	ReadOnly bool
	BaseURL  string // url + pathPrefix, no trailing slash
	HTTP     *http.Client
}

// newDatasource builds a Datasource (and its HTTP client) from config. Building
// the client does not contact the backend, so an unreachable datasource does not
// fail construction; reachability is reported later via Ping.
func newDatasource(ds config.DatasourceConfig) (*Datasource, error) {
	client, err := buildHTTPClient(ds)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(ds.URL, "/") + strings.TrimRight(ds.PathPrefix, "/")
	base = strings.TrimRight(base, "/")
	return &Datasource{
		Name:     ds.Name,
		Type:     ds.Type,
		ReadOnly: ds.ReadOnly,
		BaseURL:  base,
		HTTP:     client,
	}, nil
}

// NewForTest builds a Datasource pointing at baseURL with a default HTTP client.
// It exists so tests can target httptest servers without a full config.
func NewForTest(name, dsType, baseURL string, readOnly bool) *Datasource {
	return &Datasource{
		Name:     name,
		Type:     dsType,
		ReadOnly: readOnly,
		BaseURL:  strings.TrimRight(baseURL, "/"),
		HTTP:     &http.Client{},
	}
}

// URL joins the datasource base URL with a path and optional query values.
func (d *Datasource) URL(path string, query url.Values) string {
	u := d.BaseURL + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// IsMetrics reports whether this datasource serves the Prometheus query API.
func (d *Datasource) IsMetrics() bool { return config.IsMetricsType(d.Type) }

// IsLogs reports whether this datasource is VictoriaLogs.
func (d *Datasource) IsLogs() bool { return d.Type == config.TypeVictoriaLogs }

// IsAlertmanager reports whether this datasource is Alertmanager.
func (d *Datasource) IsAlertmanager() bool { return d.Type == config.TypeAlertmanager }

// Ping checks reachability using a type-appropriate lightweight endpoint and
// returns a short status string. It must never be fatal at startup.
func (d *Datasource) Ping(ctx context.Context) (string, error) {
	var path string
	var query url.Values
	switch d.Type {
	case config.TypeVictoriaMetrics, config.TypePrometheus:
		path, query = "/api/v1/query", url.Values{"query": {"vector(1)"}}
	case config.TypeVictoriaLogs:
		path = "/health"
	case config.TypeAlertmanager:
		path = "/api/v2/status"
	default:
		return "", fmt.Errorf("unknown datasource type %q", d.Type)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL(path, query), nil)
	if err != nil {
		return "", err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s returned HTTP %d", path, resp.StatusCode)
	}
	return "reachable (" + d.Type + ")", nil
}
