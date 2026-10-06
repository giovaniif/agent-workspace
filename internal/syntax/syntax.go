package syntax

import (
	"embed"
	"io/fs"
	"sync"

	. "github.com/alecthomas/chroma/v2" //nolint:staticcheck
)

//go:embed lexers/*.xml
var lexerFiles embed.FS

var (
	lexerOnce     sync.Once
	lexerRegistry *LexerRegistry
)

func Match(filename string) Lexer {
	lexerOnce.Do(func() {
		reg := NewLexerRegistry()
		paths, _ := fs.Glob(lexerFiles, "lexers/*.xml")
		for _, p := range paths {
			if l, err := NewXMLLexer(lexerFiles, p); err == nil {
				reg.Register(l)
			}
		}
		reg.Register(MustNewLexer(&Config{Name: "Go", Aliases: []string{"go"}, Filenames: []string{"*.go"}}, goRules))
		reg.Register(MustNewLexer(&Config{Name: "markdown", Aliases: []string{"md", "mkd"}, Filenames: []string{"*.md", "*.mkd", "*.markdown"}, EnsureNL: true}, markdownRules))
		lexerRegistry = reg
	})
	return lexerRegistry.Match(filename)
}

//nolint:govet
func goRules() Rules {
	return Rules{
		"root": {
			{`\n`, TextWhitespace, nil},
			{`\s+`, TextWhitespace, nil},
			{`//[^\s\n\r][^\n\r]*`, CommentPreproc, nil},
			{`//[^\n\r]*`, CommentSingle, nil},
			{`/(\\\n)?[*](.|\n)*?[*](\\\n)?/`, CommentMultiline, nil},
			{`(import|package)\b`, KeywordNamespace, nil},
			{`(var|func|struct|map|chan|type|interface|const)\b`, KeywordDeclaration, nil},
			{Words(``, `\b`, `break`, `default`, `select`, `case`, `defer`, `go`, `else`, `goto`, `switch`, `fallthrough`, `if`, `range`, `continue`, `for`, `return`), Keyword, nil},
			{`(true|false|iota|nil)\b`, KeywordConstant, nil},
			{Words(``, `\b(\()`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `int`, `int8`, `int16`, `int32`, `int64`, `float`, `float32`, `float64`, `complex64`, `complex128`, `byte`, `rune`, `string`, `bool`, `error`, `uintptr`, `print`, `println`, `panic`, `recover`, `close`, `complex`, `real`, `imag`, `len`, `cap`, `append`, `copy`, `delete`, `new`, `make`, `clear`, `min`, `max`), ByGroups(NameBuiltin, Punctuation), nil},
			{Words(``, `\b`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `int`, `int8`, `int16`, `int32`, `int64`, `float`, `float32`, `float64`, `complex64`, `complex128`, `byte`, `rune`, `string`, `bool`, `error`, `uintptr`, `any`), KeywordType, nil},
			{`\d+(\.\d+[eE][+\-]?\d+|\.\d*|[eE][+\-]?\d+)`, LiteralNumberFloat, nil},
			{`0[xX][0-9a-fA-F_]+`, LiteralNumberHex, nil},
			{`0b[01_]+`, LiteralNumberBin, nil},
			{`(0|[1-9][0-9_]*)`, LiteralNumberInteger, nil},
			{`'(\\['"\\abfnrtv]|\\x[0-9a-fA-F]{2}|\\[0-7]{1,3}|\\u[0-9a-fA-F]{4}|\\U[0-9a-fA-F]{8}|[^\\])'`, LiteralStringChar, nil},
			{"`[^`]*`", LiteralString, nil},
			{`"(\\\\|\\"|[^"])*"`, LiteralString, nil},
			{`(<<=|>>=|<<|>>|<=|>=|&\^=|&\^|\+=|-=|\*=|/=|%=|&=|\|=|&&|\|\||<-|\+\+|--|==|!=|:=|\.\.\.|[+\-*/%&])`, Operator, nil},
			{`([a-zA-Z_]\w*)(\s*)(\()`, ByGroups(NameFunction, UsingSelf("root"), Punctuation), nil},
			{`[|^<>=!()\[\]{}.,;:~]`, Punctuation, nil},
			{`[^\W\d]\w*`, NameOther, nil},
		},
	}
}
