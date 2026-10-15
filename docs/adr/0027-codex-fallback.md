# ADR 0027: Codex fallback when Claude quota is low

Status: accepted, 2026-09-30.

## Decision

- `domain.OfferFallback(quotas, cfg, req)` proposes a Codex start for a Claude `StartRequest` (harness, model, effort) when Claude's shortest window has less than the threshold left. It returns the `SwitchAdvice` (the figures) and the Codex `StartRequest`. `domain.OfferFallbacks` does the same for a queue, keyed by index, so the launcher (#27) asks once per queued item.
- It never touches a running session. Codex model and effort switching is refused (ADR 0018), so the fallback is always a new Codex session, never a switch of an existing one.
- No offer when the request is not Claude, when Codex has no figure, or when Codex's own shortest window is also under the threshold. Moving between two exhausted accounts helps nobody. This is stricter than `Advise`, which still warns without an offer.
- Config lives in `$CREW_HOME/config.toml`, read by `tui.LoadFallback`:

  ```toml
  [fallback]
  threshold = 25            # percent left; default 25 (domain.WarnQuotaLeft)
  [fallback.models]
  opus = "gpt-5"            # matched as a lowercase substring of the Claude model, longest key first
  default = "gpt-5"         # used when the request names no model
  [fallback.efforts]
  max = "high"
  ```

- An unmapped model is left empty, so Codex uses its own default (or the `[defaults.codex]` model in the dialog). Efforts map as `low`, `medium` and `high` to themselves, `xhigh` and `max` to `high`, anything else to empty; `[fallback.efforts]` overrides any of these.
- The new-session dialog uses `domain.AdviseAt` with the configured threshold for its warning. When an offer exists, the warning reads `ctrl+s codex 64% <model>`, and `ctrl+s` moves the dialog to Codex with the mapped model and effort (empty ones take `[defaults.codex]`). The user still confirms with enter.

## Why

- One pure rule serves the dialog and the queue, so their thresholds and mappings cannot drift.
- Substring matching copes with the many names Claude reports for one family (`opus`, `Opus 4.7`, `claude-opus-4-7`).

## Limits

- The launcher (#27, ADR 0031) calls `OfferFallbacks` and shows the offer under each queued issue; `c` takes it.
- Quotas come from running sessions' reports. With no Codex session reporting, there is no Codex figure and no offer.
- Staleness of a figure is not considered; the dialog offers on the last report.
