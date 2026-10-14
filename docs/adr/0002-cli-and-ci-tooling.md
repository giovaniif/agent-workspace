# ADR 0002: CLI dispatch and CI guardrails

Status: accepted, 2026-09-29.

## Decision

- `cmd/crew` dispatches subcommands with a plain `switch` on `os.Args`, no CLI framework, until flags get complex enough to need one.
- Version and commit are injected with `-ldflags -X`. Without them, the commit falls back to the build info's `vcs.revision`.
- Layer rules are `depguard` deny lists in `.golangci.yml`. `app` and `tui` deny the specific internal packages they must not use, rather than allow-listing, so the standard library and third-party packages stay usable.
- The `tdd` CI job is `scripts/tdd-check`. It checks out the base branch, overlays the PR's added and modified `*_test.go` files, and runs `go test` per affected package. Any package that passes fails the job. Deleted test files are ignored.
- `mutate` uses `gremlins` efficacy (killed / (killed + lived)) as the mutation score, with an 80% threshold. It skips a package with no `*_test.go` files.
- Comment rules are checked by `scripts/lint-comments`, a small `go/ast` program, because no golangci-lint linter expresses "only `// why:` inside bodies".

## Why

- Hook startup must stay near 5 ms; a stdlib dispatch adds nothing to it.
- Per-package granularity in `tdd-check` avoids parsing test names out of diffs. The cost is that a test-only refactor mixed into a feature PR fails the job, so those go in their own PR.

## Rejected

- **cobra:** more than we need for six subcommands with no flags yet.
- **Running only the changed test functions in `tdd`:** needs diff parsing for little gain now.
