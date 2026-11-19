package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// MCPSetup are the absolute paths the Settings page puts into the MCP setup
// snippets. The app never edits Claude's configuration: the user runs them.
type MCPSetup struct {
	Executable   string `json:"executable" doc:"Absolute path of the running binary; empty when it is a temporary go run build"`
	ConfigPath   string `json:"config_path" doc:"Absolute path of the config file"`
	ConfigExists bool   `json:"config_exists"`
	DBPath       string `json:"db_path" doc:"Absolute path of the SQLite database in use"`
}

// WithMCPSetup mounts GET /api/mcp/setup.
func WithMCPSetup(v MCPSetup) Option {
	return func(s *server) {
		huma.Register(s.api, op(http.MethodGet, "/api/mcp/setup", "getMCPSetup", "Absolute paths for the MCP setup snippets", "mcp"),
			func(context.Context, *struct{}) (*Body[MCPSetup], error) {
				return &Body[MCPSetup]{Body: v}, nil
			})
	}
}
