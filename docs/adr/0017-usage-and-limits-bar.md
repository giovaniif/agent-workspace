# ADR 0017: Usage and limits bar

Status: accepted, 2026-09-30.

## Decision

- Quotas are derived, not stored. `domain.Quotas(sessions)` folds every session's `Limits` into one `Quota` per harness and window: percent left (100 minus used, clamped), reset time, and when it was reported. Each window takes the newest report across sessions, since the account is shared by all sessions of a harness. The TUI computes this from the state it already has, so no new RPC or persisted field is needed.
- `Session.LimitsAt` is when `Limits` were reported. The daemon stamps a Claude status-line report when it receives it (`StatusReport.At`). For Codex it is the rollout's timestamp on the `token_count` event that carried the limits, so a days-old session's figures read as old rather than fresh. A rollout without timestamps falls back to the read time, kept stable while the limits do not change.
- Codex windows are renamed by length so they line up with Claude's: 300 minutes is `five_hour`, 10080 is `seven_day`, anything else is `<n>m`. Claude's per-model windows (`seven_day_opus`) sort with the seven-day ones and are labelled `7d opus`.
- The top bar shows one row per harness that has data, under the `crew` row: `CC 5h 43% 2h9m  7d 29% 2d`. Percent left is red below 20. A window reported more than 15 minutes ago is dimmed, and the row's oldest age shows on the right (`20m ago`). With no data the rows are absent, never zeros. (Display changed by ADR 0036: percent used and a reset clock.)
- The low-quota warning is `domain.Advise(quotas, chosen)`: it fires when the chosen harness's shortest window has under 25% left, and carries the other harness's shortest window so the dialog can show its figure and offer "start in <other>". With no figure for the other harness it carries none, and the dialog has nothing to switch on. The new-session dialog (#11) wires it; this change ships the rule and the top bar.
- Thresholds (20, 25, 15 min) are constants in `domain`.

## Why

- Quotas belong to the account, not a session, but the daemon only keeps sessions. Deriving from sessions avoids a second store that could disagree with them, and a quota disappears from the bar when the last session of its harness is gone, which matches "no data hides the slot".
- The staleness rule needs the report time, and only the adapters know it. Using the rollout's own timestamp for Codex is the only way to tell a live figure from one left in an old file.
- Rules in `domain` keep the thresholds table-tested and shared by the top bar and the dialog.

## Limits

- The top bar has 48 columns. Three Claude windows plus a stale age still fit; a fourth window would be truncated on the right.
- (Superseded by ADR 0036: a window whose reset time has passed is hidden.)
- Values are checked against the adapters' reports by unit tests. The comparison with `/usage` and `/status` is manual.
