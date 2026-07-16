// Package mcpserver wires the observability datasources into MCP tools using the
// Model Context Protocol Go SDK. It owns the datasource registry and the global
// read-only guard, and registers read tools (metrics/logs/alerts queries) plus a
// small number of write tools (Alertmanager silences), all instrumented.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/truepace-io-oss/observability-mcp-server/internal/config"
	"github.com/truepace-io-oss/observability-mcp-server/internal/datasources"
)

// serverVersion is set from main via SetVersion.
var serverVersion = "dev"

// SetVersion records the build version reported to MCP clients.
func SetVersion(v string) { serverVersion = v }

// Server holds the shared dependencies for all tool handlers.
type Server struct {
	reg      *datasources.Registry
	readOnly bool // global kill-switch
}

// New builds a Server from the registry and config.
func New(reg *datasources.Registry, cfg *config.Config) *Server {
	return &Server{reg: reg, readOnly: cfg.ReadOnly}
}

// MCPServer constructs an *mcp.Server with all tools registered.
func (s *Server) MCPServer() *mcp.Server {
	m := mcp.NewServer(&mcp.Implementation{
		Name:    "observability-mcp",
		Version: serverVersion,
	}, nil)

	s.registerCommonTools(m)
	s.registerMetricsTools(m)
	s.registerLogsTools(m)
	s.registerAlertmanagerTools(m)
	return m
}
