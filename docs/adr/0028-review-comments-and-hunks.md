# ADR 0028: Review comments, the review prompt, and hunk stage/revert

Status: accepted, 2026-09-30.

## Decision

- **Drafts.** `review.comment` adds a `domain.ReviewComment` to the session's open `ReviewDraft`. The TUI, the nvim plugin and `crew review comment` all send the same `CommentParams`; this replaced the in-memory `DraftComment` from ADR 0029. Drafts are rows in the `review_drafts` table (migration 0005), keyed by `<session>-<unix nanos>`. A session has at most one open or queued draft; a sent one stays as an archive.
- **Prompt format.** `domain.ReviewPrompt` is a lead line, then one numbered item per comment: `<worktree path>:<file>:<start>[-<end>]`, `(removed lines)` when every line was deleted (old-side numbers), the code quoted with `> ` and dedented by its common indent, then the comment. `internal/domain/testdata/review_prompt.golden` pins the exact text. The worktree is its absolute path, so the agent edits the right checkout.
- **Sending.** `review.send` queues the draft. It is pasted (bracketed paste, then Enter) as soon as the session is idle, done or waiting, the same rule as model switches (ADR 0018). A hook that moves the session into one of those states sends a queued draft. Pastes share the switch lock, so they are typed one at a time per pane. A failed paste, or a session that started a turn before the worker got the lock, puts the draft back in the queue. Comments added in the meantime join it, and their own draft row is marked `merged`.
- **Turn link.** The next `UserPromptSubmit` after a send is taken to be that prompt. Its turn snapshot refs are stored on the archived draft (`Turns`).
- **Hunks.** `review.hunk` gets the file and hunk index as the review showed them. `domain.HunkPatch` rebuilds a one-hunk patch, which goes to `git apply --cached` (stage) or `git apply -R` (revert). `git apply` changes nothing unless the whole patch applies, so a hunk that no longer matches the file is refused. Before a revert, the adapter checks the patch with `--check` and writes it to `<git dir>/crew/reverted/<unix nanos>.patch`. Renames, binaries and quoted paths are refused.
- **TUI.** The review pane has a line cursor (`j/k`). `V` marks a range, `c` opens a one-line comment input (`ctrl+j` adds a newline), `S` sends, `s` stages the cursor's hunk, and `x` reverts it after `y`.

## Why

- Reusing the switch rule keeps a single definition of "between tools". A paste during a tool run would land in the tool's input.
- The review diff runs from a base to the working tree, not from the index. So stage and revert rebuild the patch and let `git apply` check it against the real index or file, rather than trusting the base.
- A revert is the one review action that drops work. The patch backup makes it recoverable with `git apply`.

## Rejected

- **Keeping the draft on `domain.Session`:** every session diff would carry every comment to each client, and archives would grow the session row without bound.
- **`git add -p` / `git checkout -p` driven through stdin:** that depends on the order of its prompts and on user config (`interactive.singleKey`). `git apply` takes the exact hunk.
- **Linking a draft by prompt text:** hooks do not carry the full prompt for every harness.

## Consequences

- A draft is stored as sent only after its paste lands, so a daemon stopped mid-paste sends it again after the restart. A stop right after the paste but before that write can send it twice.
- If the user types their own prompt between a send and its hook, the draft links to that turn instead.
- In the branch scope, a hunk's old side is the merge base, not the index. Staging such a hunk fails if the index differs there, and the TUI shows git's error.
- Reverted patches pile up under the git dir; nothing prunes them yet.
- Editing and deleting single comments, and nvim comments, are left to later issues.
