package ghclient

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-memory Client for tests — no network, fully deterministic,
// and able to simulate the exact GitHub quirks that matter to this
// project's tools: pull requests appearing in the issues list unless
// filtered, and branch-already-exists being a no-op rather than an error.
type Fake struct {
	mu sync.Mutex

	nextIssueNum int
	nextPRNum    int

	issues  map[int]Issue
	isPR    map[int]bool // issues that are "really" pull requests, per GitHub's own data model
	prs     map[int]PullRequest
	files   map[string]map[string]FileContent // branch -> path -> content (SHA is a counter-based fake blob id)
	shaSeed int

	comments map[int][]string
}

func NewFake() *Fake {
	return &Fake{
		nextIssueNum: 1,
		nextPRNum:    1,
		issues:       map[int]Issue{},
		isPR:         map[int]bool{},
		prs:          map[int]PullRequest{},
		files:        map[string]map[string]FileContent{},
		comments:     map[int][]string{},
	}
}

// SeedIssue adds a fake open issue for tests to discover.
func (f *Fake) SeedIssue(title, body string, labels []string) Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nextIssueNum
	f.nextIssueNum++
	iss := Issue{Number: n, Title: title, Body: body, Labels: labels, URL: fmt.Sprintf("https://fake/issues/%d", n)}
	f.issues[n] = iss
	return iss
}

// SeedOpenPRAsIssue seeds an open pull request that ALSO shows up in
// GitHub's issues data model — this is what lets a test prove
// ListOpenIssues correctly filters PRs out, the way a naive
// implementation (forgetting PullRequestLinks) would not.
func (f *Fake) SeedOpenPRAsIssue(title string) Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nextIssueNum
	f.nextIssueNum++
	f.isPR[n] = true
	return Issue{Number: n, Title: title}
}

// SeedFile adds a file's content on a branch (creating the branch's file
// map if needed), with a fresh fake SHA.
func (f *Fake) SeedFile(branch, path, content string) FileContent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.putFileLocked(branch, path, content)
}

// SeedBranch creates an empty branch with no files — for tests that need
// a base branch to exist without caring about its contents.
func (f *Fake) SeedBranch(branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files[branch] == nil {
		f.files[branch] = map[string]FileContent{}
	}
}

func (f *Fake) putFileLocked(branch, path, content string) FileContent {
	if f.files[branch] == nil {
		f.files[branch] = map[string]FileContent{}
	}
	f.shaSeed++
	fc := FileContent{Content: content, SHA: fmt.Sprintf("fakesha%d", f.shaSeed)}
	f.files[branch][path] = fc
	return fc
}

func (f *Fake) ListOpenIssues(_ context.Context, _ string, optionalLabel string) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Issue
	for n, iss := range f.issues {
		if f.isPR[n] {
			continue
		}
		if optionalLabel != "" && !containsLabel(iss.Labels, optionalLabel) {
			continue
		}
		out = append(out, iss)
	}
	return out, nil
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func (f *Fake) GetIssue(_ context.Context, _ string, number int) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss, ok := f.issues[number]
	if !ok {
		return Issue{}, fmt.Errorf("issue #%d not found", number)
	}
	return iss, nil
}

func (f *Fake) GetFile(_ context.Context, _ string, ref, path string) (FileContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	branchFiles, ok := f.files[ref]
	if !ok {
		return FileContent{}, fmt.Errorf("branch %q not found", ref)
	}
	fc, ok := branchFiles[path]
	if !ok {
		return FileContent{}, fmt.Errorf("file %q not found on %q", path, ref)
	}
	return fc, nil
}

func (f *Fake) ListOpenPullRequests(_ context.Context, _ string) ([]PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []PullRequest
	for _, pr := range f.prs {
		out = append(out, pr)
	}
	return out, nil
}

func (f *Fake) CreateBranch(_ context.Context, _ string, baseBranch, newBranch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.files[newBranch]; exists {
		return nil // already exists: no-op success, matching the real client's behavior
	}
	base, ok := f.files[baseBranch]
	if !ok {
		return fmt.Errorf("base branch %q not found", baseBranch)
	}
	copied := make(map[string]FileContent, len(base))
	for k, v := range base {
		copied[k] = v
	}
	f.files[newBranch] = copied
	return nil
}

func (f *Fake) PutFile(_ context.Context, _ string, branch, path, content, _ string, expectedSHA string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files[branch] == nil {
		return fmt.Errorf("branch %q not found", branch)
	}
	if expectedSHA != "" {
		current, ok := f.files[branch][path]
		if !ok || current.SHA != expectedSHA {
			return fmt.Errorf("sha mismatch updating %s on %s: file changed since it was read", path, branch)
		}
	}
	f.putFileLocked(branch, path, content)
	return nil
}

func (f *Fake) OpenPullRequest(_ context.Context, _ string, headBranch, baseBranch, title, body string) (PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nextPRNum
	f.nextPRNum++
	pr := PullRequest{Number: n, Title: title, Body: body, HeadBranch: headBranch, BaseBranch: baseBranch, URL: fmt.Sprintf("https://fake/pulls/%d", n)}
	f.prs[n] = pr
	return pr, nil
}

func (f *Fake) CommentOnIssue(_ context.Context, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.issues[number]; !ok {
		return fmt.Errorf("issue #%d not found", number)
	}
	f.comments[number] = append(f.comments[number], body)
	return nil
}

// Comments returns every comment posted on an issue, for test assertions.
func (f *Fake) Comments(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments[number]...)
}
