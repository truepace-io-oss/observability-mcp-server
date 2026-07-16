# Metrics

Prometheus self-metrics are served on a **separate, unauthenticated port** (default `:9091`, set
`metricsAddr: "off"` to disable). This port must never be behind the `/mcp` auth or the public
ingress — scrape it in-cluster only. Labels are deliberately low-cardinality (never query text,
series, or user identity).

## Exposed metrics

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `omcp_tool_calls_total` | counter | `tool, datasource, result` | Tool invocations. `result = ok\|error\|forbidden\|blocked`. |
| `omcp_tool_call_duration_seconds` | histogram | `tool, datasource` | Tool latency. |
| `omcp_auth_requests_total` | counter | `method, result` | Agent auth. `method = static\|oidc\|none`, `result = allow\|deny`. |
| `omcp_datasource_up` | gauge | `datasource, type` | Reachability (1/0) from the 30s background probe. |
| `omcp_writes_blocked_total` | counter | `datasource, reason` | Mutations blocked. `reason = global_readonly\|datasource_readonly`. |
| `omcp_build_info` | gauge=1 | `version, goversion` | Build info. |
| `omcp_datasource_requests_total` | counter | `datasource, type, code` | Outbound HTTP requests to backends **with status codes**. |
| `omcp_datasource_request_duration_seconds` | histogram | `datasource, type` | Outbound backend latency. |
| `go_*`, `process_*` | — | — | Standard Go runtime / process collectors. |

## Scraping (Helm)

Metrics are on by default. Enable the ServiceMonitor with `serviceMonitor.enabled: true` — the
chart adds a `metrics` container/Service port and a ServiceMonitor pointing at `port: metrics`,
`path: /metrics`. (With the VictoriaMetrics operator, the ServiceMonitor is converted to a
VMServiceScrape automatically.) Set `metrics.enabled: false` to drop the port and render
`metricsAddr: "off"`.

## Grafana dashboard

`deploy/helm/observability-mcp/dashboards/observability-mcp.json` ships with the chart; enable it
with `grafanaDashboard.enabled: true` (requires the Grafana dashboard sidecar; discovered via the
`grafana_dashboard: "1"` label). Panels: build info, datasource reachability, agent-auth outcomes,
tool calls by tool and by result, tool latency p95, outbound datasource requests by code, outbound
latency p95, and writes blocked. `increase(...[$__interval])` is used so low-frequency interactive
usage stays visible.

## Useful queries

```promql
# tool error rate
sum(rate(omcp_tool_calls_total{result!="ok"}[5m])) by (tool)
# agent auth denials
sum(rate(omcp_auth_requests_total{result="deny"}[5m]))
# a datasource is down
omcp_datasource_up == 0
# outbound 5xx per datasource
sum(rate(omcp_datasource_requests_total{code=~"5.."}[5m])) by (datasource)
# writes blocked by the read-only guard
sum(increase(omcp_writes_blocked_total[1h])) by (datasource, reason)
```
