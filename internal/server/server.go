// Package server builds the MCP server this project exposes.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/ghclient"
	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/tools"
)

// New builds an MCP server with every GitHub coding-agent tool registered,
// backed by client.
func New(client ghclient.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "mcpcoder", Version: "v0.1.0"}, nil)
	tools.Register(s, client)
	return s
}
