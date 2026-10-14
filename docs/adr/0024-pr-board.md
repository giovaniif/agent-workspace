# ADR 0024: PR board

Status: accepted, 2026-09-30.

## Decision

- **One request per poll.** `adapters/github.Finder` sends a single read-only `gh api graphql` request with one `repository(owner, name)` alias per repo, replacing the `gh pr list` per repo of #16. The port is now `PRs(ctx, repos) (map[repo][]PullRequest, error)`. Owner and name go in as variables, never into the query text. The query has no mutations, and tests use a fake `gh` script.
- **Owner and name** come from `git remote get-url origin`, parsed for https, ssh and scp-style URLs, and cached per repo once found. A repo with no parsable origin is skipped and keeps its PRs. gh's own default-repo choice (for forks with an `upstream` remote) is not used.
- **What is read** for each of a repo's 50 most recently updated PRs (any state): review decision, mergeable state, review threads (first 100, unresolved counted), the last commit's check contexts (first 100), and the last 50 comments and reviews. Merge blockers, ready-to-merge and the bot comment count are derived in `domain`.
- **Bot comments since last push** counts issue comments, reviews and review-thread first comments whose author is a GitHub `Bot`, newer than the head commit's `committedDate`. `pushedDate` is deprecated and often null, so the commit date stands in for the push time.
- **Failing checks** carry a name and a URL: a CheckRun's `detailsUrl`, or a StatusContext's `targetUrl`.
- **Blockers** (`PullRequest.Blockers`), for open PRs only, in this order: checks failing or pending, changes requested or review required, merge conflicts, unresolved threads. Unknown mergeability is not a blocker, since GitHub settles it on a later poll. `ReadyToMerge` is open with no blockers.
- **Pacing** (`domain.NextPRPoll`): the base interval (60 s), a quarter of it while an open PR has checks running, doubled per consecutive failed poll up to 10x. Backoff wins over fast polling. Any error from the finder counts as a failure and any success resets it.
- **`agentws pr <session id or name> [--json]`** reads the daemon's state (no GitHub call) and lists the PRs on the session's worktrees, one entry per PR number. A name must match exactly one session; an ID always wins. The TUI no longer draws the PR board (the sidebar card was removed, see ADR 0014).

## `agentws pr --json`

The shape is pinned by `cmd/crew/testdata/pr.json.golden`. Fields are only ever added.

```json
{
  "session": "s1",
  "name": "Add login",
  "prs": [
    {
      "number": 12,
      "title": "Add login",
      "url": "https://github.com/o/api/pull/12",
      "branch": "feat",
      "state": "OPEN",
      "checks": "failing",
      "failing_checks": [{"name": "test", "url": "https://github.com/o/api/actions/runs/1/job/2"}],
      "review_decision": "CHANGES_REQUESTED",
      "unresolved_threads": 2,
      "bot_comments_since_push": 4,
      "mergeable": "CONFLICTING",
      "ready_to_merge": false,
      "blockers": ["checks failing", "changes requested", "merge conflicts", "2 unresolved threads"]
    }
  ]
}
```

- `state` is `OPEN`, `MERGED` or `CLOSED`. `checks` is `passing`, `pending`, `failing`, or empty when there are none.
- `review_decision` is `APPROVED`, `CHANGES_REQUESTED`, `REVIEW_REQUIRED`, or empty when the repo has no review policy.
- `mergeable` is `MERGEABLE`, `CONFLICTING`, or empty while GitHub has not computed it.
- `failing_checks` and `blockers` are always arrays, never null. `prs` is `[]` for a session with no PR.

## Why

- Per-repo lists cost one request per repo and could not carry threads or check names. One aliased query costs one request however many repos or PRs the daemon tracks.
- Deriving blockers in `domain` keeps the board, the CLI and any later notification rule in agreement.

## Limits

- **No ETags.** ARCHITECTURE.md planned ETag polling, but GraphQL is a POST and GitHub does not answer conditional requests on it. Politeness comes from the single request, the 60 s base, backoff on failure, and fast polling only while checks run. The daemon still publishes a diff only when a PR changed.
- Only the 50 most recently updated PRs per repo are read, and at most 100 threads, 100 checks and 50 comments per PR, to stay under GitHub's 500k node limit for many repos. A PR older than that shows no board until it is updated.
- A repo GitHub cannot resolve is left out of the answer. `gh` exits non-zero when any alias errors, so the adapter reads the answer anyway and fails the poll only when there is no data at all.
- Only `github.com`-style `gh` auth is used: `gh api graphql` runs against gh's default host.
