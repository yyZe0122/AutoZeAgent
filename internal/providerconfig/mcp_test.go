package providerconfig

import (
	"testing"
)

func TestMCPConfigValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		server  MCPServer
		wantErr bool
	}{
		{name: "stdio ok", server: MCPServer{Command: "npx"}, wantErr: false},
		{name: "stdio missing command", server: MCPServer{Type: "stdio"}, wantErr: true},
		{name: "remote ok", server: MCPServer{Type: "remote", URL: "https://example.com/mcp"}, wantErr: false},
		{name: "http ok", server: MCPServer{Type: "http", URL: "http://127.0.0.1:8080/mcp"}, wantErr: false},
		{name: "sse ok", server: MCPServer{Type: "sse", URL: "https://example.com/sse"}, wantErr: false},
		{name: "url infer remote", server: MCPServer{URL: "https://example.com/mcp"}, wantErr: false},
		{name: "remote missing url", server: MCPServer{Type: "remote"}, wantErr: true},
		{name: "bad scheme", server: MCPServer{Type: "http", URL: "ftp://x"}, wantErr: true},
		{name: "relative url", server: MCPServer{Type: "http", URL: "/mcp"}, wantErr: true},
		{name: "unknown type", server: MCPServer{Type: "websocket", URL: "https://x"}, wantErr: true},
		{name: "local alias", server: MCPServer{Type: "local", Command: "uvx"}, wantErr: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := MCPConfig{Servers: map[string]MCPServer{"s1": tc.server}}
			err := cfg.validate()
			if tc.wantErr && err == nil {
				t.Fatal("want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMCPResolvedType(t *testing.T) {
	t.Parallel()
	typ, err := (MCPServer{Command: "npx"}).ResolvedType()
	if err != nil || typ != MCPTypeStdio {
		t.Fatalf("got %q %v", typ, err)
	}
	typ, err = (MCPServer{URL: "https://x/mcp"}).ResolvedType()
	if err != nil || typ != MCPTypeRemote {
		t.Fatalf("got %q %v", typ, err)
	}
}
