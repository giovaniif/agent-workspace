---
name: babysit
description: Drive one crew PR to ready. Fix CI, answer and resolve CodeRabbit threads, re-request review, keep the branch up to date, then run the land gate. Use it when asked to babysit, watch or get a PR green in giovaniif/crew.
---

# Babysit a crew PR

One agent for each PR, in its own worktree. Never babysit in a worktree that you edit for another change.

## Ready means

- The required checks of `main` are green on the PR head: `build`, `tdd`, `mutate` and `CodeRabbit`. Read them from branch protection, not from memory:

  ```sh
  gh api repos/giovaniif/crew/branches/main/protection --jq .required_status_checks.contexts
  ```

- No open review thread. `required_conversation_resolution` is on.
- The branch is up to date with `main`.
- Then run `go run ./scripts/land check <pr>`.

## One pass

1. Read the state:

   ```sh
   gh pr view <pr> --json headRefOid,mergeStateStatus,reviewDecision,statusCheckRollup,isDraft
   ```

2. **CI.** For each failing job, read the log (`gh run view <run> --log-failed`). Fix the cause. Push. Do not re-run a job to get a pass. A flaky test gets its own `test:` PR.
3. **TDD job.** The `tdd` job fails when:
   - a non-merge commit changes tests and production code together (`refactor:` commits and Markdown are exempt)
   - a new or changed test passes on the base branch.

   Fix the commit order (test commit first, then the code). Run `scripts/tdd-check origin/main HEAD` locally on a clean tree before you push.
4. **mutate.** It fails below 80% on `internal/domain` and `internal/app`. Add a test that kills the surviving mutants. Do not lower the bar.
5. **Review threads.** For each CodeRabbit finding:
   - fix it, push, reply in the thread with the commit, and resolve the thread
   - or reply with the reason that you decline it, and resolve the thread.

   List open threads with GraphQL (`reviewThreads(first:100){nodes{id isResolved comments(first:1){nodes{body path}}}}`). Resolve with the `resolveReviewThread` mutation.
6. **CodeRabbit.** After each push, check that its newest review is for the PR head (compare the review commit with `headRefOid`).
   - If it is not, comment `@coderabbitai review`.
   - CodeRabbit is often rate limited. Its comment then gives a wait time. Wait at least that long, and never less than 15 min, then ask one time again.
   - Never poke it while its review is in progress. A new request can cancel the run.
7. **Behind main.** Update the branch on GitHub with a merge commit:

   ```sh
   gh api -X PUT repos/giovaniif/crew/pulls/<pr>/update-branch
   ```

   Then CI runs again. A clean automatic merge can still break the build.
8. **macOS jobs.** All `ci` jobs run on macOS runners. They are slow (15 to 30 min is normal). Wait. Do not cancel and re-run them.

## Gotchas

- `BLOCKED` with green checks almost always means an open thread. An update of the branch does not fix it.
- Removing a requested reviewer through the API can also remove team requests. Put them back.
- An approval can stay on an older head. Always compare the review commit with the head.
- Do not open draft PRs from a loop.
- When the PR is ready, merge it with `go run ./scripts/land merge <pr>` (step 9 of the root `AGENTS.md`). Do not wait for the owner. The owner examines the merged PRs in the daily review (`go run ./scripts/land digest --since 24h`).
- Stacked PRs: when the base PR merges, GitHub changes the base to `main`. Then update the branch.

## When ready

```sh
go run ./scripts/land check <pr>
```

Report the result. The verifier (a fresh agent) posts the verdict with `scripts/land verdict`.
