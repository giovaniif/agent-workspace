# ADR 0037: The new-session dialog as a centred popup

Status: accepted, 2026-09-30. Changes where [ADR 0015](0015-session-lifecycle.md)'s dialog is drawn and how it looks; `session.new` and the dialog's rules are unchanged.

## Decision

- `n` in the sidebar calls `client.popup` `{"command","env"}`. The daemon runs `display-popup -E` on the attached client with that command, at most 100×32 cells and never larger than the client (tmux refuses a popup that does not fit). The popup closes when the command exits.
- The command is `crew tui --new-session`: the same TUI with `Options.NewSessionOnly`. It subscribes like the sidebar, opens the dialog once the state arrives, and draws only the dialog, centred. `esc` ends it; after `session.new` succeeds it calls `session.focus`, then ends, so the popup never closes before the new session is in view.
- If `client.popup` fails, for example with no terminal attached, `n` opens the same dialog inline in the sidebar, stacked to fit 48 columns.
- The form follows `docs/images/new-session.png`: a title bar, framed Work item and Workspace fields, a preview of what the work item was read as and how the session will be named, the workspace's kind, root and whether it was last used, a `● claude ○ codex` toggle, Model and Effort pickers, an explainer of where the session starts (from `domain.PlanSessionStart`, the plan the daemon makes), the low-quota warning as a box with `ctrl+s start in codex`, and cancel / create.
- Model is a picker over `domain.SwitchChoices` for the harness, plus "default", plus the current model when a default or a fallback brought one the list lacks. It is no longer free text.

## Why

- A tmux pane cannot overlap another, and the sidebar is 48 columns, so the mockup's centred window needs a popup. The shell's `T` already uses `display-popup` (ADR 0029), and a second TUI process keeps the dialog's code and tests in one place.
- Everything the form shows comes from state the TUI already holds or from pure domain functions, so drawing it does no IO.

## Limits

- The popup is a separate process, so the sidebar's selection does not move to the new session; the new session is in view in the main slot.
- The mockup's "AGENTS.md, skills and memory apply" line is left out: it would need reading the workspace from disk.
