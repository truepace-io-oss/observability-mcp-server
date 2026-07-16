# Datasources — adding & configuring

A **datasource** is one observability backend the MCP can query. Datasources are declared under
`datasources:` in `config.yaml` (or, in Helm, the `datasources` value). Each has a `type`, a
`url`, and optional path prefix, TLS, auth, timeout, and a per-datasource `readOnly` flag.

```yaml
defaultDatasource: vm
datasources:
  - name: vm                    # DNS-label, unique
    type: victoriametrics       # victoriametrics | prometheus | victorialogs | alertmanager
    url: "http://host:port"
    pathPrefix: ""              # appended to url; e.g. /select/0/prometheus for vmselect cluster
    readOnly: false             # per-datasource write guard (matters for alertmanager)
    timeout: "30s"
    tls:
      insecureSkipTLSVerify: false
      caFile: "/etc/omcp/datasources/vm/ca.crt"   # or caData: <base64 PEM>
    auth:                       # optional; for a vmauth proxy or protected endpoints
      basic: { username: "u", passwordFile: "/etc/omcp/datasources/vm/password" }
      # or: bearer: { tokenFile: "/etc/omcp/datasources/vm/token" }
```

`auth.basic` and `auth.bearer` are mutually exclusive; omit both for unauthenticated access.
Prefer the `*File` forms — they are re-read per request, so ESO/projected rotation needs no restart.

## Per-type reference (with in-cluster examples for `prod-averion-tools`)

The VictoriaMetrics stack in `prod-averion-tools` runs in **cluster mode** in namespace
`victoria-metrics-k8s-stack`. Verified Service endpoints:

### `victoriametrics` — VictoriaMetrics (vmselect)
```yaml
- name: metrics
  type: victoriametrics
  url: "http://vmselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:8481"
  pathPrefix: "/select/0/prometheus"     # REQUIRED in cluster mode (accountID 0)
```
Endpoints: `/api/v1/query`, `/query_range`, `/series`, `/labels`, `/label/<n>/values`,
`/metadata`, `/rules`, `/alerts`, `/targets`. Tools: `metrics_*`, `rules_list`, `alerts_active`, `targets_list`.

### `prometheus` — Prometheus
```yaml
- name: prom
  type: prometheus
  url: "http://prometheus-operated.monitoring.svc:9090"
```
Same `/api/v1/*` surface and the same tools as `victoriametrics`.

### `victorialogs` — VictoriaLogs (vlselect)
```yaml
- name: logs
  type: victorialogs
  url: "http://vlselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9471"
```
Endpoints under `/select/logsql/*`. Tools: `logs_query`, `logs_hits`, `logs_stats`,
`logs_field_names`, `logs_field_values`, `logs_streams`. Queries are LogsQL; results are capped
(default 100, max 1000 lines) to protect the agent context.

### `alertmanager` — Alertmanager / vmalertmanager
```yaml
- name: alertmanager
  type: alertmanager
  url: "http://vmalertmanager-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9093"
  readOnly: true                # allow reading; block silence create/delete
```
Endpoints under `/api/v2/*`. Tools: `alertmanager_alerts`, `alertmanager_alert_groups`,
`alertmanager_silences_list`, `alertmanager_silence_get`, `alertmanager_status`,
`alertmanager_receivers`, and the write tools `alertmanager_silence_create` / `_delete`.

## With vs. without a `vmauth` proxy

Both are pure config; no code path differs.

- **Direct** (in-cluster, trusted network): point `url` at `vmselect`/`vlselect` and set `auth: {}`.
- **Via `vmauth`** (e.g. a shared, authenticated gateway): point `url` at the `vmauth` Service/host
  and add `auth.basic` (a VMUser username/password) or `auth.bearer`. `vmauth` routes to the right
  component internally, so `pathPrefix` is usually empty.

```yaml
- name: vm-via-vmauth
  type: victoriametrics
  url: "http://vmauth-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:8427"
  auth:
    basic: { username: "observability-mcp", passwordFile: "/etc/omcp/datasources/vm-via-vmauth/password" }
```

## Adding a **new datasource type** (developer guide)

1. **Client package** — add `internal/<type>/client.go` implementing the backend's HTTP API
   (take a `*datasources.Datasource`, use `ds.HTTP` and `ds.URL(path, query)`).
2. **Type constant + validation** — add the constant to `internal/config/config.go`, include it in
   `validTypes`, and (if it serves the Prometheus API) `IsMetricsType`.
3. **Reachability** — add a case to `Datasource.Ping` in `internal/datasources/datasource.go`.
4. **Capability helper** — add `Is<Type>()` on `Datasource` if tools need to gate on it.
5. **Tools** — add `internal/mcpserver/tools_<type>.go` with a `register<Type>Tools` function, call
   it from `Server.MCPServer()`, and add an `assert<Type>` guard in `params.go`.
6. **Formatting** — add compact formatters in `format.go`.
7. **Docs** — add a section here and a row to the README compatibility table.
8. **Tests** — a client test against `httptest`, and extend the e2e fake backend + assertions.
