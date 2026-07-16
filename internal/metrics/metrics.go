// Package metrics defines the Prometheus metrics for the observability MCP
// server and an outbound-HTTP RoundTripper that records every request the server
// makes to a datasource. Metrics are served on a separate, unauthenticated port
// (see main); labels are deliberately low-cardinality (never query text, series
// or user identities).
package metrics

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	toolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omcp_tool_calls_total",
		Help: "MCP tool calls by tool, target datasource and result (ok|error|forbidden|blocked).",
	}, []string{"tool", "datasource", "result"})

	toolDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "omcp_tool_call_duration_seconds",
		Help:    "MCP tool call latency by tool and datasource.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tool", "datasource"})

	authRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omcp_auth_requests_total",
		Help: "Agent authentication attempts by method (static|oidc|none) and result (allow|deny).",
	}, []string{"method", "result"})

	datasourceUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "omcp_datasource_up",
		Help: "Datasource reachability (1 = reachable, 0 = not), by datasource and type.",
	}, []string{"datasource", "type"})

	writesBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omcp_writes_blocked_total",
		Help: "Mutating tool calls blocked by the read-only guard, by datasource and reason.",
	}, []string{"datasource", "reason"})

	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "omcp_build_info",
		Help: "Build information; constant 1.",
	}, []string{"version", "goversion"})

	dsRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omcp_datasource_requests_total",
		Help: "Outbound HTTP requests to datasources by datasource, type and status code.",
	}, []string{"datasource", "type", "code"})

	dsLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "omcp_datasource_request_duration_seconds",
		Help:    "Outbound datasource request latency by datasource and type.",
		Buckets: prometheus.DefBuckets,
	}, []string{"datasource", "type"})
)

// RecordTool records a tool call outcome and latency.
func RecordTool(tool, datasource, result string, d time.Duration) {
	if datasource == "" {
		datasource = "-"
	}
	toolCalls.WithLabelValues(tool, datasource, result).Inc()
	toolDuration.WithLabelValues(tool, datasource).Observe(d.Seconds())
}

// RecordAuth records an agent-authentication outcome.
func RecordAuth(method, result string) { authRequests.WithLabelValues(method, result).Inc() }

// RecordWriteBlocked records a mutation blocked by the read-only guard.
func RecordWriteBlocked(datasource, reason string) {
	writesBlocked.WithLabelValues(datasource, reason).Inc()
}

// SetDatasourceUp sets the reachability gauge for a datasource.
func SetDatasourceUp(datasource, dsType string, up bool) {
	v := 0.0
	if up {
		v = 1
	}
	datasourceUp.WithLabelValues(datasource, dsType).Set(v)
}

// SetBuildInfo publishes the build-info gauge.
func SetBuildInfo(version string) {
	buildInfo.WithLabelValues(version, runtime.Version()).Set(1)
}

// roundTripper wraps a transport and records outbound datasource request metrics.
type roundTripper struct {
	next       http.RoundTripper
	datasource string
	dsType     string
}

// NewRoundTripper returns an http.RoundTripper that records
// omcp_datasource_requests_total and omcp_datasource_request_duration_seconds
// for every request, labeled by datasource and type.
func NewRoundTripper(next http.RoundTripper, datasource, dsType string) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &roundTripper{next: next, datasource: datasource, dsType: dsType}
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := rt.next.RoundTrip(req)
	dsLatency.WithLabelValues(rt.datasource, rt.dsType).Observe(time.Since(start).Seconds())
	code := "error"
	if err == nil && resp != nil {
		code = strconv.Itoa(resp.StatusCode)
	}
	dsRequests.WithLabelValues(rt.datasource, rt.dsType, code).Inc()
	return resp, err
}
