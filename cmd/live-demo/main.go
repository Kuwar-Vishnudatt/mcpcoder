// Command live-demo drives the REAL mcpcoder MCP server (spawned as a
// real subprocess over real stdio — the exact transport a real MCP client
// like Claude Code or Claude Desktop uses) against a REAL GitHub
// repository: finds the planted bug's issue, reads the buggy file, fixes
// it, commits it to a new branch, opens a real pull request, and comments
// on the issue linking it.
//
// Requires GITHUB_TOKEN. Mutates real GitHub state (creates a branch and
// a pull request) — not run in CI, and not part of the test suite; this
// is a one-time, by-hand demonstration script, kept here as documentation
// of exactly how a real agent would drive this server.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const repo = "Kuwar-Vishnudatt/mcpcoder-demo-target"

func main() {
	if os.Getenv("GITHUB_TOKEN") == "" {
		log.Fatal("GITHUB_TOKEN is required")
	}

	serverBinPath := "./mcpcoder-server-bin"
	if len(os.Args) > 1 {
		serverBinPath = os.Args[1]
	}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "live-demo-agent", Version: "v0.0.1"}, nil)

	cmd := exec.Command(serverBinPath)
	cmd.Env = os.Environ()
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	must(err)
	defer session.Close()

	fmt.Println("=== 1. list_open_issues ===")
	issuesRes := call(session, "list_open_issues", map[string]any{"repo": repo})
	fmt.Println(issuesRes)

	issueNumber := 1 // the planted demo issue
	fmt.Println("\n=== 2. get_issue ===")
	issueRes := call(session, "get_issue", map[string]any{"repo": repo, "number": issueNumber})
	fmt.Println(issueRes)

	fmt.Println("\n=== 3. get_file (the buggy file) ===")
	fileRes, fileOut := callStructured(session, "get_file", map[string]any{"repo": repo, "ref": "master", "path": "lib/counter.go"})
	fmt.Println(fileRes)
	currentContent := fileOut["content"].(string)
	currentSHA := fileOut["sha"].(string)

	branch := "fix-issue-1-off-by-one"
	fmt.Println("\n=== 4. create_branch ===")
	fmt.Println(call(session, "create_branch", map[string]any{"repo": repo, "base_branch": "master", "new_branch": branch}))

	fixed := fixOffByOne(currentContent)
	fmt.Println("\n--- fixed content ---")
	fmt.Println(fixed)

	fmt.Println("\n=== 5. put_file (commit the fix) ===")
	fmt.Println(call(session, "put_file", map[string]any{
		"repo": repo, "branch": branch, "path": "lib/counter.go", "content": fixed,
		"commit_message": "Fix off-by-one bug in Count (fixes #1)", "expected_sha": currentSHA,
	}))

	fmt.Println("\n=== 6. open_pull_request ===")
	prRes, prOut := callStructured(session, "open_pull_request", map[string]any{
		"repo": repo, "head_branch": branch, "base_branch": "master",
		"title": "Fix off-by-one bug in Count",
		"body":  "Fixes #1. `Count` returned `len(items) - 1` instead of `len(items)`.\n\nOpened by mcpcoder, an MCP coding agent, via its real tool calls over the MCP protocol.",
	})
	fmt.Println(prRes)
	pr := prOut["pull_request"].(map[string]any)
	prURL := pr["URL"].(string)

	fmt.Println("\n=== 7. comment_on_issue ===")
	fmt.Println(call(session, "comment_on_issue", map[string]any{
		"repo": repo, "number": issueNumber, "body": "Opened " + prURL + " to fix this.",
	}))

	fmt.Println("\n=== DONE. Pull request:", prURL, "===")
}

func fixOffByOne(src string) string {
	return replaceOnce(src, "return len(items) - 1", "return len(items)")
}

func replaceOnce(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		log.Fatalf("expected to find %q in file content", old)
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func call(session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	must(err)
	if res.IsError {
		log.Fatalf("%s returned an error: %+v", name, res.Content)
	}
	return res
}

func callStructured(session *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	res := call(session, name, args)
	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		log.Fatalf("%s: expected structured content, got %T", name, res.StructuredContent)
	}
	return res, out
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
