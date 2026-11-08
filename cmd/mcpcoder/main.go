// Command mcpcoder runs the MCP server over stdio: wire this up as an MCP
// server in Claude Code, Claude Desktop, or any other MCP client, with a
// GITHUB_TOKEN environment variable set, and a coding agent can list
// issues, read code, and open real pull requests against any repo the
// token has access to.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/ghclient"
	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/server"
)

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	client := ghclient.NewReal(token)
	s := server.New(client)

	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
