# ADR 0053: Mates in Slack

Status: accepted, 2026-10-14. Epic #377. Issues #379, #381, #382, #384, #385, #386, #387 and #388 implement it.

## Context

The owner wants to give work to crew from Slack. Other people on the team also want to ask crew for help in a Slack thread. A **mate** is a named personality of crew in Slack. Each mate has its own prompt, harness, model and repos.

What exists:

- `session.new` starts a session in its own worktree (ADR 0050 uses it for automations).
- The daemon runs all the time on the owner's server. It already sends push notifications (web push) to the PWA and the Mac app.
- `crew hook` gets the Claude and Codex hook events, `PreToolUse` included. The hook process must be fast (ARCHITECTURE.md budgets).
- The store is sqlite (ADR 0004). Global settings are in `$CREW_HOME/config.toml`.

Slack text comes from many people. Thus it is not trusted. A prompt cannot keep a dangerous action safe, because text in the thread can change what the agent does (prompt injection).

## Decision

### One Slack app for each mate

- Each mate is its own Slack app. Thus it has its own @handle, name and avatar in Slack.
- The app uses Socket Mode. The daemon opens the connection to Slack. Thus the server needs no public URL and no open port.
- The daemon on the owner's server runs all the mates of that owner. Each person who wants mates runs them on their own crew server.

### Self-service setup

- `crew mate add <name>` makes a Slack app manifest for the mate. The manifest has the bot user, the scopes, the events and Socket Mode on. The PWA and the Mac app have the same function.
- The command shows the api.slack.com "create new app from manifest" link with the manifest in it, and opens it when it can.
- The user installs the app in the workspace and pastes two tokens: the bot token (`xoxb-`) and the app-level token (`xapp-`, scope `connections:write`).
- crew keeps the tokens as secrets in a file with mode 600 under `$CREW_HOME/secrets/`. They are never in `config.toml`, never in a log, never in an RPC result and never in a `serve` response. A client can only set or remove a token. It cannot read it.
- The app is not in the Slack Marketplace. Thus no Slack review is necessary. A company workspace can require an admin to approve the install. The docs tell the user this.

### The owner

- The owner of a mate is the person who installed it: the person who ran `crew mate add` and installed the Slack app for that crew server. The owner is not a fixed user in crew.
- Setup records the Slack user ID of the installer at install time. After the tokens are set, crew shows a one-time code. The installer sends the code to the mate in a direct message. The daemon takes the user ID from that Slack event and writes it to `owner`. The code expires after 10 minutes.
- Thus the owner ID comes from a Slack fact, not from text that a user types.
- Every owner check uses this ID. To change the owner, the user runs `crew mate claim <name>` again on the server.

### The mate definition

A mate is a `[[mate]]` table in `config.toml`:

| Field | Meaning |
|---|---|
| `name` | The key. It is unique. |
| `prompt` | The personality and the instructions. |
| `agent` | `claude` or `codex`. |
| `model`, `effort` | Optional. Empty values use `[defaults.<harness>]`. |
| `repos` | The repos that the mate can work in. |
| `owner` | The Slack user ID of the owner, for example `U012ABCDEF`. Setup writes it (see "The owner"). |
| `enabled` | The default is true. |

The validation is a pure function in `domain` with table tests. The tokens are not in the definition.

### A thread is a session

- Anyone in the workspace can call a mate. A mention of the mate starts a crew session in a new worktree, or continues the session of that thread.
- The daemon records the link of the thread (team, channel, thread timestamp, mate) to the session in sqlite.
- The first prompt has the mate prompt, the text of the thread and the name of the person who asked. The daemon marks the Slack text as data from a user, not as instructions from the owner.
- A reply in the thread goes to the same session.
- The daemon posts progress and the result back to the thread. It changes one status message for progress, and it posts a new message for the result.
- A mate session is a normal session. It shows in the session list, and the owner can take it over.

### Dangerous actions

The rule: only the owner can order a dangerous action.

| Class | Examples |
|---|---|
| Safe | Read files, answer, edit and commit in the worktree, run tests, use the browser to read. |
| Dangerous | Merge, create a PR, push, delete branches, delete or write files out of the worktree, deploy, external writes (an API call that changes data, a message out of the thread), read or change secrets. |

