# observability-mcp-server — Implementation Plan

> **Status of this document:** planning artifact. No production code has been written yet.
> Each step below is self-contained enough for an AI code assistant to execute one at a
> time. After completing a step, append a detailed entry to `observability-mcp-process.md`
> (same directory) so work can be paused and resumed. **Do not skip the process log.**

---

## 0. Context & Goal

Build an **MCP (Model Context Protocol) server** — `observability-mcp-server` — that lets an
AI agent (Claude Code / Cursor) query observability backends:

- **VictoriaMetrics** (single-node or cluster `vmselect`), directly **or via a `vmauth` proxy**
- **VictoriaLogs** (`vlselect` / single), directly **or via `vmauth`**
- **Prometheus** (query API, Prometheus-compatible)
- **Alertmanager** (v2 API: alerts, silences, status)

It is modeled **1:1 on the existing `kubernetes-mcp` blueprint** in this workspace
(`../kubernetes-mcp`). We reuse its architecture, agent-auth (static bearer + Authentik OIDC),
Prometheus self-metrics, Grafana dashboard, Helm chart, CI/CD, renovate config, docs style, and
`environments` GitOps integration. The **only substantive domain change** is: the
"cluster registry" (Kubernetes clients) becomes a "**datasource registry**" (HTTP clients to
observability backends).

### Blueprint reference paths (READ-ONLY — do not modify)
```
../kubernetes-mcp/                      # the Go MCP server blueprint
../environments/                        # GitOps repo (myks + ytt + Helm + Argo CD)
../environments/prototypes/kubernetes-mcp/
../environments/prototypes/authentik/ytt/kubernetes-mcp-blueprint-secret.yaml
../environments/envs/prod-averion-tools/
../environments/docs/kubernetes-mcp.md
```

### Naming decisions (locked)
| Concept | kubernetes-mcp value | observability-mcp-server value |
|---|---|---|
| Git repo / Go module | `github.com/truepace-io-oss/kubernetes-mcp-server` | `github.com/truepace-io-oss/observability-mcp-server` |
| Container image | `ghcr.io/truepace-io-oss/kubernetes-mcp-server` | `ghcr.io/truepace-io-oss/observability-mcp-server` |
| Binary name | `kubernetes-mcp` | `observability-mcp` |
| Deployment identity / chart name / namespace / authentik slug / ingress host prefix | `kubernetes-mcp` | `observability-mcp` |
| OCI Helm chart | `oci://ghcr.io/truepace-io-oss/charts/kubernetes-mcp` | `oci://ghcr.io/truepace-io-oss/charts/observability-mcp` |
| Env var prefix | `KMCP_` | `OMCP_` |
| Config mount path | `/etc/kmcp/config.yaml` | `/etc/omcp/config.yaml` |
| Metric name prefix | `kmcp_` | `omcp_` |
| Ingress host (prod) | `kubernetes-mcp.tools.averion.zone` | `observability-mcp.tools.averion.zone` |

### Ports (unchanged from blueprint)
- `9090` — MCP streamable-HTTP endpoint (`/mcp`), `/healthz`, `/readyz`, OIDC metadata
- `9091` — Prometheus self-metrics (`/metrics`), separate unauthenticated port

---

## 1. Domain model: datasources instead of clusters

A **Datasource** is an HTTP endpoint with a **type**, an optional **auth** (for `vmauth` /
protected endpoints), optional **TLS** settings, and an optional **path prefix**
(for VM cluster mode, e.g. `/select/0/prometheus`).

### Datasource types & the tools each enables
| Type (`type:`) | Backend | Query language | Tools enabled |
|---|---|---|---|
| `victoriametrics` | VictoriaMetrics (single or `vmselect`) | MetricsQL / PromQL | `metrics_query`, `metrics_query_range`, `metrics_series`, `metrics_labels`, `metrics_label_values`, `metrics_metadata`, `rules_list`, `alerts_active`, `targets_list` |
| `prometheus` | Prometheus | PromQL | same metrics tools as above (Prometheus-compatible `/api/v1/*`) |
| `victorialogs` | VictoriaLogs | LogsQL | `logs_query`, `logs_stats`, `logs_field_names`, `logs_field_values`, `logs_streams`, `logs_hits` |
| `alertmanager` | Alertmanager | — (v2 REST) | `alertmanager_alerts`, `alertmanager_alert_groups`, `alertmanager_silences_list`, `alertmanager_silence_get`, `alertmanager_status`, `alertmanager_receivers`, **(write)** `alertmanager_silence_create`, `alertmanager_silence_delete` |

Always available: `datasources_list` (list configured datasources, their type, and reachability).

### "With and without vmauth proxy"
This is handled purely by config — no separate code path:
- **Direct:** `url: http://vmselect.vm.svc:8481`, `pathPrefix: /select/0/prometheus`, `auth: none`.
- **Via vmauth:** `url: https://vmauth.tools.averion.zone`, `pathPrefix: ""` (vmauth routes), `auth.basic` or `auth.bearer` set.

### Datasource config schema (rendered into `config.yaml`)
```yaml
listenAddr: "0.0.0.0:9090"
metricsAddr: ":9091"
logLevel: "info"
readOnly: false                 # global kill-switch; blocks alertmanager silence create/delete
defaultDatasource: "vm"
datasources:
  # NOTE: all URLs below are VERIFIED against the live prod-averion-tools cluster (see process log
  # 2026-07-16). The VM stack runs in CLUSTER mode (vmselect/vlselect), namespace
  # victoria-metrics-k8s-stack. vmselect/vlselect/vmalertmanager are headless Services (clusterIP
  # None) — HTTP clients resolve them via DNS to ready pod IPs; this is fine.
  - name: "vm"                  # DNS-label, unique
    type: "victoriametrics"     # victoriametrics | prometheus | victorialogs | alertmanager
    url: "http://vmselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:8481"
    pathPrefix: "/select/0/prometheus"  # REQUIRED for vmselect cluster mode (accountID 0 = default tenant)
    readOnly: false             # per-datasource write guard (only meaningful for alertmanager)
    timeout: "30s"              # per-request timeout
    tls:
      insecureSkipTLSVerify: false
      caFile: ""                # PEM CA path (ESO-mounted)
      caData: ""                # base64 PEM (discouraged; file preferred)
    auth:                       # optional; for vmauth or protected endpoints
      # choose at most one of basic/bearer; omit for unauthenticated
      basic:
        username: ""
        password: ""            # inline, discouraged
        passwordFile: ""        # preferred (ESO/rotatable)
      bearer:
        token: ""               # inline, discouraged
        tokenFile: ""           # preferred
  - name: "logs"
    type: "victorialogs"
    url: "http://vlselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9471"
    # vlselect (cluster) serves the same /select/logsql/* API; no path prefix needed.
  - name: "alertmanager"
    type: "alertmanager"
    url: "http://vmalertmanager-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9093"
    readOnly: true              # allow reading alerts/silences but not mutating
# OPTIONAL — same VM & Logs data via the vmauth proxy (the "with vmauth" variant):
#   type: victoriametrics, url: http://vmauth-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:8427,
#   auth.basic (a VMUser username/password from Bitwarden). vmauth routes to vmselect/vlselect internally.
auth:                           # agent -> MCP auth (IDENTICAL to kubernetes-mcp)
  enabled: false
  static: { enabled: false, tokens: [] }
  oidc:   { enabled: false, issuer: "", audience: "", ... }
```

