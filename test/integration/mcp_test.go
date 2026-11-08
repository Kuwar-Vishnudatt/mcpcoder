// Package integration drives the MCP server over the REAL MCP wire
// protocol (JSON-RPC over an in-memory transport — the same codec and
// dispatch path a real stdio connection to Claude Code or Claude Desktop
// would use, just without an actual OS pipe), proving the tool schemas,
// serialization, and dispatch genuinely work end to end — not just that
// the underlying Go functions are correct in isolation.
package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/ghclient"
	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/server"
)

// connect starts the real server and a real client connected over an
// in-memory transport pair, returning a ready client session.
func connect(t *testing.T, fake *ghclient.Fake) *mcp.ClientSession {
	t.Helper()
	srv := server.New(fake)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, serverTransport)
	}()
	t.Cleanup(func() {
		<-serverDone
	})

	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTool[Out any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) Out {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s returned an error result: %+v", name, res.Content)
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s result: %v", name, err)
	}
	return out
}

type listIssuesOut struct {
	Issues []ghclient.Issue `json:"issues"`
}

func TestListOpenIssuesOverRealMCPProtocol(t *testing.T) {
	fake := ghclient.NewFake()
	fake.SeedIssue("Off-by-one in parser", "details here", []string{"bug"})
	fake.SeedOpenPRAsIssue("An open PR that must not appear")

	session := connect(t, fake)
	out := callTool[listIssuesOut](t, session, "list_open_issues", map[string]any{"repo": "o/r"})

	if len(out.Issues) != 1 || out.Issues[0].Title != "Off-by-one in parser" {
		t.Fatalf("expected exactly the one real issue over the wire, got %+v", out.Issues)
	}
}

type getFileOut struct {
	Content string `json:"content"`
	SHA     string `json:"sha"`
}

func TestFullFixWorkflowOverRealMCPProtocol(t *testing.T) {
	// This is the capstone: drive the entire "read an issue, read the
	// buggy file, fix it, commit it to a new branch, open a PR" workflow
	// through the real MCP protocol, exactly the sequence of tool calls
	// an LLM-driven coding agent would make.
	fake := ghclient.NewFake()
	issue := fake.SeedIssue("off_by_one returns the wrong count", "It returns len(items)-1 instead of len(items).", []string{"bug"})
	fake.SeedFile("main", "counter.go", "package lib\n\nfunc Count(items []int) int {\n\treturn len(items) - 1\n}\n")

	session := connect(t, fake)

	issues := callTool[listIssuesOut](t, session, "list_open_issues", map[string]any{"repo": "o/r"})
	if len(issues.Issues) != 1 {
		t.Fatalf("expected 1 open issue, got %d", len(issues.Issues))
	}

	file := callTool[getFileOut](t, session, "get_file", map[string]any{"repo": "o/r", "ref": "main", "path": "counter.go"})
	if file.SHA == "" {
		t.Fatal("expected a non-empty sha for optimistic-concurrency updates")
	}

	branchName := "fix-issue-1"
	_ = callTool[struct {
		Created bool `json:"created"`
	}](t, session, "create_branch", map[string]any{"repo": "o/r", "base_branch": "main", "new_branch": branchName})

	fixed := "package lib\n\nfunc Count(items []int) int {\n\treturn len(items)\n}\n"
	_ = callTool[struct {
		OK bool `json:"ok"`
	}](t, session, "put_file", map[string]any{
		"repo": "o/r", "branch": branchName, "path": "counter.go", "content": fixed,
		"commit_message": "Fix off-by-one in Count", "expected_sha": file.SHA,
	})

	type prOut struct {
		PullRequest ghclient.PullRequest `json:"pull_request"`
	}
	pr := callTool[prOut](t, session, "open_pull_request", map[string]any{
		"repo": "o/r", "head_branch": branchName, "base_branch": "main",
		"title": "Fix off-by-one in Count", "body": "Fixes #1",
	})
	if pr.PullRequest.Number == 0 {
		t.Fatal("expected a pull request number to be assigned")
	}

	_ = callTool[struct {
		OK bool `json:"ok"`
	}](t, session, "comment_on_issue", map[string]any{
		"repo": "o/r", "number": issue.Number, "body": "Opened " + pr.PullRequest.URL,
	})

	if len(fake.Comments(issue.Number)) != 1 {
		t.Fatal("expected the comment to be recorded against the issue")
	}

	// Verify the fix actually landed, reading back over the wire again.
	updated := callTool[getFileOut](t, session, "get_file", map[string]any{"repo": "o/r", "ref": branchName, "path": "counter.go"})
	if updated.Content != fixed {
		t.Fatalf("expected the fixed content on the branch, got:\n%s", updated.Content)
	}
}
