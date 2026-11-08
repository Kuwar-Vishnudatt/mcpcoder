package ghclient

import (
	"context"
	"testing"
)

func TestListOpenIssuesExcludesPullRequests(t *testing.T) {
	// The real-world gotcha this guards: GitHub's Issues API returns pull
	// requests as issues too (a PR IS an issue on GitHub's data model),
	// distinguishable only by PullRequestLinks being set. An agent asking
	// "what needs fixing" must never see open PRs (its own or anyone
	// else's) in that list.
	f := NewFake()
	f.SeedIssue("Fix the bug", "details", nil)
	f.SeedOpenPRAsIssue("Fix the bug (PR)")

	issues, err := f.ListOpenIssues(context.Background(), "o/r", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Title != "Fix the bug" {
		t.Fatalf("expected only the real issue, got %+v", issues)
	}
}

func TestListOpenIssuesFiltersByLabel(t *testing.T) {
	f := NewFake()
	f.SeedIssue("Bug A", "", []string{"bug"})
	f.SeedIssue("Feature B", "", []string{"enhancement"})

	issues, err := f.ListOpenIssues(context.Background(), "o/r", "bug")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Title != "Bug A" {
		t.Fatalf("expected only the labeled issue, got %+v", issues)
	}
}

func TestGetFileReturnsShaForOptimisticConcurrency(t *testing.T) {
	f := NewFake()
	fc := f.SeedFile("main", "x.go", "package main")

	got, err := f.GetFile(context.Background(), "o/r", "main", "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA != fc.SHA || got.Content != "package main" {
		t.Fatalf("unexpected file content: %+v", got)
	}
}

func TestCreateBranchCopiesBaseBranchFiles(t *testing.T) {
	f := NewFake()
	f.SeedFile("main", "x.go", "original")

	if err := f.CreateBranch(context.Background(), "o/r", "main", "fix-1"); err != nil {
		t.Fatal(err)
	}
	fc, err := f.GetFile(context.Background(), "o/r", "fix-1", "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Content != "original" {
		t.Fatalf("expected the new branch to start with base branch content, got %q", fc.Content)
	}
}

func TestCreateBranchIsIdempotentWhenBranchAlreadyExists(t *testing.T) {
	f := NewFake()
	f.SeedFile("main", "x.go", "v1")
	f.CreateBranch(context.Background(), "o/r", "main", "fix-1")
	f.PutFile(context.Background(), "o/r", "fix-1", "x.go", "v2", "msg", f.files["fix-1"]["x.go"].SHA)

	// Re-creating the same branch must be a no-op success, preserving the
	// work already done on it — an agent resuming against an issue it
	// already started must not have its progress wiped.
	if err := f.CreateBranch(context.Background(), "o/r", "main", "fix-1"); err != nil {
		t.Fatal(err)
	}
	fc, _ := f.GetFile(context.Background(), "o/r", "fix-1", "x.go")
	if fc.Content != "v2" {
		t.Fatalf("expected re-creating an existing branch to preserve its content, got %q", fc.Content)
	}
}

func TestPutFileRejectsStaleSHA(t *testing.T) {
	f := NewFake()
	f.SeedFile("main", "x.go", "v1")
	f.CreateBranch(context.Background(), "o/r", "main", "fix-1")
	originalSHA := f.files["fix-1"]["x.go"].SHA

	// Someone else updates the file on this branch first.
	f.PutFile(context.Background(), "o/r", "fix-1", "x.go", "v2-from-elsewhere", "msg", originalSHA)

	// Our update, still carrying the now-stale SHA, must be rejected —
	// the same optimistic-concurrency guard GitHub's real Contents API
	// enforces, so a tool built against the Fake behaves the same way
	// against the real API.
	err := f.PutFile(context.Background(), "o/r", "fix-1", "x.go", "v3-from-us", "msg", originalSHA)
	if err == nil {
		t.Fatal("expected a stale-SHA update to be rejected")
	}
}

func TestOpenPullRequestThenListOpenPullRequests(t *testing.T) {
	f := NewFake()
	pr, err := f.OpenPullRequest(context.Background(), "o/r", "fix-1", "main", "Fix the bug", "Fixes #1")
	if err != nil {
		t.Fatal(err)
	}

	prs, err := f.ListOpenPullRequests(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != pr.Number {
		t.Fatalf("expected the newly opened PR to be listed, got %+v", prs)
	}
}

func TestCommentOnIssueRecordsComment(t *testing.T) {
	f := NewFake()
	iss := f.SeedIssue("Bug", "", nil)
	if err := f.CommentOnIssue(context.Background(), "o/r", iss.Number, "working on it"); err != nil {
		t.Fatal(err)
	}
	comments := f.Comments(iss.Number)
	if len(comments) != 1 || comments[0] != "working on it" {
		t.Fatalf("unexpected comments: %v", comments)
	}
}

func TestGetFileOnUnknownBranchErrors(t *testing.T) {
	f := NewFake()
	if _, err := f.GetFile(context.Background(), "o/r", "does-not-exist", "x.go"); err == nil {
		t.Fatal("expected an error for an unknown branch")
	}
}