---

## 2. Reuse strategy (what to copy verbatim vs. rewrite)

| Blueprint file | Action for observability-mcp-server |
|---|---|
| `internal/auth/auth.go`, `oidc.go`, `static.go`, `*_test.go` | **Copy verbatim.** Only change import paths `.../kubernetes-mcp-server/...` → `.../observability-mcp-server/...`, and the `ResourceName: "kubernetes-mcp"` string in `auth.go` → `"observability-mcp"`. Auth is datasource-agnostic. |
| `internal/config/config.go` | **Rewrite** the `Config`/`ClusterConfig` parts as `Config`/`DatasourceConfig`; **keep** the `Auth`/`AuthStatic`/`AuthOIDC` structs, `Load`, `applyEnv` (rename `KMCP_`→`OMCP_`), `applyDefaults`, `Validate`, `Warnings` shapes. |
| `internal/metrics/metrics.go` | **Rewrite**: rename `kmcp_`→`omcp_`, replace `mcp_cluster` label with `datasource`, replace client-go adapters with an outbound-HTTP `RoundTripper` recorder (`omcp_datasource_requests_total`, `omcp_datasource_request_duration_seconds`). Keep `RecordTool`, `RecordAuth`, `RecordWriteBlocked`, `SetDatasourceUp`, `SetBuildInfo`. |
| `internal/mcpserver/instrument.go`, `params.go`, `format.go`, `server.go` | **Adapt**: same generic `addTool[In]` wrapper, same `textResult`/`errorResult`, `classifyResult`; swap `clusterParam`→`datasourceParam`, `metricCluster()`→`metricDatasource()`, `resolveCluster`→`resolveDatasource`. |
| `internal/mcpserver/tools_read.go`, `tools_write.go` | **Rewrite**: the actual tools are observability tools (see §1). |
| `internal/clusters/*` | **Replace** with `internal/datasources/*` (registry, datasource, credentials/HTTP client). |
| `internal/k8s/resources.go` | **Replace** with backend clients: `internal/promapi`, `internal/logsql`, `internal/alertmanager`. |
| `main.go` | **Adapt**: same structure (config load, metrics register, registry build, mcpserver, auth middleware, `/mcp`+`/healthz`+`/readyz`+metrics server, `probeDatasources`). |
| `Dockerfile`, `Makefile`, `.dockerignore`, `.gitignore`, `LICENSE`, `renovate.json` | **Copy**, adjust names (`kubernetes-mcp`→`observability-mcp`, module path). `renovate.json` is copied **verbatim** (same extends/automerge). |
| `.github/workflows/ci.yml` | **Adapt**: drop `e2e-envtest` and `e2e-kind` (no Kubernetes); keep `test`, `helm`, `build-and-push`, `publish-chart`. Add a Go `e2e` job using `httptest` fake backends. |
| `deploy/helm/kubernetes-mcp/**` | **Copy → `deploy/helm/observability-mcp/`**, rename, replace cluster/RBAC specifics with datasource config; **drop `rbac.yaml`/`serviceaccount` RBAC tiers** (no cluster access needed) — keep a plain ServiceAccount. |
| `docs/**`, `examples/**`, `README.md` | **Rewrite** for the observability domain (keep structure & mermaid). |

---

## 3. Step-by-step implementation

> Legend: each step lists **Files**, **What to do**, and **Verify**. Env-var prefix is `OMCP_`.
> All Go import paths use module `github.com/truepace-io-oss/observability-mcp-server`.

### Phase A — Repo scaffold & Go module

#### Step A1 — Initialize module & static files
- **Files:** `go.mod`, `.gitignore`, `.dockerignore`, `LICENSE`, `Makefile`, `renovate.json`, `Dockerfile`
- **Do:**
  - `go mod init github.com/truepace-io-oss/observability-mcp-server` with `go 1.26.0`.
  - Copy `../kubernetes-mcp/.gitignore`, `.dockerignore`, `LICENSE`, `renovate.json` **verbatim**.
    (`renovate.json` keeps `"extends": ["config:recommended","local>truepace-io-oss/renovate-bot"]`,
    `platformAutomerge: true`, the "all dependencies" group + automerge — unchanged.)
  - Copy `Makefile`; set `BINARY := observability-mcp`, `PKG := github.com/truepace-io-oss/observability-mcp-server`;
    **remove** `ENVTEST_K8S`, `test-e2e`, `test-e2e-kind` targets; change `test` to `go test ./internal/... -race -count=1`;
    add `test-e2e: go test ./test/e2e/... -race -count=1`; `helm-lint: helm lint deploy/helm/observability-mcp`;
    `run: go run . --config examples/config.yaml`.
  - Copy `Dockerfile`; change build output `-o /out/observability-mcp .`, `COPY --from=builder /out/observability-mcp /observability-mcp`, `ENTRYPOINT ["/observability-mcp"]`. Keep `EXPOSE 9090`, distroless nonroot, multi-arch `$BUILDPLATFORM`/`$TARGETARCH`.
  - Initial `go.mod` requires (add as code is written): `github.com/modelcontextprotocol/go-sdk`, `github.com/coreos/go-oidc/v3`, `github.com/go-jose/go-jose/v4`, `github.com/prometheus/client_golang`, `sigs.k8s.io/yaml`, `github.com/prometheus/common/model` (for PromQL types, optional). No `k8s.io/*` deps.
- **Verify:** `go mod tidy` runs clean once packages exist (defer full tidy until after Phase B/C).

