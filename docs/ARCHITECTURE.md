# Architecture

## What problem this solves

"Agents" was the other term the 2026 AI Engineer hiring research found in
roughly half of postings, alongside "evals." An MCP server is the
standard way an agent gets real-world capability — here, the ability to
read a GitHub issue, inspect the actual code, and open a real pull
request — without the agent's host (Claude Code, Claude Desktop, any MCP
client) needing bespoke integration code for every tool. This project
builds that server for real: the official MCP Go SDK, the real GitHub
API, tested both against a fake (fast, deterministic) and — once — against
an actual GitHub repository with a real planted bug, finding a real issue
and opening a real, inspectable pull request.

## Primitives, not an embedded agent

Every tool here (`internal/tools`) is a primitive: list issues, read a
file, create a branch, write a file, open a PR, comment. None of them
contain "what should the fix be" reasoning. That's a deliberate
boundary — the judgment of *what to change* belongs to whatever LLM is
driving these tools (a human's Claude Code session, an autonomous agent
loop, whatever), not to the server exposing the capability. A server that
embedded its own fix-generation logic would be a much less reusable
building block, and a much harder one to reason about and test, than one
that just exposes correct, safe primitives.

The one exception, and why it lives at the `ghclient` layer instead of as
agent-level logic: **branch-creation idempotency**.
`ghclient.Client.CreateBranch` treats an already-existing branch as a
successful no-op rather than an error — so an agent resuming work on an
issue it already started (a previous run got interrupted, or two agent
runs happen to target the same issue) doesn't lose its progress or
crash on a "reference already exists" error it has no principled way to
recover from on its own. This is infrastructure-level safety, not
business judgment, which is why it belongs below the tool boundary.

## A real gotcha found and designed around: PRs ARE issues

GitHub's REST Issues API returns pull requests as issues too — a PR *is*
an issue, internally, on GitHub's own data model, distinguishable only by
`PullRequestLinks` being non-nil. Without filtering this out,
`list_open_issues` would show an agent its own (or anyone else's) open
pull requests mixed in with things that actually need fixing — exactly
the kind of subtle, easy-to-miss API behavior that would otherwise surface
as "the agent keeps trying to re-fix things that already have an open PR."
`ghclient.Real.ListOpenIssues` filters this explicitly, and
`ghclient.Fake.SeedOpenPRAsIssue` + `TestListOpenIssuesExcludesPullRequests`
exist specifically to keep this filter from regressing — a fake that
didn't model this distinction wouldn't have been able to catch it.

## Optimistic concurrency on file writes

`GetFile` returns a `sha` alongside content; `PutFile` requires that exact
`sha` to update an existing file (empty `sha` means "create new"). This
mirrors GitHub's own Contents API behavior: if the file changed since the
agent read it (a human pushed a commit in between, or two agent runs
raced), the write is rejected rather than silently clobbering whatever
changed it — `ghclient.Fake` models this exact behavior
(`TestPutFileRejectsStaleSHA`) so a tool's correctness can be proven
without needing a live race against the real API to trigger it.

## Why two implementations of `ghclient.Client`, and why tests only use the Fake

`ghclient.Real` (go-github-backed) is what actually runs in production;
`ghclient.Fake` (in-memory, deterministic) is what every automated test
uses — the same Fake-backend pattern used throughout this portfolio
(llmgate, evalbench, evalgate). Hitting the live GitHub API from an
automated test suite would make tests flaky (network, rate limits),
require committing credentials, and risk mutating real repository state
on every CI run — none of which teach you anything about whether the
*tool's logic* is correct that a well-designed fake can't teach you
faster and more reliably.

## The real, live demonstration

Automated tests prove the tool logic and the MCP wire protocol are
correct. They don't, by themselves, prove the whole thing works against
the actual GitHub API with its actual quirks. For that: a small dedicated
public repository, [mcpcoder-demo-target](https://github.com/Kuwar-Vishnudatt/mcpcoder-demo-target),
was created with one deliberately planted bug (`lib.Count` returning
`len(items) - 1`) and a real GitHub issue describing it. `cmd/live-demo`
then drove the **real** `mcpcoder` server — spawned as a real subprocess
over real stdio, the exact transport a real MCP client uses — through the
complete real workflow: `list_open_issues` found the real issue,
`get_issue` read its body, `get_file` read the real buggy source, the fix
was computed, `create_branch` + `put_file` committed it to a real new
branch, `open_pull_request` opened
[a real pull request](https://github.com/Kuwar-Vishnudatt/mcpcoder-demo-target/pull/2)
with the correct one-line diff, and `comment_on_issue` linked it back on
the issue. Independently re-verified afterward via `gh pr diff` and
`gh issue view` (not just trusting the demo script's own stdout).

`cmd/live-demo` is kept in the repo as documentation of exactly how a real
agent drives this server — it requires `GITHUB_TOKEN` and mutates real
GitHub state, so it is deliberately not part of the test suite or any CI
workflow, run by hand only.

## Testing strategy

- `internal/ghclient`: every Fake-backed behavior above — PR filtering,
  label filtering, SHA-based optimistic concurrency (both the happy path
  and the stale-SHA rejection), branch-creation idempotency and that it
  preserves in-progress work on the existing branch, and comment
  recording.
- `test/integration`: the real MCP wire protocol (JSON-RPC over an
  in-memory transport — same codec and dispatch path as a real stdio
  connection, just without an actual OS pipe) driving first a single tool
  call, then the complete list-issue → read-file → branch → commit →
  open-PR → comment workflow in the exact sequence a real agent would
  use — proving the tool schemas, serialization, and dispatch work end to
  end, not just that the underlying Go functions are correct in
  isolation.

## What's deliberately out of scope (v1)

- **An embedded LLM call to decide what the fix should be.** This server
  is intentionally the capability layer only; wiring an LLM decision loop
  on top (reading an issue, deciding what to change, calling these same
  tools) is exactly what a real MCP client (Claude Code, Claude Desktop,
  a custom agent loop) already does — building a second one inside this
  server would duplicate that, not add a new capability.
- **Webhook-triggered automatic operation.** This server waits for an MCP
  client to call it; it doesn't itself watch a repo for new issues and
  act autonomously. Natural v2 extension, not attempted here.
- **Write-conflict resolution beyond rejection.** A stale-SHA `put_file`
  call fails with a clear error; automatically re-reading and retrying
  the write is left to the calling agent, which is better positioned to
  decide whether its intended change is still correct against the new
  content.
