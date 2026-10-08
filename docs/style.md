# Writing style

Write the prose docs in this repo in ASD-STE100 Simplified Technical English (STE), adapted. This applies to `README.md`, `FEATURES.md`, `ARCHITECTURE.md`, every `AGENTS.md`, and the other docs under `docs/`. Decision records in `docs/adr/` stay as written, but write new ADRs in this style.

## Rules

- Write one topic in each sentence. Write one topic in each paragraph.
- Keep procedural sentences (instructions) to 20 words or fewer. Keep descriptive sentences to 25 words or fewer.
- Keep paragraphs to 6 sentences or fewer.
- Use the active voice. Write instructions in the imperative: "Run `make lint`.", not "You should run `make lint`."
- Use one word for one meaning. Use only the terms in the [glossary](#glossary) for the concepts it names.
- Use a simple verb, not a phrasal verb or an idiom: "remove", not "get rid of".
- For a rule, write "do" or "do not". Do not write "should", "may" or "might" for a rule.
- Use a numbered list for steps. Use a bulleted list for conditions.
- Put a warning or a caution before the step that it applies to.
- Do not change code, commands, paths, method names or identifiers. Write them exactly as they are, in backticks.

## Glossary

Use these terms with these meanings only.

| Term | Meaning |
|---|---|
| **agent** | The AI program that does the work in a session: Claude Code, Codex or Oh My Pi (`omp`). |
| **harness** | The kind of agent program: `claude`, `codex` or `omp`. Each harness has an adapter in `internal/adapters`. |
| **session** | One agent that runs in one pane, with its state, model, effort and worktrees. |
| **task** | A work item: a Linear issue, a PR or free text. Sessions are grouped under it. |
| **workspace** | A folder that `agentws` manages: a single repo, or an orchestration root. |
| **orchestration root** | A workspace folder that holds several repos. |
| **project** | A registered workspace root with an optional machine-local setup script (`domain.Project`). |
| **repo** | A git repository: a folder with its own `.git` directory. |
| **worktree** | One repo at one branch, made with `git worktree add`. It has its own PR, checks, ports and cleanup. |
| **subtask** | One part of a task that has its own worktree in a repo. |
| **daemon** | The long-lived `agentws daemon` process. It owns all state for one `AGENTWS_HOME`. |
| **TUI** | The terminal interface (`agentws tui`): the sidebar and its views. |
| **sidebar** | The left part of the TUI. It lists projects, sessions and worktrees. |
| **pane** | A tmux pane on the `agentws` tmux server. An agent, a shell or nvim runs in it. |
| **slot** | The main area of the client layout. The pane in view is swapped into it. |
| **tab** | One terminal or agent in a project worktree. The tab strip on the pane's top border shows them. |
| **hook** | A command that a harness runs on an event. In `agentws`, it is `agentws hook`. |
| **event** | A harness-neutral message that the daemon applies to a session. |
| **review** | The local, PR-style view of a diff, with its scope and draft comments. |
| **scope** | The part of the changes that a review shows: `last_turn`, `uncommitted` or `branch`. |
| **turn** | One prompt and the agent's work on it, until the agent stops. |
| **banner** | A desktop notification for a session. |
| **setup recipe** | The `[setup]` table in a repo's `.agentws.toml`. It prepares a new worktree. |
| **cleanup** | The removal of a worktree after its PR merges, with a backup of any uncommitted changes. |
| **port** | In `internal/app` and the layer rules: an interface that an adapter implements. In the ports view and `ports.*` methods: a TCP port that a dev server listens on. Use "port interface" or "TCP port" when the context does not make the meaning clear. |
| **adapter** | Code in `internal/adapters` that implements a port with a real tool or library. |
| **device** | A phone or other client that is paired with `agentws serve`. |