#### Step A2 — Directory layout
- **Do:** create the tree (empty for now):
```
main.go
internal/
  config/            config.go        config_test.go
  metrics/           metrics.go       metrics_test.go
  auth/              auth.go oidc.go static.go oidc_test.go static_test.go   (copied)
  datasources/       registry.go datasource.go credentials.go httpclient.go registry_test.go
  promapi/           client.go types.go            (Prometheus/VM query API)
  logsql/            client.go types.go            (VictoriaLogs)
  alertmanager/      client.go types.go            (Alertmanager v2)
  mcpserver/         server.go instrument.go params.go format.go
                     tools_metrics.go tools_logs.go tools_alertmanager.go tools_test.go
deploy/helm/observability-mcp/   (Phase H)
docs/                            (Phase I)
examples/                        (Phase I)
test/e2e/                        (Phase G)
```

### Phase B — Config

#### Step B1 — `internal/config/config.go`
- **Do:** Port the blueprint config. Keep `Auth`, `AuthStatic`, `AuthToken`, `AuthOIDC` structs and their
  `validate()`/`ServeResourceMetadata()` **unchanged**. Replace cluster types with:
```go
type Config struct {
    ListenAddr        string             `json:"listenAddr"`
    MetricsAddr       string             `json:"metricsAddr"`
    LogLevel          string             `json:"logLevel"`
    ReadOnly          bool               `json:"readOnly"`
    DefaultDatasource string             `json:"defaultDatasource"`
    Datasources       []DatasourceConfig `json:"datasources"`
    Auth              Auth               `json:"auth"`
}

type DatasourceConfig struct {
    Name       string        `json:"name"`
    Type       string        `json:"type"`        // victoriametrics|prometheus|victorialogs|alertmanager
    URL        string        `json:"url"`
    PathPrefix string        `json:"pathPrefix,omitempty"`
    ReadOnly   bool          `json:"readOnly,omitempty"`
    Timeout    string        `json:"timeout,omitempty"` // Go duration; default 30s
    TLS        DatasourceTLS `json:"tls,omitempty"`
    Auth       DatasourceAuth `json:"auth,omitempty"`
}

type DatasourceTLS struct {
    InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
    CAFile                string `json:"caFile,omitempty"`
    CAData                string `json:"caData,omitempty"` // base64 PEM
}

type DatasourceAuth struct {
    Basic  *DatasourceBasicAuth  `json:"basic,omitempty"`
    Bearer *DatasourceBearerAuth `json:"bearer,omitempty"`
}
type DatasourceBasicAuth struct {
    Username     string `json:"username"`
    Password     string `json:"password,omitempty"`
    PasswordFile string `json:"passwordFile,omitempty"`
}
type DatasourceBearerAuth struct {
    Token     string `json:"token,omitempty"`
    TokenFile string `json:"tokenFile,omitempty"`
}
```
- **Validation rules** (`Validate()`), first violation returned:
  - `logLevel` in {debug,info,warn,error}.
  - `len(Datasources) > 0`.
  - Each datasource: non-empty `Name` matching DNS-label regex (reuse `nameRe`); unique name;
    `Type` in the allowed set; `URL` non-empty and parseable, scheme `http`/`https`.
  - `tls.insecureSkipTLSVerify` must not be combined with `caFile`/`caData`.
  - `auth.basic` and `auth.bearer` are mutually exclusive; basic requires `username` + exactly one of `password`/`passwordFile`; bearer requires exactly one of `token`/`tokenFile`.
  - `timeout` (if set) parses as `time.Duration`.
  - Default `defaultDatasource` to the sole datasource when only one; else require it and require it to exist.
  - `Auth.validate()` unchanged from blueprint.
- **applyDefaults:** `ListenAddr` `0.0.0.0:9090`; `MetricsAddr` `:9091`; `LogLevel` `info`;
  per-datasource `Timeout` default `30s`; OIDC `GroupsClaim`/`UsernameClaim` defaults unchanged.
- **applyEnv:** rename all `KMCP_*` → `OMCP_*`; keep `OMCP_LISTEN_ADDR`, `OMCP_METRICS_ADDR`,
  `OMCP_LOG_LEVEL`, `OMCP_READ_ONLY`, `OMCP_DEFAULT_DATASOURCE`, `OMCP_AUTH_ENABLED`,
  `OMCP_AUTH_STATIC_TOKEN`, `OMCP_AUTH_OIDC_ISSUER`, `OMCP_AUTH_OIDC_AUDIENCE`.
- **Warnings():** inline password/token discouraged; `insecureSkipTLSVerify` warning; auth-enabled TLS warning.
  Add helper `DatasourceNames()`.
- **Verify:** unit test `config_test.go` covering: valid multi-datasource; unknown type; dup name; bad URL;
  basic+bearer conflict; insecure+CA conflict; default inference. `go test ./internal/config/...`.

### Phase C — Datasource registry & HTTP client

#### Step C1 — `internal/datasources/httpclient.go`
- **Do:** Build a `*http.Client` per datasource from `DatasourceConfig`:
  - TLS: load `CAFile` (preferred) or decode `CAData` into a `*x509.CertPool`; set `InsecureSkipVerify` when configured.
  - Auth injected via a `http.RoundTripper` wrapper:
    - basic → set `Authorization: Basic base64(user:pass)`; re-read `PasswordFile` each request (rotation).
    - bearer → set `Authorization: Bearer <token>`; re-read `TokenFile` each request.
  - Wrap the transport with the **metrics RoundTripper** (Step E) so every outbound call records
    `omcp_datasource_requests_total{datasource,type,code}` + duration.
  - `Timeout` from config; `User-Agent: observability-mcp`.

#### Step C2 — `internal/datasources/datasource.go`
- **Do:**
```go
type Datasource struct {
    Name     string
    Type     string // victoriametrics|prometheus|victorialogs|alertmanager
    ReadOnly bool
    BaseURL  string // url + pathPrefix, no trailing slash
    HTTP     *http.Client
}
// URL joins BaseURL + path (+ query).
func (d *Datasource) URL(path string) string
// Ping checks reachability per type and returns a short status string.
func (d *Datasource) Ping(ctx context.Context) (string, error)
```
  - `Ping` per type: `prometheus`/`victoriametrics` → GET `/api/v1/query?query=vector(1)` (or `-/healthy`);
    `victorialogs` → GET `/health` (fallback `/select/logsql/query?query=*&limit=1`);
    `alertmanager` → GET `/api/v2/status`. Non-2xx ⇒ error.
  - Capability helpers: `IsMetrics()` (prometheus|victoriametrics), `IsLogs()`, `IsAlertmanager()`.

