# observability-mcp Helm chart

Deploys the [observability-mcp](https://github.com/truepace-io-oss/observability-mcp-server)
server. The MCP queries one or more observability **datasources**
(VictoriaMetrics, VictoriaLogs, Prometheus, Alertmanager) over outbound HTTP. It
needs **no cluster permissions** — the ServiceAccount carries no RBAC.
Per-datasource credentials (basic-auth passwords, bearer tokens, custom CAs) are
provided via the External Secrets Operator (ESO).

## Install

```bash
helm install my-mcp deploy/helm/observability-mcp \
  --namespace observability-mcp --create-namespace
```

## What gets created

| Resource | When |
|----------|------|
| `ServiceAccount` | always — plain identity, **no RBAC** (outbound HTTP only) |
| `ConfigMap` (`config.yaml`) | always — server + datasource registry (non-secret) |
| `ExternalSecret` (per datasource) | `externalSecrets.enabled` + a datasource has `auth.basic.esoRef` / `auth.bearer.esoRef` / `tls.caEsoRef` |
| `ExternalSecret` (agent static token) | `externalSecrets.enabled` + `auth.static.tokens[].eso.ref` |
| `ExternalSecret` (image pull) | `externalSecrets.enabled` + `imagePullSecret.esoRef` |
| `ExternalSecret` (ingress basic-auth) | `externalSecrets.enabled` + `ingress.basicAuth.esoRef` |
| `Deployment`, `Service` | always |
| `Ingress` | `ingress.enabled` |
| `ServiceMonitor` | `serviceMonitor.enabled` (needs `metrics.enabled`) |
| Grafana dashboard `ConfigMap` | `grafanaDashboard.enabled` (sidecar label `grafana_dashboard: "1"`) |

## Configuring datasources

Each entry in `datasources` is rendered into `config.yaml`. `type` is one of
`victoriametrics | prometheus | victorialogs | alertmanager`. Set
`config.defaultDatasource` to one of the datasource names.

Direct (no auth) example:

```yaml
config:
  defaultDatasource: vm

datasources:
  - name: vm
    type: victoriametrics
    url: http://vmselect-cluster.monitoring:8481
    pathPrefix: "/select/0/prometheus"
    readOnly: false
    timeout: "30s"
```

vmauth / basic-auth example (password supplied via ESO):

```yaml
externalSecrets:
  enabled: true
  secretStore:
    kind: ClusterSecretStore
    name: bitwarden-secretsmanager

datasources:
  - name: vm-auth
    type: victoriametrics
    url: https://vmauth.monitoring.example.com
    pathPrefix: "/select/0/prometheus"
    timeout: "30s"
    tls:
      insecureSkipTLSVerify: false
      caEsoRef: <backend-uuid-for-ca.crt>   # → secret key ca.crt → caFile
    auth:
      basic:
        username: readonly
        esoRef: <backend-uuid-for-password>  # → secret key password → passwordFile
```

Bearer-token example:

```yaml
datasources:
  - name: prom
    type: prometheus
    url: https://prometheus.example.com
    auth:
      bearer:
        esoRef: <backend-uuid-for-token>     # → secret key token → tokenFile
```

## ESO secret wiring

For any datasource referencing a secret, one `ExternalSecret`
`<release>-observability-mcp-ds-<name>` is created, pulling the configured backend
keys into a single Kubernetes Secret with keys:

| config field | Secret key | Mounted file |
|--------------|------------|--------------|
| `auth.basic.esoRef` | `password` | `/etc/omcp/datasources/<name>/password` (→ `passwordFile`) |
| `auth.bearer.esoRef` | `token` | `/etc/omcp/datasources/<name>/token` (→ `tokenFile`) |
| `tls.caEsoRef` | `ca.crt` | `/etc/omcp/datasources/<name>/ca.crt` (→ `caFile`) |

The Secret is mounted at `/etc/omcp/datasources/<name>` and referenced from
`config.yaml` via `*File` keys, so ESO rotation is picked up without a restart.
Without ESO, set `datasources[].existingSecret` to a Secret you manage (same keys).

## Agent → MCP authentication

Optional client-side auth (independent of datasource auth). Disabled by default.

Static bearer tokens via ESO (one Secret per token, mounted at
`/etc/omcp/auth/<name>/token`):

```yaml
auth:
  enabled: true
  static:
    enabled: true
    tokens:
      - name: ci
        eso: { ref: <backend-uuid> }   # → secret key `token`
      # - name: legacy
      #   existingSecret: my-secret     # must contain key `token`
```

OIDC (Authentik / Keycloak — no secret needed, JWKS is public):

```yaml
auth:
  enabled: true
  oidc:
    enabled: true
    issuer: https://authentik.example.com/application/o/observability-mcp/
    audience: https://observability-mcp.intern.tools.averion.zone
    requiredGroups: ["observability-admins"]     # optional
```

Both can be enabled together. Always pair auth with TLS (the ingress below).

## Ingress & security

The transport has **no built-in auth**. `ingress.enabled` defaults to `false`.
When enabled, it uses `ingressClassName: nginx-internal` with streaming-friendly
annotations (long `proxy-read/send-timeout`, buffering off) and optional
ESO-backed basic-auth. Never expose publicly.
