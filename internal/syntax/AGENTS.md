# internal/syntax

The curated chroma lexers that the TUI review pane and `agentws rpc` share, so both clients highlight a diff the same way (see [ADR 0049](../../docs/adr/0049-macos-app.md), "Review highlighting in Go").

- Lexers: chroma XML files in `lexers/` (MIT, `COPYING` kept), plus Go and Markdown rules in Go. They are built on first use. A new language goes in `lexers/` as a chroma XML file. depguard bans chroma's `lexers` and `styles` packages: their init would cost every `agentws hook`.
- `Match(filename)` returns the lexer for a file name, or nil.
- `Spans(filename, lines)` tokenises the lines as one text and returns `[start, end, class]` spans per line: byte offsets in the line, adjacent same-class tokens merged. Classes are a fixed set: `keyword`, `string`, `comment`, `number`, `function`, `type`, `operator`, `punctuation`. A file with no lexer, or a lexer that fails, gives nil. The TUI colours chroma token types itself (`internal/tui/highlight.go`); the class mapping is only for native clients.
- Tests: `go test ./cmd/agentws/ -run Tokens` pins the spans for Go, TypeScript and Markdown in `cmd/agentws/testdata/review_tokens.json.golden` (regenerate with `-update` and review the diff); the TUI review goldens pin the TUI side.
