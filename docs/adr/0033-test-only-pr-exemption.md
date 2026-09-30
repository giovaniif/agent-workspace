# ADR 0033: tdd job exempts test-only PRs titled test:

Status: accepted, 2026-09-30.

## Decision

- A PR whose title starts with `test:` skips the "tests must fail on base" rule when every changed file is a `*_test.go` file, under a `testdata/` dir, or under `test/`.
- If a `test:` PR changes any other file, `scripts/tdd-check` lists those files and then applies the normal rule, so the job fails when the tests pass on base.
- CI passes the title to the script in the `TDD_PR_TITLE` env var, never interpolated into the shell command.

## Why

- Deflakes and test refactors have no behavior to fail on base (ADR 0002, ADR 0009), so the job blocked them.
- Requiring every file to be a test file keeps the exemption from covering production changes.

## Rejected

- **A label or commit trailer:** the title already carries a conventional prefix.
- **Exempting all test-only diffs regardless of title:** ADR 0009 rejected it; behavior tests that already pass would slip through. The `test:` title is an explicit claim that nothing new is being tested.