#### Step C3 — `internal/datasources/registry.go`
- **Do:** Port `clusters/registry.go` structure verbatim, renamed:
  `Registry{mu, datasources map[string]*Datasource, order []string, defaultName string}`,
  `Build(cfg *config.Config) (*Registry, error)`, `Get(name)` (empty → default, descriptive miss error listing names),
  `Default()`, `DefaultName()`, `Names()`, `All()` (sorted), plus `NewRegistryForTest`.
- **Verify:** `registry_test.go` (build from config, get default, unknown-name error, All() sorted).

#### Step C4 — `internal/datasources/credentials.go`
- **Do:** Helper(s) used by C1: `loadCACertPool(cfg)`, `basicAuthHeader(user, passOrFile)`, `bearerHeader(tokenOrFile)`,
  each re-reading files on call. Include base64 CA decode with a clear error.

### Phase D — Backend clients

> Each client takes a `*datasources.Datasource` (for `HTTP` + `URL()`), issues requests, decodes JSON,
> and returns Go structs. Keep responses compact and LLM-friendly (formatting happens in mcpserver).

#### Step D1 — `internal/promapi/` (Prometheus & VictoriaMetrics query API)
- **Endpoints** (all under BaseURL; VM & Prometheus share the `/api/v1` surface):
  - `Query(ctx, expr, time)` → `GET /api/v1/query?query=&time=`
  - `QueryRange(ctx, expr, start, end, step)` → `GET /api/v1/query_range`
  - `Series(ctx, matches[], start, end)` → `GET /api/v1/series`
  - `Labels(ctx, start, end, matches[])` → `GET /api/v1/labels`
  - `LabelValues(ctx, label, start, end, matches[])` → `GET /api/v1/label/<label>/values`
  - `Metadata(ctx, metric, limit)` → `GET /api/v1/metadata`
  - `Rules(ctx, type)` → `GET /api/v1/rules` (alerting/recording)
  - `ActiveAlerts(ctx)` → `GET /api/v1/alerts`
  - `Targets(ctx, state)` → `GET /api/v1/targets`
- **types.go:** decode the standard Prometheus envelope `{status, data, errorType, error, warnings}`.
  Return an error when `status != "success"`, surfacing `error`/`errorType` text to the agent.
- **Verify:** `client_test.go` with `httptest.Server` returning canned Prometheus JSON for query, labels, rules.

#### Step D2 — `internal/logsql/` (VictoriaLogs)
- **Endpoints** (VictoriaLogs HTTP API; POST form or GET with `query` param):
  - `Query(ctx, logsql, start, end, limit)` → `POST /select/logsql/query` (streaming NDJSON; read up to `limit` lines)
  - `Hits(ctx, logsql, start, end, step, field)` → `POST /select/logsql/hits`
  - `StatsQuery(ctx, logsql, time)` → `POST /select/logsql/stats_query`
  - `FieldNames(ctx, logsql, start, end)` → `POST /select/logsql/field_names`
  - `FieldValues(ctx, logsql, field, start, end, limit)` → `POST /select/logsql/field_values`
  - `Streams(ctx, logsql, start, end, limit)` → `POST /select/logsql/streams`
- **Notes:** LogsQL results are NDJSON (one JSON object per line). Decode line-by-line, cap at `limit`
  (default 100, hard max e.g. 1000) to protect the agent context. Accept `start`/`end` as RFC3339 or
  relative (e.g. `5m`, `1h`) — pass through as VictoriaLogs `start`/`end` params.
- **Verify:** `client_test.go` with `httptest.Server` returning canned NDJSON.

#### Step D3 — `internal/alertmanager/` (Alertmanager v2)
- **Endpoints:**
  - `Alerts(ctx, active, silenced, inhibited, filter[])` → `GET /api/v2/alerts`
  - `AlertGroups(ctx, filter[])` → `GET /api/v2/alerts/groups`
  - `Silences(ctx, filter[])` → `GET /api/v2/silences`
  - `Silence(ctx, id)` → `GET /api/v2/silence/{id}`
  - `Status(ctx)` → `GET /api/v2/status`
  - `Receivers(ctx)` → `GET /api/v2/receivers`
  - **(write)** `CreateSilence(ctx, silence)` → `POST /api/v2/silences` → returns `silenceID`
  - **(write)** `DeleteSilence(ctx, id)` → `DELETE /api/v2/silence/{id}`
- **types.go:** silence = `{matchers:[{name,value,isRegex,isEqual}], startsAt, endsAt, createdBy, comment}`.
- **Verify:** `client_test.go` with fake AM v2 server for alerts + silence create/delete.

### Phase E — Metrics

#### Step E1 — `internal/metrics/metrics.go`
- **Do:** Port the blueprint, `kmcp_`→`omcp_`, `mcp_cluster`→`datasource`. Metrics:
  - `omcp_tool_calls_total{tool,datasource,result}` counter (`result = ok|error|forbidden|blocked`)
  - `omcp_tool_call_duration_seconds{tool,datasource}` histogram
  - `omcp_auth_requests_total{method,result}` counter (`method = static|oidc|none`, `result = allow|deny`)
  - `omcp_datasource_up{datasource,type}` gauge (from background probe)
  - `omcp_writes_blocked_total{datasource,reason}` counter (`reason = global_readonly|datasource_readonly`)
  - `omcp_build_info{version,goversion}` gauge
  - `omcp_datasource_requests_total{datasource,type,code}` counter (outbound HTTP result)
  - `omcp_datasource_request_duration_seconds{datasource,type}` histogram
- **Do:** replace the client-go adapters with:
  - `func NewRoundTripper(next http.RoundTripper, datasource, dsType string) http.RoundTripper` that times the
    request and records the two `omcp_datasource_*` metrics keyed by `datasource`,`type`,`code`.
- **Keep:** `RecordTool`, `RecordAuth`, `RecordWriteBlocked`, `SetDatasourceUp`, `SetBuildInfo`.
- **Verify:** `metrics_test.go` (RoundTripper increments counter on a fake transport; labels bounded).

### Phase F — MCP server & tools

