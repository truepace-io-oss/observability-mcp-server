// Package e2e boots the full observability-mcp HTTP server (with static-token
// auth) against fake VictoriaMetrics / VictoriaLogs / Alertmanager backends and
// drives it with a real MCP client over the streamable HTTP transport.
package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/truepace-io-oss/observability-mcp-server/internal/auth"
	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
	"github.com/truepace-io-oss/observability-mcp-server/internal/mcpserver"
)

const staticToken = "s3cret-token"

// fakeBackends returns one httptest server multiplexing VM, logs and AM responses.
func fakeBackends(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	// VictoriaMetrics / Prometheus query API (also used by Ping via vector(1)).
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"api"},"value":[1700000000,"1"]}]}}`))
	})
	// VictoriaLogs.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/select/logsql/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"_time\":\"2026-01-01T00:00:00Z\",\"_msg\":\"boom\"}\n"))
	})
	// Alertmanager.
	mux.HandleFunc("/api/v2/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"cluster":{"status":"ready"}}`))
	})
	mux.HandleFunc("/api/v2/alerts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"labels":{"alertname":"HighCPU"},"status":{"state":"active"}}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// startServer assembles the MCP HTTP server exactly like main: /mcp behind auth.
func startServer(t *testing.T, backend string) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		LogLevel:          "info",
		DefaultDatasource: "vm",
		Datasources: []config.DatasourceConfig{
			{Name: "vm", Type: "victoriametrics", URL: backend, Timeout: "10s"},
			{Name: "logs", Type: "victorialogs", URL: backend, Timeout: "10s"},
			{Name: "am", Type: "alertmanager", URL: backend, ReadOnly: true, Timeout: "10s"},
		},
		Auth: config.Auth{
			Enabled: true,
			Static:  config.AuthStatic{Enabled: true, Tokens: []config.AuthToken{{Name: "t", Token: staticToken}}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	reg, err := datasources.Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := mcpserver.New(reg, cfg)
	authn, err := auth.Build(context.Background(), cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	mcpSrv := srv.MCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", authn.Middleware(handler))
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

// bearerRT adds a static bearer token to every request.
type bearerRT struct {
	next  http.RoundTripper
	token string
}

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.next.RoundTrip(r)
}

func connect(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "test"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:             endpoint + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{next: http.DefaultTransport, token: token}},
		DisableStandaloneSSE: true,
	}
	sess, err := c.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func callText(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestE2E_Tools(t *testing.T) {
	backend := fakeBackends(t)
	server := startServer(t, backend.URL)
	sess := connect(t, server.URL, staticToken)

	// Tool surface is present.
	ltr, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range ltr.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"datasources_list", "metrics_query", "logs_query", "alertmanager_alerts", "alertmanager_silence_create"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}

	if got := resultText(callText(t, sess, "metrics_query", map[string]any{"query": "up"})); !strings.Contains(got, "up") {
		t.Errorf("metrics_query: %q", got)
	}
	if got := resultText(callText(t, sess, "logs_query", map[string]any{"datasource": "logs", "query": "*"})); !strings.Contains(got, "boom") {
		t.Errorf("logs_query: %q", got)
	}
	if got := resultText(callText(t, sess, "alertmanager_alerts", map[string]any{"datasource": "am"})); !strings.Contains(got, "HighCPU") {
		t.Errorf("alertmanager_alerts: %q", got)
	}
	if got := resultText(callText(t, sess, "datasources_list", map[string]any{})); !strings.Contains(got, "vm") {
		t.Errorf("datasources_list: %q", got)
	}
}

func TestE2E_ReadOnlyBlocksWrite(t *testing.T) {
	backend := fakeBackends(t)
	server := startServer(t, backend.URL)
	sess := connect(t, server.URL, staticToken)

	res := callText(t, sess, "alertmanager_silence_create", map[string]any{
		"datasource": "am",
		"matchers":   []map[string]any{{"name": "alertname", "value": "HighCPU"}},
		"createdBy":  "e2e",
		"comment":    "test",
	})
	if !res.IsError || !strings.Contains(strings.ToLower(resultText(res)), "readonly") {
		t.Fatalf("expected read-only block, got IsError=%v text=%q", res.IsError, resultText(res))
	}
}

func TestE2E_AuthGate(t *testing.T) {
	backend := fakeBackends(t)
	server := startServer(t, backend.URL)

	// No token → connection/initialize must fail (401).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "test"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRT{next: http.DefaultTransport, token: ""}},
		DisableStandaloneSSE: true,
	}
	if sess, err := c.Connect(ctx, tr, nil); err == nil {
		_ = sess.Close()
		t.Fatal("expected unauthenticated connect to fail")
	}
}
