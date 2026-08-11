package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/yyZe0122/yunmengze-agent/internal/mcp"
	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

var nonNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// MCPServerStatus is one configured MCP server (no secrets).
type MCPServerStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
	Error     string `json:"error,omitempty"`
}

// MCPStatusSnapshot is a read-only MCP runtime summary for Gateway/TUI.
type MCPStatusSnapshot struct {
	Enabled bool              `json:"enabled"`
	Total   int               `json:"total"`
	OK      int               `json:"ok"`
	Error   int               `json:"error"`
	Tools   int               `json:"tools"`
	Servers []MCPServerStatus `json:"servers,omitempty"`
}

// MCPRegistry holds live MCP clients for daemon shutdown and status.
type MCPRegistry struct {
	mu      sync.Mutex
	clients []*mcp.Client
	names   []string
	servers []MCPServerStatus
}

// ToolNames returns registered mcp_* tool names.
func (r *MCPRegistry) ToolNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

// Status returns a snapshot (Enabled false when nothing configured/registered).
func (r *MCPRegistry) Status() MCPStatusSnapshot {
	if r == nil {
		return MCPStatusSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.servers) == 0 && len(r.names) == 0 {
		return MCPStatusSnapshot{}
	}
	ok, errN := 0, 0
	for _, s := range r.servers {
		if s.Connected {
			ok++
		} else {
			errN++
		}
	}
	return MCPStatusSnapshot{
		Enabled: true,
		Total:   len(r.servers),
		OK:      ok,
		Error:   errN,
		Tools:   len(r.names),
		Servers: append([]MCPServerStatus(nil), r.servers...),
	}
}

// Close stops all MCP server processes.
func (r *MCPRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.clients {
		if err := c.Close(); err != nil {
			slog.Warn("mcp server close", "component", "mcp", "error", err)
		}
	}
	r.clients = nil
}

type mcpTool struct {
	client      *mcp.Client
	serverName  string
	remoteName  string
	localName   string
	description string
	schema      json.RawMessage
}

func (t *mcpTool) Definition() toolapi.Definition {
	desc := t.description
	if strings.TrimSpace(desc) == "" {
		desc = "MCP tool " + t.remoteName + " from server " + t.serverName
	}
	schema := t.schema
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	// Ensure object schema for broker validation.
	if err := validateInputSchema(schema); err != nil {
		schema = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return toolapi.Definition{
		Name: t.localName, Description: desc,
		Risk: string(policy.RiskR2), DefaultTimeoutMillis: 60000,
		InputSchema: schema,
	}
}

func (t *mcpTool) Authorization(json.RawMessage) (Authorization, error) {
	return Authorization{Capability: t.localName}, nil
}

func (t *mcpTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return t.client.CallTool(ctx, t.remoteName, raw)
}

// RegisterMCP starts configured MCP servers, lists tools, and registers them on the broker.
// Failures for individual servers are logged; successful servers still register.
// Returns registry for shutdown and the list of local tool names.
func RegisterMCP(ctx context.Context, broker *Broker, config providerconfig.MCPConfig) (*MCPRegistry, []string, error) {
	if broker == nil {
		return nil, nil, fmt.Errorf("tool broker is required")
	}
	reg := &MCPRegistry{}
	if len(config.Servers) == 0 {
		return reg, nil, nil
	}
	var names []string
	for serverName, server := range config.Servers {
		serverName = strings.TrimSpace(serverName)
		client, err := mcp.Start(ctx, mcp.ServerConfig{
			Name: serverName, Command: server.Command, Args: server.Args, Env: server.Env,
		})
		if err != nil {
			slog.Error("mcp server start failed", "component", "mcp", "server", serverName, "error", err)
			reg.mu.Lock()
			reg.servers = append(reg.servers, MCPServerStatus{Name: serverName, Connected: false, Error: err.Error()})
			reg.mu.Unlock()
			continue
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			slog.Error("mcp tools/list failed", "component", "mcp", "server", serverName, "error", err)
			_ = client.Close()
			reg.mu.Lock()
			reg.servers = append(reg.servers, MCPServerStatus{Name: serverName, Connected: false, Error: err.Error()})
			reg.mu.Unlock()
			continue
		}
		toolCount := 0
		reg.mu.Lock()
		reg.clients = append(reg.clients, client)
		reg.mu.Unlock()
		for _, desc := range tools {
			local := mcpLocalName(serverName, desc.Name)
			if !toolapi.ValidName(local) {
				slog.Warn("mcp tool name skipped", "component", "mcp", "server", serverName, "tool", desc.Name, "local", local)
				continue
			}
			tool := &mcpTool{
				client: client, serverName: serverName, remoteName: desc.Name,
				localName: local, description: desc.Description, schema: desc.InputSchema,
			}
			if err := broker.Register(tool); err != nil {
				slog.Error("mcp tool register failed", "component", "mcp", "tool", local, "error", err)
				continue
			}
			names = append(names, local)
			toolCount++
			reg.mu.Lock()
			reg.names = append(reg.names, local)
			reg.mu.Unlock()
			slog.Info("mcp tool registered", "component", "mcp", "server", serverName, "tool", local)
		}
		reg.mu.Lock()
		reg.servers = append(reg.servers, MCPServerStatus{
			Name: serverName, Connected: true, ToolCount: toolCount,
		})
		reg.mu.Unlock()
	}
	return reg, names, nil
}

func mcpLocalName(server, remote string) string {
	server = nonNameChars.ReplaceAllString(server, "_")
	remote = nonNameChars.ReplaceAllString(remote, "_")
	server = strings.Trim(server, "_")
	remote = strings.Trim(remote, "_")
	if server == "" {
		server = "server"
	}
	if remote == "" {
		remote = "tool"
	}
	return "mcp_" + server + "_" + remote
}