#### Step F1 — `internal/mcpserver/server.go`
- **Do:** `Server{reg *datasources.Registry, readOnly bool}`, `New(reg,cfg)`, `SetVersion`,
  `MCPServer()` builds `mcp.NewServer(&mcp.Implementation{Name:"observability-mcp", Version:serverVersion}, nil)`,
  then `registerCommonTools`, `registerMetricsTools`, `registerLogsTools`, `registerAlertmanagerTools`.
  Each registration is guarded so a tool only appears when at least one datasource supports it
  (e.g. skip logs tools if no `victorialogs` datasource configured) — but always register the tool and
  let the handler return a friendly error if the *targeted* datasource is the wrong type. (Simpler: always
  register all tools; validate datasource type inside the handler.)

#### Step F2 — `internal/mcpserver/instrument.go` + `params.go` + `format.go`
- **instrument.go:** identical generic wrapper `addTool[In any]`, recording
  `metrics.RecordTool(name, s.datasourceLabel(in), classifyResult(res,err), dur)`.
  `datasourceLabel(in)` via interface `interface{ metricDatasource() string }` → default name fallback.
  `classifyResult` unchanged (`ok|error|forbidden|blocked`; treat "read-only" text as `blocked`).
- **params.go:**
```go
type datasourceParam struct {
    Datasource string `json:"datasource,omitempty" jsonschema:"the configured datasource to target; defaults to the server's default datasource when omitted"`
}
func (d datasourceParam) metricDatasource() string { return d.Datasource }

type timeRangeParam struct {
    Start string `json:"start,omitempty" jsonschema:"start time: RFC3339, unix seconds, or relative like '1h'/'5m' (default depends on tool)"`
    End   string `json:"end,omitempty" jsonschema:"end time: RFC3339, unix seconds, or 'now' (default now)"`
}
```
  Add `resolveDatasource(name) (*datasources.Datasource, error)` (delegates to registry),
  and `assertTypeMetrics/Logs/Alertmanager(ds)` returning a clear error when the datasource is the wrong type.
  Add `assertWritable(ds)` mirroring the blueprint (checks global `s.readOnly` then `ds.ReadOnly`, records
  `omcp_writes_blocked_total`).
- **format.go:** `textResult`, `errorResult` (verbatim). Add formatters:
  - `formatVector`/`formatMatrix` (compact PromQL result: metric labels → value(s), capped rows);
  - `formatLogLines` (NDJSON → `time level msg` compact lines, capped);
  - `formatAlerts`, `formatSilences`, `formatRules`, `formatTargets` tables.

#### Step F3 — `internal/mcpserver/tools_metrics.go`
- **Do:** register + implement (all embed `datasourceParam`; default datasource must be metrics-type):
  - `metrics_query{Query, Time?}` → promapi.Query → `formatVector`
  - `metrics_query_range{Query, Start, End, Step}` → QueryRange → `formatMatrix`
  - `metrics_series{Match[], Start?, End?}` → Series
  - `metrics_labels{Start?, End?, Match[]?}` → Labels
  - `metrics_label_values{Label, Start?, End?, Match[]?}` → LabelValues
  - `metrics_metadata{Metric?, Limit?}` → Metadata
  - `rules_list{Type?}` → Rules → `formatRules`
  - `alerts_active{}` → ActiveAlerts → `formatAlerts`
  - `targets_list{State?}` → Targets → `formatTargets`
- Each: `ds,err := s.resolveDatasource(in.Datasource)`; `if err := s.assertTypeMetrics(ds); ...`; call client; `textResult(formatted)`.

#### Step F4 — `internal/mcpserver/tools_logs.go`
- **Do:** register + implement (datasource must be `victorialogs`):
  - `logs_query{Query, Start?, End?, Limit?}` → logsql.Query → `formatLogLines`
  - `logs_hits{Query, Start?, End?, Step?, Field?}` → Hits
  - `logs_stats{Query, Time?}` → StatsQuery
  - `logs_field_names{Query?, Start?, End?}` → FieldNames
  - `logs_field_values{Field, Query?, Start?, End?, Limit?}` → FieldValues
  - `logs_streams{Query?, Start?, End?, Limit?}` → Streams

#### Step F5 — `internal/mcpserver/tools_alertmanager.go`
- **Do:** register + implement (datasource must be `alertmanager`):
  - Read: `alertmanager_alerts{Active?,Silenced?,Inhibited?,Filter[]?}`, `alertmanager_alert_groups{Filter[]?}`,
    `alertmanager_silences_list{Filter[]?}`, `alertmanager_silence_get{ID}`, `alertmanager_status{}`,
    `alertmanager_receivers{}`.
  - Write (gate with `s.assertWritable(ds)` first):
    `alertmanager_silence_create{Matchers[], StartsAt?, EndsAt/Duration, CreatedBy, Comment}` → CreateSilence;
    `alertmanager_silence_delete{ID}` → DeleteSilence.
- **Verify (F3–F5):** `tools_test.go` — spin fake backends (`httptest`), build a `Server` from a test registry,
  call each handler, assert formatted output & that write tools are blocked when `readOnly`.

#### Step F6 — `internal/mcpserver/tools_common.go`
- **Do:** `datasources_list{}` (no params) → iterate `reg.All()`, ping each, print
  `- <name> [type] (default?) readOnly=<b> — <status>`.

### Phase G — main.go & e2e

#### Step G1 — `main.go`
- **Do:** Port blueprint `main.go`:
  - flags `--config` (env `OMCP_CONFIG`), `--version`.
  - `config.Load`, logger, `mcpserver.SetVersion`, `metrics.SetBuildInfo`, log `cfg.Warnings()`.
  - `reg := datasources.Build(cfg)`; log datasource names + default + readOnly.
  - `srv := mcpserver.New(reg, cfg)`, `authn := auth.Build(ctx, cfg.Auth)`.
  - `mcp.NewStreamableHTTPHandler`, mux with `/mcp` (auth), OIDC metadata path, `/healthz`,
    `/readyz` (ping default datasource, 3s timeout).
  - metrics server on `MetricsAddr` (unless `"off"`), `go probeDatasources(ctx, reg)` (30s ticker →
    `metrics.SetDatasourceUp(name, type, up)`).
  - graceful shutdown identical.
- **Verify:** `go build ./...`; `go run . --config examples/config.yaml` (after Phase I creates the example) starts and serves `/healthz`.

