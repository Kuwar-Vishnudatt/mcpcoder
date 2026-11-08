// Package ghclient is the one boundary between this project and the real
// GitHub API — every MCP tool (internal/tools) calls through this
// interface, never go-github directly, so every tool's logic can be unit
// tested against Fake (in-memory, no network) while Real (go-github
// backed) is what actually runs against github.com.
package ghclient

import "context"

type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
	URL    string
}

type PullRequest struct {
	Number     int
	Title      string
	Body       string
	HeadBranch string
	BaseBranch string
	URL        string
}

type FileContent struct {
	Content string // decoded text content
	SHA     string // the blob SHA GitHub needs to accept an update to this exact version
}

// Client is every GitHub operation an MCP tool needs. repo is always
// "owner/name".
type Client interface {
	// ListOpenIssues returns every open issue (pull requests excluded —
	// GitHub's Issues API returns PRs as issues too, which is a real and
	// well-known gotcha; see docs/ARCHITECTURE.md). optionalLabel, if
	// non-empty, filters to issues carrying that label.
	ListOpenIssues(ctx context.Context, repo string, optionalLabel string) ([]Issue, error)

	GetIssue(ctx context.Context, repo string, number int) (Issue, error)

	// GetFile reads a file's content at ref (a branch name, tag, or commit
	// SHA). The returned SHA is required by PutFile to update this exact
	// version (GitHub's Contents API rejects an update whose sha doesn't
	// match current HEAD, which is the API's own optimistic-concurrency
	// guard against clobbering a change made since you read the file).
	GetFile(ctx context.Context, repo, ref, path string) (FileContent, error)

	// ListOpenPullRequests lets a caller check for an existing PR before
	// creating a new branch/PR for the same issue — the dedup an agent
	// should perform itself; see docs/ARCHITECTURE.md for why this isn't
	// baked into OpenPullRequest instead.
	ListOpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error)

	// CreateBranch creates newBranch pointing at baseBranch's current
	// commit. If newBranch already exists, this is a no-op success (not
	// an error) — re-running an agent against an issue it's already
	// started work on should resume, not fail.
	CreateBranch(ctx context.Context, repo, baseBranch, newBranch string) error

	// PutFile creates or updates a file on branch. expectedSHA must be the
	// SHA from a prior GetFile call when updating an existing file (empty
	// when creating a new one) — see GetFile's doc comment.
	PutFile(ctx context.Context, repo, branch, path, content, commitMessage, expectedSHA string) error

	OpenPullRequest(ctx context.Context, repo, headBranch, baseBranch, title, body string) (PullRequest, error)

	CommentOnIssue(ctx context.Context, repo string, number int, body string) error
}
