package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerCommonTools(m *mcp.Server) {
	addTool(m, s, "datasources_list", "List the observability datasources this MCP instance can query, with type, default marker, read-only status and reachability.", s.datasourcesList)
}

func (s *Server) datasourcesList(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	var b strings.Builder
	b.WriteString("Configured datasources:\n")
	def := s.reg.DefaultName()
	for _, ds := range s.reg.All() {
		marker := ""
		if ds.Name == def {
			marker = " (default)"
		}
		status := "reachable"
		if st, err := ds.Ping(ctx); err != nil {
			status = "UNREACHABLE: " + err.Error()
		} else {
			status = st
		}
		fmt.Fprintf(&b, "- %s [%s]%s readOnly=%t — %s\n", ds.Name, ds.Type, marker, ds.ReadOnly, status)
	}
	return textResult(b.String()), nil, nil
}