#### Step G2 — `test/e2e/`
- **Do:** e2e harness that starts fake VM/Logs/Alertmanager `httptest` servers, writes a temp config pointing
  at them, boots the MCP server in-process, and drives it over streamable HTTP: assert `datasources_list`,
  a `metrics_query`, a `logs_query`, `alertmanager_alerts`, and that auth (static token) gates `/mcp`.
- **Verify:** `go test ./test/e2e/... -race -count=1`; `go vet ./...`; `gofmt -l .` clean; `go mod tidy`.

### Phase H — Helm chart (`deploy/helm/observability-mcp/`)

Copy `../kubernetes-mcp/deploy/helm/kubernetes-mcp/` and adapt. Key changes:

#### Step H1 — Chart.yaml & _helpers.tpl
- `name: observability-mcp`, description referencing observability datasources, `version`/`appVersion` start `0.1.0`.
- `_helpers.tpl`: rename template prefixes `kubernetes-mcp.*` → `observability-mcp.*`. Replace the
  `remoteSecretName` helper with `datasourceSecretName` (`<fullname>-ds-<name>`).

#### Step H2 — values.yaml
- **Remove** `localCluster`, `remoteClusters`, and all `rbac` tiers.
- **Add** `datasources:` list mirroring §1 schema:
```yaml
config:
  logLevel: "info"
  readOnly: false
  defaultDatasource: "vm"
datasources: []
  # - name: vm
  #   type: victoriametrics
  #   url: http://vmsingle...:8429
  #   pathPrefix: ""
  #   readOnly: false
  #   auth:
  #     basic: { username: "", esoRef: "" }      # ESO -> Secret key `password`, mounted as passwordFile
  #     bearer: { esoRef: "" }                    # ESO -> Secret key `token`, mounted as tokenFile
  #   tls: { caEsoRef: "" }                       # ESO -> Secret key `ca.crt`, mounted as caFile
externalSecrets:
  enabled: true
  secretStore: { kind: ClusterSecretStore, name: bitwarden-secretsmanager }
  refreshInterval: 1h
metrics: { enabled: true, port: 9091 }
auth: { ... }            # identical to blueprint (static + oidc)
service: { type: ClusterIP, port: 80, targetPort: 9090 }
ingress: { enabled: false, className: nginx-internal, host: "", annotations: {streaming...}, tls: {...}, basicAuth: {...} }
serviceMonitor: { enabled: false, interval: 30s, scrapeTimeout: 10s, labels: {} }
grafanaDashboard: { enabled: false, label: grafana_dashboard, labelValue: "1", folder: "", folderAnnotation: grafana_folder }
resources / podSecurityContext / securityContext / nodeSelector / tolerations / affinity  # verbatim
```

#### Step H3 — templates
- `serviceaccount.yaml`: keep (plain SA). **Delete** `rbac.yaml`.
- `configmap.yaml`: render `config.yaml` from `.Values.datasources`, mounting per-datasource secret material at
  `/etc/omcp/datasources/<name>/{password,token,ca.crt}` and referencing them as `passwordFile`/`tokenFile`/`caFile`.
- `deployment.yaml`: `args: ["--config","/etc/omcp/config.yaml"]`; mount config at `/etc/omcp/config.yaml`;
  loop `.Values.datasources` to mount their secrets; keep static-auth token mounts at `/etc/omcp/auth/<name>/token`;
  ports 9090/9091; probes `/healthz`,`/readyz`.
- `externalsecret.yaml`: one ExternalSecret per datasource that has an `auth.*.esoRef`/`tls.caEsoRef`
  (`<fullname>-ds-<name>` with keys `password`/`token`/`ca.crt` as configured) + static-auth token ESO +
  image pull ESO + optional ingress basic-auth ESO (all as blueprint).
- `service.yaml`, `servicemonitor.yaml`, `ingress.yaml`, `grafana-dashboard.yaml`, `NOTES.txt`: copy, rename,
  drop cluster wording. Dashboard file → `dashboards/observability-mcp.json`.
- `deploy/helm/observability-mcp/README.md`: chart docs (install, datasource config, ESO, auth, ingress).
- **Verify:** `helm lint deploy/helm/observability-mcp`; `helm template t deploy/helm/observability-mcp` and with
  `--set auth.enabled=true --set auth.oidc.enabled=true --set auth.oidc.issuer=https://x --set auth.oidc.audience=observability-mcp`
  and with a sample datasource set — all render without error.

#### Step H4 — Grafana dashboard `dashboards/observability-mcp.json`
- Panels (Prometheus/VM datasource, `increase(...[$__range])` style like blueprint):
  1. Build info (`omcp_build_info`) table
  2. Datasource reachable (`omcp_datasource_up`) stat per `datasource`,`type`
  3. Agent auth per interval (`sum(increase(omcp_auth_requests_total[$__interval])) by (result)`)
  4. Tool calls by tool (`... omcp_tool_calls_total ... by (tool)`)
  5. Tool calls by result (`... by (result)`: ok/error/forbidden/blocked)
  6. Tool latency p95 (`histogram_quantile(0.95, sum(rate(omcp_tool_call_duration_seconds_bucket[5m])) by (le,tool))`)
  7. Outbound datasource requests by code (`sum(increase(omcp_datasource_requests_total[$__interval])) by (datasource,code)`)
  8. Outbound datasource latency p95 (`omcp_datasource_request_duration_seconds_bucket`)
  9. Writes blocked (`sum(increase(omcp_writes_blocked_total[$__interval])) by (datasource,reason) or vector(0)`)
  - Template var `datasource` (Prometheus/VM selector), 5s refresh, last-3h default.

### Phase I — Docs, examples, README

#### Step I1 — `README.md` (with mermaid + compatibility list)
- Sections: motivation; **datasource compatibility list** (table below); topology (mermaid);
  central agent-auth model (mermaid sequence: agent→MCP OIDC/static, MCP→datasource optional vmauth);
  tool catalog (grouped by type); config schema; quick start; agent config (Claude/Cursor); deploy (Helm+ESO);
  security model (agent-auth ≠ datasource-auth; read-only guard; internal vs OIDC-gated ingress); metrics; testing; development.
- **Compatibility list (put in README):**

