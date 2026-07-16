// Package config loads and validates the observability-mcp server configuration:
// the listen address, logging, a global read-only kill-switch, and the registry
// of observability datasources this instance can query (VictoriaMetrics,
// VictoriaLogs, Prometheus, Alertmanager). Authentication of the AI agent to the
// MCP (static bearer tokens / OIDC) is a separate concern handled by the auth
// package; datasource-side auth (for a vmauth proxy or protected endpoints) is
// expressed per datasource here.
package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Config is the top-level server configuration.
type Config struct {
	ListenAddr        string             `json:"listenAddr"`
	MetricsAddr       string             `json:"metricsAddr"`
	LogLevel          string             `json:"logLevel"`
	ReadOnly          bool               `json:"readOnly"`
	DefaultDatasource string             `json:"defaultDatasource"`
	Datasources       []DatasourceConfig `json:"datasources"`
	Auth              Auth               `json:"auth"`
}

// Datasource types.
const (
	TypeVictoriaMetrics = "victoriametrics"
	TypePrometheus      = "prometheus"
	TypeVictoriaLogs    = "victorialogs"
	TypeAlertmanager    = "alertmanager"
)

// DatasourceConfig describes one observability backend and how to reach it.
type DatasourceConfig struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	URL        string         `json:"url"`
	PathPrefix string         `json:"pathPrefix,omitempty"`
	ReadOnly   bool           `json:"readOnly,omitempty"`
	Timeout    string         `json:"timeout,omitempty"` // Go duration; default 30s
	TLS        DatasourceTLS  `json:"tls,omitempty"`
	Auth       DatasourceAuth `json:"auth,omitempty"`
}

// DatasourceTLS configures TLS trust for an https datasource.
type DatasourceTLS struct {
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
	CAFile                string `json:"caFile,omitempty"`
	CAData                string `json:"caData,omitempty"` // base64 (PEM)
}

// DatasourceAuth is optional datasource-side auth (e.g. a vmauth proxy). At most
// one of Basic/Bearer may be set; omit both for unauthenticated access.
type DatasourceAuth struct {
	Basic  *DatasourceBasicAuth  `json:"basic,omitempty"`
	Bearer *DatasourceBearerAuth `json:"bearer,omitempty"`
}

// DatasourceBasicAuth is HTTP basic auth. Exactly one of Password/PasswordFile.
type DatasourceBasicAuth struct {
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`     // inline, discouraged
	PasswordFile string `json:"passwordFile,omitempty"` // preferred (ESO / rotatable)
}

// DatasourceBearerAuth is a bearer token. Exactly one of Token/TokenFile.
type DatasourceBearerAuth struct {
	Token     string `json:"token,omitempty"`     // inline, discouraged
	TokenFile string `json:"tokenFile,omitempty"` // preferred
}

// Auth configures how the AI agent authenticates to this MCP server (the
// client-side link). It is independent of datasource auth: this decides who may
// talk to the MCP, while datasource auth decides how the MCP reaches a backend.
// When disabled, the transport is unauthenticated and must be protected by the
// deployment (e.g. an OIDC-gated or internal ingress).
type Auth struct {
	Enabled bool       `json:"enabled"`
	Static  AuthStatic `json:"static"`
	OIDC    AuthOIDC   `json:"oidc"`
}

// AuthStatic configures one or more shared bearer tokens.
type AuthStatic struct {
	Enabled bool        `json:"enabled"`
	Tokens  []AuthToken `json:"tokens"`
}

// AuthToken is a single shared bearer token. Exactly one of Token/TokenFile.
type AuthToken struct {
	Name      string `json:"name"`
	Token     string `json:"token,omitempty"`     // inline, discouraged
	TokenFile string `json:"tokenFile,omitempty"` // preferred (ESO / rotatable)
}

// AuthOIDC configures the MCP as an OAuth 2.1 resource server validating JWT
// access tokens from an OIDC provider (Authentik / Keycloak).
type AuthOIDC struct {
	Enabled        bool     `json:"enabled"`
	Issuer         string   `json:"issuer"`
	Audience       string   `json:"audience"`
	JWKSURL        string   `json:"jwksUrl,omitempty"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
	RequiredGroups []string `json:"requiredGroups,omitempty"`
	GroupsClaim    string   `json:"groupsClaim,omitempty"`
	UsernameClaim  string   `json:"usernameClaim,omitempty"`
	// ResourceMetadata controls serving /.well-known/oauth-protected-resource.
	// Defaults to true (enabled) when unset.
	ResourceMetadata *bool `json:"resourceMetadata,omitempty"`
}

// ServeResourceMetadata reports whether the protected-resource-metadata endpoint
// should be served (defaults to true).
func (o AuthOIDC) ServeResourceMetadata() bool {
	return o.ResourceMetadata == nil || *o.ResourceMetadata
}

