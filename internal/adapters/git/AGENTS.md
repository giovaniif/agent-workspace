# internal/adapters/git

The `git` CLI through exec. Do not use a Go git library: only the CLI handles worktrees, sparse checkouts and user config correctly. Tests build real repos in temp dirs with `GIT_CONFIG_GLOBAL=/dev/null`. Thus the git config of the user never gets into the tests.

- **Repo facts** (`RepoInspector`, a maximum of 4 repos at the same time):
  - the default branch from `origin/HEAD` (empty if there is no `origin/HEAD`)
  - the current branch (empty when detached)
  - the count of changed files.

  They come from one `git status --porcelain=v2 --branch -z` and one `git symbolic-ref` for each repo.
- **Worktrees**: `git worktree list --porcelain -z`, a maximum of 4 at the same time (scan rules in [internal/daemon/](../../daemon/AGENTS.md)).
- **Cleanup facts**: one `git status`, the `origin/HEAD` lookup and `git merge-base --is-ancestor` for each worktree, a maximum of 4 at the same time.

## Review

See [ADR 0023](../../../docs/adr/0023-review-pane.md). The adapter parses diffs from the `git diff` output. It does not calculate them in Go.

- **Scopes.** `last_turn` diffs from the newest turn snapshot of the session. `uncommitted` diffs from `HEAD`. `branch` diffs from the merge base with `origin/<default>`. All scopes end at the working tree, with untracked files. The adapter takes the working tree as a tree from a temp copy of the index (`Review`). Thus it never touches the real index.
- **Turns.** Snapshots are at `refs/agentws/turns/<session>/<worktree key>/<n>`. Only the newest for each session and worktree stays. They never show in `git branch`.
- **Hunks** ([ADR 0028](../../../docs/adr/0028-review-comments-and-hunks.md)). Stage is `git apply --cached`, and revert is `git apply -R`. Both apply a patch that the adapter builds again from the shown hunk. A revert first writes the patch to `<git dir>/agentws/reverted/`. Viewed marks are stored for each worktree, path and blob. They reset when the file changes. Tests (`-run 'ReviewStage|ReviewRevert'`) compare against `git add -p` and `git checkout -p` in temp repos.
- **Budget.** `BenchmarkReviewOpen50Files` (`-tags integration`) fails if a cold 50-file, 3,000-line review takes more than 300 ms (about 60 ms on an M3).