| Backend | Status | Notes |
|---|---|---|
| VictoriaMetrics (single & cluster) | ✅ Supported | `/api/v1/*` query API; direct or via `vmauth` |
| Prometheus | ✅ Supported | Prometheus-compatible `/api/v1/*` |
| VictoriaLogs | ✅ Supported | LogsQL via `/select/logsql/*`; direct or via `vmauth` |
| Alertmanager | ✅ Supported | v2 API; read + silence create/delete (write-gated) |
| Grafana (dashboards/query) | 🔜 Planned | not yet implemented |
| Loki | 🔜 Planned | LogQL — candidate for a `loki` datasource type |
| Thanos / Cortex / Mimir | 🔜 Planned | Prometheus-API-compatible, likely works as `prometheus` |
| Jaeger / Tempo (traces) | 🔜 Considered | separate tool group |

- **Mermaid** request-flow diagram (agent → `/mcp` → tool → datasource registry → HTTP client → backend).

#### Step I2 — `docs/architecture.md`
- Request flow, package map, datasource-auth modes (none/basic/bearer, vmauth), TLS/CA, testing strategy — all mermaid.

#### Step I3 — `docs/datasources.md` (**"how to add & configure"**, mirroring blueprint's rbac/config docs)
- **How to add a datasource:** the config block, one subsection per type with exact endpoints & example URLs
  for the in-cluster VM stack (`victoria-metrics-k8s-stack`, `victoria-logs-collector`) and `vmauth`.
- **How to add a NEW datasource type** (developer guide): implement a client package under `internal/`,
  add a `Type` constant + validation, register its tools in `mcpserver`, add reachability in `Ping`, update the README compatibility list.

#### Step I4 — `docs/auth.md`
- Copy blueprint auth.md (agent→MCP static + Authentik/Keycloak OIDC), adjust names; add a short section on
  **datasource-side auth** (basic/bearer for vmauth) so the two auth layers are not confused.

#### Step I5 — `docs/metrics.md`
- The `omcp_*` metric table (from Step E), scraping via ServiceMonitor, the Grafana dashboard, and useful queries
  (tool error rate, auth denies, datasource down, outbound 5xx by datasource).

#### Step I6 — `docs/environments-integration.md`
- Mirror blueprint's; describe adding the app to the `environments` GitOps repo (Phase J), the OCI chart pin,
  the Authentik blueprint, and the datasource wiring for the tools cluster.

#### Step I7 — `examples/`
- `config.yaml`: all datasource types incl. one direct + one `vmauth` example + static & OIDC auth (disabled by default).
- `mcp.claude.json`, `mcp.cursor.json`: three entries (OIDC via Authentik with `clientId`+`authServerMetadataUrl`,
  DCR provider, static bearer) — adjust host to `observability-mcp.tools.averion.zone`.

### Phase J — CI/CD (`.github/workflows/ci.yml`)

#### Step J1 — CI
- **Do:** Copy blueprint CI, then:
  - Keep `test` job (`go vet ./...`, `go test ./internal/... -race -count=1`).
  - Replace `e2e-envtest` + `e2e-kind` with a single `e2e` job: `go test ./test/e2e/... -race -count=1`.
  - Keep `helm` job: `helm lint deploy/helm/observability-mcp` + `helm template` (defaults, and auth/OIDC set,
    and a sample datasource set).
  - Keep `build-and-push` **unchanged** (multi-arch GHCR image `ghcr.io/${{ github.repository }}`,
    branch/semver/sha/latest tags) — `needs: [test, e2e, helm]`.
  - Keep `publish-chart` **unchanged** in shape: on `v*` tag, `helm package deploy/helm/observability-mcp` and
    `helm push ... oci://ghcr.io/${{ github.repository_owner }}/charts`. This yields
    `oci://ghcr.io/truepace-io-oss/charts/observability-mcp`.
- **Verify:** `act`/manual read-through; `helm template` commands succeed locally.

### Phase K — `environments` GitOps integration (prod-averion-tools)

> Mirrors exactly how `kubernetes-mcp` is wired. All paths are inside `../environments/`.
> **Do not touch other apps.** After all edits: `myks render ALL` and commit `rendered/` + sources.

#### Step K1 — Prototype `prototypes/observability-mcp/`
- `app-data.ytt.yaml` (`#@data/values-schema`): `application.observabilityMcp` with
  `namespace: observability-mcp`, `image.repository: ghcr.io/truepace-io-oss/observability-mcp-server`,
  `#! renovate: datasource=docker` `tag: "0.1.0"`, `serviceMonitor`, `grafanaDashboard`, `dashboardFolder`,
  `ingressHost`, `oidc:{issuer,audience:"observability-mcp",requiredGroups:[]}`, and a `#@schema/default []`
  `datasources:` list (`name,type,url,pathPrefix,readOnly` + optional `basicUserBitwardenId/passwordBitwardenId/
  bearerTokenBitwardenId/caBitwardenId`).
- `vendir/vendir-data.ytt.yaml` (`observabilityMcpChart`): **alphabetical keys** (renovate!):
  `name: observability-mcp`, `url: oci://ghcr.io/truepace-io-oss/charts`, `version: 0.1.0`, with `#! renovate: datasource=docker`.
- `vendir/base.yaml`: vendir `Config` pulling the OCI chart (copy blueprint, swap the data ref).
- `helm/observability-mcp.yaml`: ytt-templated Helm values → sets `image`, `config.defaultDatasource`,
  `auth.oidc` (enabled, issuer/audience/requiredGroups, groupsClaim, usernameClaim, resourceMetadata),
  `externalSecrets` (enabled when any datasource needs a secret), `datasources` (loop; map Bitwarden UUIDs →
  `auth.basic.esoRef`/`auth.bearer.esoRef`/`tls.caEsoRef`), `ingress` (enabled, `className: nginx`, host, TLS),
  `podSecurityContext` (nonroot 65532), `serviceMonitor`, `grafanaDashboard` (`folderAnnotation: grafana-folder`), `service`.

#### Step K2 — Authentik blueprint
- `prototypes/authentik/ytt/observability-mcp-blueprint-secret.yaml`: copy the kubernetes-mcp blueprint,
  replace names → group `observability-mcp-users`, provider "Observability MCP", `client_id`/`applicationSlug`
  `observability-mcp`, same `groups` scope mapping, public client + PKCE, RS256, localhost redirect regexes,
  token validities. Wrap in `#@ if data.values.authentikBlueprints.observabilityMcp.enabled:`.
- `prototypes/authentik/helm/authentik.yaml`: add, under an `#@ if ...observabilityMcp.enabled:` guard,
  `authentik-blueprint-observability-mcp` to `blueprints.secrets`.

