# mcpcoder

An MCP server exposing real GitHub coding-agent primitives — list issues,
read a file, create a branch, commit a fix, open a pull request, comment
on the issue — so any MCP client (Claude Code, Claude Desktop, a custom
agent loop) can drive a real coding agent against a real repository. Built
with the official MCP Go SDK and go-github, tested against both a fast
deterministic fake and, once, for real: a live run against a dedicated
demo repository found a real planted bug's issue and opened
[a real pull request](https://github.com/Kuwar-Vishnudatt/mcpcoder-demo-target/pull/2)
with the correct fix.

```
MCP client (e.g. Claude) ──▶ list_open_issues ──▶ get_issue ──▶ get_file
                                                                     │
                                               create_branch ◀──────┘
                                                     │
                                            put_file (the fix)
                                                     │
                                          open_pull_request ──▶ comment_on_issue
```

## Why this project

The 2026 AI Engineer hiring research found "agents" in roughly half of
postings — and the differentiator is building the real capability layer
an agent operates through, not a toy loop that prints what it "would" do.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full reasoning,
including a real GitHub API gotcha this project found and designed
around (the Issues API returns pull requests as issues too, unless you
explicitly filter them out), SHA-based optimistic concurrency on file
writes, and the full live-demonstration writeup.

## Quickstart

```bash
make build
make test        # 10+ tests: unit tests against a Fake GitHub client,
                  # plus the real MCP wire protocol end-to-end
```

Running the server for real:
```bash
export GITHUB_TOKEN=ghp_...
./bin/mcpcoder   # speaks MCP over stdio — wire it up as an MCP server
                  # in Claude Code / Claude Desktop / any MCP client
```

## What's actually proven, concretely

- **A real GitHub API gotcha found and designed around**: GitHub's Issues
  API returns pull requests as issues too; `list_open_issues` filters
  these out explicitly, with a test (`SeedOpenPRAsIssue` +
  `TestListOpenIssuesExcludesPullRequests`) specifically modeling the
  distinction a naive implementation would miss.
- **SHA-based optimistic concurrency on writes**: `get_file` returns a
  `sha`; `put_file` rejects an update whose `sha` doesn't match current
  content — the same guard GitHub's real Contents API enforces, proven
  against both the happy path and the stale-SHA rejection.
- **Branch-creation idempotency**: re-running `create_branch` against a
  branch that already exists is a no-op success that preserves whatever
  work is already on it, not an error — an agent resuming interrupted work
  on an issue shouldn't lose progress or crash.
- **The real MCP wire protocol**, not just the underlying Go functions:
  tests drive a real `mcp.Client` against a real `mcp.Server` over an
  in-memory JSON-RPC transport, including the complete
  list-issue → read-file → branch → commit → open-PR → comment sequence a
  real agent would perform.
- **A real, independently-verified live run**: against a dedicated demo
  repository with one planted bug, the actual server (as a real
  subprocess over real stdio) found the real issue, read the real buggy
  file, fixed it, and opened a real pull request with exactly the right
  one-line diff — confirmed afterward with `gh pr diff` and
  `gh issue view`, not just trusting the demo script's own output.

## Project layout

```
internal/ghclient/   GitHub operations behind one interface: Real (go-github) + Fake (in-memory, for tests)
internal/tools/        typed MCP tools wrapping ghclient, registered on an mcp.Server
internal/server/        wires tools into a server
cmd/mcpcoder/            the stdio MCP server binary
cmd/live-demo/           a by-hand, not-CI driver that ran the real end-to-end demo (see docs/ARCHITECTURE.md)
test/integration/        real MCP wire protocol (in-memory transport) end-to-end tests
docs/ARCHITECTURE.md    design rationale + the full live-demo writeup
```

## What's deliberately out of scope (v1)

See docs/ARCHITECTURE.md — no embedded LLM decision loop (this server is
the capability layer an external agent drives, not an agent itself), no
autonomous webhook-triggered operation, no automatic stale-write retry.