var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// validTypes is the set of datasource types this server understands.
var validTypes = map[string]bool{
	TypeVictoriaMetrics: true,
	TypePrometheus:      true,
	TypeVictoriaLogs:    true,
	TypeAlertmanager:    true,
}

// IsMetricsType reports whether a datasource type serves the Prometheus query API.
func IsMetricsType(t string) bool { return t == TypeVictoriaMetrics || t == TypePrometheus }

// Load reads config from path (if non-empty), applies OMCP_* environment
// overrides, fills defaults and validates the result.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse config %q: %w", path, err)
		}
	}
	cfg.applyEnv()
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("OMCP_LISTEN_ADDR"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("OMCP_METRICS_ADDR"); v != "" {
		c.MetricsAddr = v
	}
	if v := os.Getenv("OMCP_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("OMCP_DEFAULT_DATASOURCE"); v != "" {
		c.DefaultDatasource = v
	}
	if v := os.Getenv("OMCP_READ_ONLY"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.ReadOnly = b
		}
	}
	if v := os.Getenv("OMCP_AUTH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Auth.Enabled = b
		}
	}
	if v := os.Getenv("OMCP_AUTH_STATIC_TOKEN"); v != "" {
		c.Auth.Static.Enabled = true
		c.Auth.Static.Tokens = append(c.Auth.Static.Tokens, AuthToken{Name: "env", Token: v})
	}
	if v := os.Getenv("OMCP_AUTH_OIDC_ISSUER"); v != "" {
		c.Auth.OIDC.Enabled = true
		c.Auth.OIDC.Issuer = v
	}
	if v := os.Getenv("OMCP_AUTH_OIDC_AUDIENCE"); v != "" {
		c.Auth.OIDC.Audience = v
	}
}

func (c *Config) applyDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = "0.0.0.0:9090"
	}
	if c.MetricsAddr == "" {
		c.MetricsAddr = ":9091" // separate port; set "off" to disable
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	for i := range c.Datasources {
		if c.Datasources[i].Timeout == "" {
			c.Datasources[i].Timeout = "30s"
		}
	}
	// If exactly one datasource is defined and no default is set, use it.
	if c.DefaultDatasource == "" && len(c.Datasources) == 1 {
		c.DefaultDatasource = c.Datasources[0].Name
	}
	if c.Auth.OIDC.GroupsClaim == "" {
		c.Auth.OIDC.GroupsClaim = "groups"
	}
	if c.Auth.OIDC.UsernameClaim == "" {
		c.Auth.OIDC.UsernameClaim = "preferred_username"
	}
}

// Validate enforces the invariants documented on Config/DatasourceConfig.
// It returns the first violation found.
func (c *Config) Validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid logLevel %q (want debug|info|warn|error)", c.LogLevel)
	}
	if len(c.Datasources) == 0 {
		return fmt.Errorf("no datasources configured")
	}

	seen := map[string]bool{}
	for _, ds := range c.Datasources {
		if ds.Name == "" {
			return fmt.Errorf("datasource with empty name")
		}
		if !nameRe.MatchString(ds.Name) {
			return fmt.Errorf("datasource %q: name must be a DNS label (lowercase alphanumeric and '-')", ds.Name)
		}
		if seen[ds.Name] {
			return fmt.Errorf("duplicate datasource name %q", ds.Name)
		}
		seen[ds.Name] = true

		if !validTypes[ds.Type] {
			return fmt.Errorf("datasource %q: invalid type %q (want victoriametrics|prometheus|victorialogs|alertmanager)", ds.Name, ds.Type)
		}
		if ds.URL == "" {
			return fmt.Errorf("datasource %q: url is required", ds.Name)
		}
		u, err := url.Parse(ds.URL)
		if err != nil {
			return fmt.Errorf("datasource %q: invalid url: %w", ds.Name, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("datasource %q: url scheme must be http or https", ds.Name)
		}
		if u.Host == "" {
			return fmt.Errorf("datasource %q: url must include a host", ds.Name)
		}
		if ds.Timeout != "" {
			if _, err := time.ParseDuration(ds.Timeout); err != nil {
				return fmt.Errorf("datasource %q: invalid timeout %q: %w", ds.Name, ds.Timeout, err)
			}
		}
		if ds.TLS.InsecureSkipTLSVerify && (ds.TLS.CAFile != "" || ds.TLS.CAData != "") {
			return fmt.Errorf("datasource %q: insecureSkipTLSVerify must not be combined with a CA", ds.Name)
		}
		if err := ds.Auth.validate(ds.Name); err != nil {
			return err
		}
	}

	if c.DefaultDatasource == "" {
		return fmt.Errorf("defaultDatasource must be set when more than one datasource is configured")
	}
	if !seen[c.DefaultDatasource] {
		return fmt.Errorf("defaultDatasource %q is not one of the configured datasources", c.DefaultDatasource)
	}

	return c.Auth.validate()
}

