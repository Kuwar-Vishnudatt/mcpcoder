// Package tools wires internal/ghclient's GitHub operations into typed
// MCP tools: the primitives an LLM-driven coding agent calls to read
// issues, inspect code, and open a pull request. Every tool here is
// intentionally a primitive — read this, write that, open a PR — with no
// embedded "what should the fix be" reasoning; that judgment belongs to
// the agent driving these tools, not the server exposing them. See
// docs/ARCHITECTURE.md for the one deliberate exception to that rule
// (branch-creation idempotency) and why it's handled at the ghclient
// layer rather than here.
package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kuwar-Vishnudatt/mcpcoder/internal/ghclient"
)

// Register adds every tool in this package to server, backed by client.
func Register(server *mcp.Server, client ghclient.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_open_issues",
		Description: "List open issues in a GitHub repository, optionally filtered by label. Never includes pull requests.",
	}, listOpenIssuesHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_issue",
		Description: "Get the full title and body of one issue by number.",
	}, getIssueHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_file",
		Description: "Read a file's content at a given branch, tag, or commit. Returns a sha required to later update this exact version.",
	}, getFileHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_open_pull_requests",
		Description: "List open pull requests — call this before creating a new branch/PR for an issue, to avoid opening a duplicate.",
	}, listOpenPullRequestsHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_branch",
		Description: "Create a new branch from a base branch. Safe to call again for a branch that already exists (no-op success) — use this to resume work on an issue already started.",
	}, createBranchHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "put_file",
		Description: "Create or update a file's content on a branch. Pass expected_sha (from a prior get_file call) when updating an existing file; omit it when creating a new one.",
	}, putFileHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "open_pull_request",
		Description: "Open a pull request from a head branch into a base branch.",
	}, openPullRequestHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "comment_on_issue",
		Description: "Post a comment on an issue — e.g. to link the pull request that addresses it.",
	}, commentOnIssueHandler(client))
}

type ListOpenIssuesInput struct {
	Repo  string `json:"repo" jsonschema:"the repository as owner/name"`
	Label string `json:"label,omitempty" jsonschema:"optional label to filter by"`
}
type ListOpenIssuesOutput struct {
	Issues []ghclient.Issue `json:"issues"`
}

func listOpenIssuesHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, ListOpenIssuesInput) (*mcp.CallToolResult, ListOpenIssuesOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListOpenIssuesInput) (*mcp.CallToolResult, ListOpenIssuesOutput, error) {
		issues, err := client.ListOpenIssues(ctx, in.Repo, in.Label)
		if err != nil {
			return nil, ListOpenIssuesOutput{}, err
		}
		return nil, ListOpenIssuesOutput{Issues: issues}, nil
	}
}

type GetIssueInput struct {
	Repo   string `json:"repo" jsonschema:"the repository as owner/name"`
	Number int    `json:"number" jsonschema:"the issue number"`
}
type GetIssueOutput struct {
	Issue ghclient.Issue `json:"issue"`
}

func getIssueHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, GetIssueInput) (*mcp.CallToolResult, GetIssueOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetIssueInput) (*mcp.CallToolResult, GetIssueOutput, error) {
		issue, err := client.GetIssue(ctx, in.Repo, in.Number)
		if err != nil {
			return nil, GetIssueOutput{}, err
		}
		return nil, GetIssueOutput{Issue: issue}, nil
	}
}

type GetFileInput struct {
	Repo string `json:"repo" jsonschema:"the repository as owner/name"`
	Ref  string `json:"ref" jsonschema:"branch name, tag, or commit sha"`
	Path string `json:"path" jsonschema:"file path within the repository"`
}
type GetFileOutput struct {
	Content string `json:"content"`
	SHA     string `json:"sha" jsonschema:"pass this back to put_file's expected_sha to update this exact version"`
}

func getFileHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, GetFileInput) (*mcp.CallToolResult, GetFileOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetFileInput) (*mcp.CallToolResult, GetFileOutput, error) {
		fc, err := client.GetFile(ctx, in.Repo, in.Ref, in.Path)
		if err != nil {
			return nil, GetFileOutput{}, err
		}
		return nil, GetFileOutput{Content: fc.Content, SHA: fc.SHA}, nil
	}
}

type ListOpenPullRequestsInput struct {
	Repo string `json:"repo" jsonschema:"the repository as owner/name"`
}
type ListOpenPullRequestsOutput struct {
	PullRequests []ghclient.PullRequest `json:"pull_requests"`
}

func listOpenPullRequestsHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, ListOpenPullRequestsInput) (*mcp.CallToolResult, ListOpenPullRequestsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListOpenPullRequestsInput) (*mcp.CallToolResult, ListOpenPullRequestsOutput, error) {
		prs, err := client.ListOpenPullRequests(ctx, in.Repo)
		if err != nil {
			return nil, ListOpenPullRequestsOutput{}, err
		}
		return nil, ListOpenPullRequestsOutput{PullRequests: prs}, nil
	}
}

type CreateBranchInput struct {
	Repo       string `json:"repo" jsonschema:"the repository as owner/name"`
	BaseBranch string `json:"base_branch" jsonschema:"the existing branch to branch from"`
	NewBranch  string `json:"new_branch" jsonschema:"the new branch's name"`
}
type CreateBranchOutput struct {
	Created bool `json:"created" jsonschema:"true if newly created, false if it already existed"`
}

func createBranchHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, CreateBranchInput) (*mcp.CallToolResult, CreateBranchOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateBranchInput) (*mcp.CallToolResult, CreateBranchOutput, error) {
		if in.BaseBranch == "" || in.NewBranch == "" {
			return nil, CreateBranchOutput{}, fmt.Errorf("base_branch and new_branch are required")
		}
		if err := client.CreateBranch(ctx, in.Repo, in.BaseBranch, in.NewBranch); err != nil {
			return nil, CreateBranchOutput{}, err
		}
		return nil, CreateBranchOutput{Created: true}, nil
	}
}

type PutFileInput struct {
	Repo          string `json:"repo" jsonschema:"the repository as owner/name"`
	Branch        string `json:"branch" jsonschema:"the branch to commit to"`
	Path          string `json:"path" jsonschema:"file path within the repository"`
	Content       string `json:"content" jsonschema:"the full new file content"`
	CommitMessage string `json:"commit_message" jsonschema:"the commit message"`
	ExpectedSHA   string `json:"expected_sha,omitempty" jsonschema:"required when updating an existing file; omit when creating a new one"`
}
type PutFileOutput struct {
	OK bool `json:"ok"`
}

func putFileHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, PutFileInput) (*mcp.CallToolResult, PutFileOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in PutFileInput) (*mcp.CallToolResult, PutFileOutput, error) {
		if in.CommitMessage == "" {
			return nil, PutFileOutput{}, fmt.Errorf("commit_message is required")
		}
		if err := client.PutFile(ctx, in.Repo, in.Branch, in.Path, in.Content, in.CommitMessage, in.ExpectedSHA); err != nil {
			return nil, PutFileOutput{}, err
		}
		return nil, PutFileOutput{OK: true}, nil
	}
}

type OpenPullRequestInput struct {
	Repo       string `json:"repo" jsonschema:"the repository as owner/name"`
	HeadBranch string `json:"head_branch" jsonschema:"the branch containing the fix"`
	BaseBranch string `json:"base_branch" jsonschema:"the branch to merge into"`
	Title      string `json:"title" jsonschema:"the pull request title"`
	Body       string `json:"body" jsonschema:"the pull request description, e.g. referencing the issue it fixes"`
}
type OpenPullRequestOutput struct {
	PullRequest ghclient.PullRequest `json:"pull_request"`
}

func openPullRequestHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, OpenPullRequestInput) (*mcp.CallToolResult, OpenPullRequestOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in OpenPullRequestInput) (*mcp.CallToolResult, OpenPullRequestOutput, error) {
		if in.Title == "" {
			return nil, OpenPullRequestOutput{}, fmt.Errorf("title is required")
		}
		pr, err := client.OpenPullRequest(ctx, in.Repo, in.HeadBranch, in.BaseBranch, in.Title, in.Body)
		if err != nil {
			return nil, OpenPullRequestOutput{}, err
		}
		return nil, OpenPullRequestOutput{PullRequest: pr}, nil
	}
}

type CommentOnIssueInput struct {
	Repo   string `json:"repo" jsonschema:"the repository as owner/name"`
	Number int    `json:"number" jsonschema:"the issue number"`
	Body   string `json:"body" jsonschema:"the comment text"`
}
type CommentOnIssueOutput struct {
	OK bool `json:"ok"`
}

func commentOnIssueHandler(client ghclient.Client) func(context.Context, *mcp.CallToolRequest, CommentOnIssueInput) (*mcp.CallToolResult, CommentOnIssueOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CommentOnIssueInput) (*mcp.CallToolResult, CommentOnIssueOutput, error) {
		if err := client.CommentOnIssue(ctx, in.Repo, in.Number, in.Body); err != nil {
			return nil, CommentOnIssueOutput{}, err
		}
		return nil, CommentOnIssueOutput{OK: true}, nil
	}
}