#### Step K3 — Global schema `envs/env-data.ytt.yaml`
- Add to `authentikBlueprints`:
```yaml
  observabilityMcp:
    enabled: false
    clientId: observability-mcp
    applicationSlug: observability-mcp
    groupName: observability-mcp-users
    signingKeyName: "authentik Self-signed Certificate"
```

#### Step K4 — Enable in prod-averion-tools `envs/prod-averion-tools/env-data.ytt.yaml`
- Add `- proto: observability-mcp` to `environment.applications`.
- Add `observabilityMcp: { enabled: true }` under `authentikBlueprints`.

#### Step K5 — Per-app overrides `envs/prod-averion-tools/_apps/observability-mcp/app-data.ytt.yaml`
```yaml
#@data/values
---
application:
  observabilityMcp:
    ingressHost: "observability-mcp.tools.averion.zone"
    serviceMonitor: true
    grafanaDashboard: true
    dashboardFolder: "MCP Servers"
    oidc:
      issuer: "https://auth.tools.averion.zone/application/o/observability-mcp/"
      audience: "observability-mcp"
    datasources:
      - name: metrics
        type: victoriametrics
        url: "http://vmselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:8481"
        pathPrefix: "/select/0/prometheus"
        readOnly: true
      - name: logs
        type: victorialogs
        url: "http://vlselect-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9471"
        readOnly: true
      - name: alertmanager
        type: alertmanager
        url: "http://vmalertmanager-victoria-metrics-k8s-stack.victoria-metrics-k8s-stack.svc:9093"
        readOnly: true
```
> **These URLs/ports are VERIFIED against the live `prod-averion-tools` cluster (2026-07-16):** VM stack runs in
> **cluster mode** in namespace `victoria-metrics-k8s-stack`; `vmselect` :8481 (needs `/select/0/prometheus`),
> `vlselect` :9471, `vmalertmanager` :9093. These are headless Services (clusterIP `None`) — the Go HTTP client
> resolves them via DNS with no extra config. VictoriaLogs storage lives in the `victoria-metrics-k8s-stack`
> namespace (the `victoria-logs-collector` ns holds only the shipping daemonset — no Service). All datasources
> start `readOnly: true` for the first prod rollout. (Optional `vmauth` proxy variant: `vmauth-...svc:8427` with a
> VMUser basic-auth credential from Bitwarden.)

#### Step K6 — Docs & render
- `environments/docs/observability-mcp.md`: mirror `docs/kubernetes-mcp.md` (endpoint, Authentik group
  `observability-mcp-users`, Claude Code config snippet, what it can query).
- Run `myks render ALL` (never a single app). Commit updated `rendered/argocd/prod-averion-tools/app-observability-mcp.yaml`
  and `rendered/envs/prod-averion-tools/observability-mcp/**`, plus all edited sources.
- **Verify:** the rendered ArgoCD `Application` points at `rendered/envs/prod-averion-tools/observability-mcp`,
  namespace `observability-mcp`, and the Deployment references `ghcr.io/truepace-io-oss/observability-mcp-server:<tag>`.

---

## 4. Release & first deploy sequence (so it can deploy immediately after first push)

1. Land Phases A–J on `main`; push. CI runs `test`+`e2e`+`helm`, then `build-and-push` publishes
   `ghcr.io/truepace-io-oss/observability-mcp-server:{sha,latest,branch}`.
2. Tag `v0.1.0`; push tag. CI `build-and-push` (semver) + `publish-chart` pushes
   `oci://ghcr.io/truepace-io-oss/charts/observability-mcp:0.1.0`.
3. In `environments`, set the prototype `vendir-data` chart `version: 0.1.0` and app image `tag: "0.1.0"`
   (or an image `sha-…` tag) — Phase K. `myks render ALL`, commit.
4. Argo CD (`prod-averion-tools` project) syncs `prod-averion-tools-observability-mcp` automatically.
5. Add yourself to the Authentik `observability-mcp-users` group; add the MCP to Claude Code and
   `claude mcp login observability-mcp`.

> The chart/image versions must exist in GHCR before the `environments` render pins them, hence the tag-first order.

---

## 5. Acceptance checklist

- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go mod tidy` no-op.
- [ ] `go test ./internal/... ./test/e2e/... -race` green (config, registry, promapi, logsql, alertmanager, metrics, tools, e2e).
- [ ] `metrics_query`, `metrics_query_range`, `logs_query`, `alertmanager_alerts` work against fake backends; write tools blocked when `readOnly`.
- [ ] Static bearer + Authentik OIDC gate `/mcp`; `/healthz`,`/readyz`,`/metrics` open; OIDC protected-resource metadata served.
- [ ] `omcp_*` metrics exposed on `:9091`; ServiceMonitor scrapes; Grafana dashboard imports.
- [ ] `helm lint` + `helm template` (defaults, auth, datasources) succeed; chart renders SA (no RBAC).
- [ ] CI: `test`,`e2e`,`helm`,`build-and-push`,`publish-chart` all present and correct; renovate.json identical shape to blueprint.
- [ ] README has mermaid diagrams + datasource compatibility list; `docs/datasources.md` explains add/configure + adding new types.
- [ ] `environments`: prototype + authentik blueprint + prod-averion-tools enablement + per-app overrides added;
      `myks render ALL` clean; ArgoCD `Application` and Deployment rendered correctly; datasource Service URLs verified.

---

## 6. Open decisions / assumptions (revisit if wrong)

- **Env prefix `OMCP_`, deployment identity `observability-mcp`, module `observability-mcp-server`** — per §0 table.
- **Alertmanager write tools** (`silence_create/delete`) are included but gated by `readOnly`; prod starts read-only.
- **Datasource TLS/auth** modeled as optional per-datasource (`none|basic|bearer`, optional CA) to cover vmauth.
- ✅ **Exact in-cluster Service names/ports** — VERIFIED against live `prod-averion-tools` (2026-07-16), baked into §1 and Step K5: `vmselect…:8481` (+`/select/0/prometheus`), `vlselect…:9471`, `vmalertmanager…:9093`, optional `vmauth…:8427`; namespace `victoria-metrics-k8s-stack`; cluster mode; headless Services.
- ✅ **Go SDK version** — CONFIRMED `github.com/modelcontextprotocol/go-sdk v1.6.1` in the blueprint `go.mod`. Pin the same version for API compatibility of `mcp.AddTool`, `mcp.NewStreamableHTTPHandler`, `auth.RequireBearerToken`, `oauthex.ProtectedResourceMetadata`.