func (a DatasourceAuth) validate(name string) error {
	if a.Basic != nil && a.Bearer != nil {
		return fmt.Errorf("datasource %q: set at most one of auth.basic or auth.bearer", name)
	}
	if a.Basic != nil {
		if a.Basic.Username == "" {
			return fmt.Errorf("datasource %q: auth.basic.username is required", name)
		}
		if (a.Basic.Password == "") == (a.Basic.PasswordFile == "") {
			return fmt.Errorf("datasource %q: set exactly one of auth.basic.password or passwordFile", name)
		}
	}
	if a.Bearer != nil {
		if (a.Bearer.Token == "") == (a.Bearer.TokenFile == "") {
			return fmt.Errorf("datasource %q: set exactly one of auth.bearer.token or tokenFile", name)
		}
	}
	return nil
}

func (a Auth) validate() error {
	if !a.Enabled {
		return nil
	}
	if !a.Static.Enabled && !a.OIDC.Enabled {
		return fmt.Errorf("auth.enabled is true but neither auth.static nor auth.oidc is enabled")
	}
	if a.Static.Enabled {
		if len(a.Static.Tokens) == 0 {
			return fmt.Errorf("auth.static.enabled is true but no tokens configured")
		}
		names := map[string]bool{}
		for i, t := range a.Static.Tokens {
			if t.Name == "" {
				return fmt.Errorf("auth.static.tokens[%d]: name is required", i)
			}
			if names[t.Name] {
				return fmt.Errorf("auth.static.tokens: duplicate name %q", t.Name)
			}
			names[t.Name] = true
			if (t.Token == "") == (t.TokenFile == "") {
				return fmt.Errorf("auth.static.tokens[%q]: set exactly one of token or tokenFile", t.Name)
			}
		}
	}
	if a.OIDC.Enabled {
		if a.OIDC.Issuer == "" {
			return fmt.Errorf("auth.oidc.enabled is true but issuer is empty")
		}
		if !strings.HasPrefix(a.OIDC.Issuer, "https://") {
			return fmt.Errorf("auth.oidc.issuer must be an https URL")
		}
		if a.OIDC.Audience == "" {
			return fmt.Errorf("auth.oidc.enabled is true but audience is empty")
		}
	}
	return nil
}

// Warnings returns non-fatal advisories (loud but not blocking), e.g. inline
// secrets or insecure TLS. Callers log these at startup.
func (c *Config) Warnings() []string {
	var w []string
	for _, ds := range c.Datasources {
		if ds.TLS.InsecureSkipTLSVerify {
			w = append(w, fmt.Sprintf("datasource %q: insecureSkipTLSVerify=true — TLS verification disabled, do not use in production", ds.Name))
		}
		if ds.Auth.Basic != nil && ds.Auth.Basic.Password != "" {
			w = append(w, fmt.Sprintf("datasource %q: inline basic password is discouraged; prefer passwordFile (ESO/rotatable)", ds.Name))
		}
		if ds.Auth.Bearer != nil && ds.Auth.Bearer.Token != "" {
			w = append(w, fmt.Sprintf("datasource %q: inline bearer token is discouraged; prefer tokenFile (ESO/rotatable)", ds.Name))
		}
	}
	if c.Auth.Static.Enabled {
		for _, t := range c.Auth.Static.Tokens {
			if t.Token != "" {
				w = append(w, fmt.Sprintf("auth.static.tokens[%q]: inline token is discouraged; prefer tokenFile (ESO/rotatable)", t.Name))
			}
		}
	}
	if c.Auth.Enabled {
		w = append(w, "auth is enabled — ensure the transport is TLS-protected (OIDC-gated or internal ingress); bearer tokens over plaintext are insecure")
	}
	return w
}

// DatasourceNames returns the configured datasource names in order.
func (c *Config) DatasourceNames() []string {
	names := make([]string, 0, len(c.Datasources))
	for _, ds := range c.Datasources {
		names = append(names, ds.Name)
	}
	return names
}

// String redacts secrets for safe logging.
func (d DatasourceConfig) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "name=%s type=%s url=%s", d.Name, d.Type, d.URL)
	if d.PathPrefix != "" {
		fmt.Fprintf(&b, " pathPrefix=%s", d.PathPrefix)
	}
	switch {
	case d.Auth.Basic != nil:
		b.WriteString(" auth=basic")
	case d.Auth.Bearer != nil:
		b.WriteString(" auth=bearer")
	default:
		b.WriteString(" auth=none")
	}
	fmt.Fprintf(&b, " readOnly=%t", d.ReadOnly)
	return b.String()
}
