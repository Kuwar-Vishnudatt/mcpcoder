package ghclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v66/github"
)

// Real is the production Client, backed by the real GitHub API via
// go-github.
type Real struct {
	gh *github.Client
}

func NewReal(token string) *Real {
	return &Real{gh: github.NewClient(nil).WithAuthToken(token)}
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo must be \"owner/name\", got %q", repo)
	}
	return parts[0], parts[1], nil
}

func (r *Real) ListOpenIssues(ctx context.Context, repo, optionalLabel string) ([]Issue, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	opts := &github.IssueListByRepoOptions{
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	if optionalLabel != "" {
		opts.Labels = []string{optionalLabel}
	}

	var out []Issue
	for {
		issues, resp, err := r.gh.Issues.ListByRepo(ctx, owner, name, opts)
		if err != nil {
			return nil, fmt.Errorf("list issues: %w", err)
		}
		for _, iss := range issues {
			// The Issues API returns pull requests as issues too (a PR IS
			// an issue, internally, on GitHub's side) — PullRequestLinks
			// being set is how you tell them apart. An agent asking "what
			// issues need fixing" should never see its own (or anyone
			// else's) open PRs in that list.
			if iss.PullRequestLinks != nil {
				continue
			}
			out = append(out, Issue{
				Number: iss.GetNumber(),
				Title:  iss.GetTitle(),
				Body:   iss.GetBody(),
				Labels: labelNames(iss.Labels),
				URL:    iss.GetHTMLURL(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

func labelNames(labels []*github.Label) []string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.GetName()
	}
	return names
}

func (r *Real) GetIssue(ctx context.Context, repo string, number int) (Issue, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return Issue{}, err
	}
	iss, _, err := r.gh.Issues.Get(ctx, owner, name, number)
	if err != nil {
		return Issue{}, fmt.Errorf("get issue #%d: %w", number, err)
	}
	return Issue{
		Number: iss.GetNumber(), Title: iss.GetTitle(), Body: iss.GetBody(),
		Labels: labelNames(iss.Labels), URL: iss.GetHTMLURL(),
	}, nil
}

func (r *Real) GetFile(ctx context.Context, repo, ref, path string) (FileContent, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return FileContent{}, err
	}
	fc, _, _, err := r.gh.Repositories.GetContents(ctx, owner, name, path, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return FileContent{}, fmt.Errorf("get file %s@%s: %w", path, ref, err)
	}
	if fc == nil {
		return FileContent{}, fmt.Errorf("get file %s@%s: path is a directory, not a file", path, ref)
	}
	content, err := fc.GetContent()
	if err != nil {
		return FileContent{}, fmt.Errorf("decode file %s@%s: %w", path, ref, err)
	}
	return FileContent{Content: content, SHA: fc.GetSHA()}, nil
}

func (r *Real) ListOpenPullRequests(ctx context.Context, repo string) ([]PullRequest, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	opts := &github.PullRequestListOptions{State: "open", ListOptions: github.ListOptions{PerPage: 100}}

	var out []PullRequest
	for {
		prs, resp, err := r.gh.PullRequests.List(ctx, owner, name, opts)
		if err != nil {
			return nil, fmt.Errorf("list pull requests: %w", err)
		}
		for _, pr := range prs {
			out = append(out, PullRequest{
				Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
				HeadBranch: pr.GetHead().GetRef(), BaseBranch: pr.GetBase().GetRef(), URL: pr.GetHTMLURL(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

func (r *Real) CreateBranch(ctx context.Context, repo, baseBranch, newBranch string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	baseRef, _, err := r.gh.Git.GetRef(ctx, owner, name, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("get base branch %s: %w", baseBranch, err)
	}

	newRef := &github.Reference{
		Ref:    github.String("refs/heads/" + newBranch),
		Object: &github.GitObject{SHA: baseRef.Object.SHA},
	}
	_, _, err = r.gh.Git.CreateRef(ctx, owner, name, newRef)
	if err != nil {
		if strings.Contains(err.Error(), "Reference already exists") {
			// Re-running against an issue the agent already started work
			// on should resume from the existing branch, not fail.
			return nil
		}
		return fmt.Errorf("create branch %s: %w", newBranch, err)
	}
	return nil
}

func (r *Real) PutFile(ctx context.Context, repo, branch, path, content, commitMessage, expectedSHA string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	opts := &github.RepositoryContentFileOptions{
		Message: github.String(commitMessage),
		Content: []byte(content),
		Branch:  github.String(branch),
	}

	if expectedSHA != "" {
		opts.SHA = github.String(expectedSHA)
		if _, _, err := r.gh.Repositories.UpdateFile(ctx, owner, name, path, opts); err != nil {
			return fmt.Errorf("update file %s on %s: %w", path, branch, err)
		}
		return nil
	}

	if _, _, err := r.gh.Repositories.CreateFile(ctx, owner, name, path, opts); err != nil {
		return fmt.Errorf("create file %s on %s: %w", path, branch, err)
	}
	return nil
}

func (r *Real) OpenPullRequest(ctx context.Context, repo, headBranch, baseBranch, title, body string) (PullRequest, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return PullRequest{}, err
	}
	pr, _, err := r.gh.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(headBranch),
		Base:  github.String(baseBranch),
		Body:  github.String(body),
	})
	if err != nil {
		return PullRequest{}, fmt.Errorf("open pull request: %w", err)
	}
	return PullRequest{
		Number: pr.GetNumber(), Title: pr.GetTitle(), Body: pr.GetBody(),
		HeadBranch: pr.GetHead().GetRef(), BaseBranch: pr.GetBase().GetRef(), URL: pr.GetHTMLURL(),
	}, nil
}

func (r *Real) CommentOnIssue(ctx context.Context, repo string, number int, body string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, _, err = r.gh.Issues.CreateComment(ctx, owner, name, number, &github.IssueComment{Body: github.String(body)})
	if err != nil {
		return fmt.Errorf("comment on issue #%d: %w", number, err)
	}
	return nil
}