- A pure function in `domain` classifies each tool call (the tool name and its input, for example a shell command) as safe or dangerous. It has table tests. When the function cannot decide, the result is dangerous.
- crew enforces the rule, not the prompt. The gate is in the Claude `PreToolUse` hook and in the Codex hook and approval policy. The model cannot change the gate.
- Each mate session has an **authority** that the daemon sets from Slack facts, not from text: the Slack user ID of the event that started the current turn. A mention of the owner's name in text gives no authority.
- If the authority is the owner, the gate lets the dangerous action run.
- If the authority is not the owner, the hook denies the action at once with a reason ("waiting for owner approval"). The daemon records a pending approval. The hook does not wait, so the hook budget stays.
- The owner gets the approval request in three places: a Slack message in the thread with Approve and Deny buttons, a push notification and an approval card in the PWA and the Mac app.
- Only a click from the owner's Slack user ID counts. The daemon ignores other clicks and posts nothing for them.
- An approval allows only the one action that was asked for (the same classified action and the same input), one time. Then the daemon tells the session to try it again. A deny tells the session that the owner said no.
- A pending approval expires after 24 hours.

### Mate to mate

- A mate can mention a different mate in a thread. The second mate gets a turn as if a person asked it, with the first mate as the author.
- Owner authority goes through a chain of mates only when the chain starts from a direct request of the owner in that thread, and only for the action that the owner asked for. For other actions the authority is not the owner. Thus a mate cannot get owner authority from a different mate's text.
- Loop guards: a hop limit for each thread (default 5), a rate limit for each mate (default 20 turns each hour), a daily usage budget for each mate (turns and tokens) and a global stop. When a guard stops a turn, the mate posts one message that tells why.
- The daemon knows the action that the owner asked for from the words of the owner's message (merge, PR, push, delete a branch, deploy). The text of the owner is a Slack fact of the owner, so this is not text from a different person. A mate's text never adds an action. The authority is for the kind of action (for example a merge), not for one target (for example one PR).
- The limits are in `[mates]` of `config.toml`: `hop_limit`, `turns_per_hour`, `daily_turns` (default 100), `daily_tokens` (default 0, no limit) and `stop`. The token count comes from the status line of Claude and the rollout of Codex. The counts are in memory, so a restart sets them to zero.

### Control

- A Mates page in the PWA, the Mac app and the TUI shows each mate: its state (connected, paused, stopped by a guard), its current work and its threads.
- The owner can pause or resume each mate, stop all mates at one time (the kill switch), edit the settings and act on pending approvals.
- RPC methods: `mate.list`, `mate.add`, `mate.set`, `mate.remove`, `mate.pause`, `mate.resume`, `mate.stop_all`, `approval.list`, `approval.decide`. The CLI is `crew mate ...`.
- The kill switch is `stop` in `[mates]` of `config.toml`. `mate.stop_all` writes or removes this key. The daemon keeps no other record of the stop. Thus a restart keeps the stop, and a hand edit of the file and the kill switch always agree.
- `mate.pause` and `mate.resume` write `enabled` of the mate. A paused mate has no Slack connection.

## Consequences

- People who are not the owner can start sessions on the owner's server and use the owner's subscription. The rate limit and the daily budget control the cost. The owner sees all of this on the Mates page.
- Safe work runs with no prompt. Thus a person who is not the owner can make a mate read the code in its repos. The owner selects `repos` with this in mind.
- The classifier is a deny list of known dangerous actions plus "dangerous when not sure". A new tool can need a new row. The tests show each row.
- Slack is a new network dependency. The adapter stays out of the hook and the event loop. It runs in its own worker.

## Alternatives

- **One Slack app for all mates.** Rejected. All mates then have one handle and one avatar.
- **Events API over HTTP.** Rejected. It needs a public URL, and the server is only on the tailnet.
- **Safety in the prompt.** Rejected. Text in the thread can change the prompt.
- **A hook that waits for the approval.** Rejected. It breaks the hook budget and keeps a harness process blocked for hours.
- **Owner authority through every mate chain.** Rejected. A person who is not the owner could then ask mate A to ask mate B to merge.
