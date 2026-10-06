package syntax

import (
	"path"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

type Span struct {
	Start, End int
	Class      string
}

func (s Span) MarshalJSON() ([]byte, error) {
	return []byte("[" + strconv.Itoa(s.Start) + "," + strconv.Itoa(s.End) + "," + strconv.Quote(s.Class) + "]"), nil
}

func Class(tt chroma.TokenType) string {
	switch {
	case tt.InCategory(chroma.Comment):
		return "comment"
	case tt == chroma.KeywordType || tt == chroma.NameClass:
		return "type"
	case tt.InCategory(chroma.Keyword), tt == chroma.GenericHeading, tt == chroma.GenericSubheading:
		return "keyword"
	case tt == chroma.NameFunction || tt == chroma.NameBuiltin:
		return "function"
	case tt.InSubCategory(chroma.LiteralString):
		return "string"
	case tt.InSubCategory(chroma.LiteralNumber):
		return "number"
	case tt.InCategory(chroma.Operator):
		return "operator"
	case tt == chroma.Punctuation:
		return "punctuation"
	}
	return ""
}

func Spans(filename string, lines []string) (out [][]Span) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	lexer := Match(path.Base(filename))
	if lexer == nil {
		return nil
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		return nil
	}
	out = make([][]Span, len(lines))
	row, col := 0, 0
	for t := it(); t != chroma.EOF && row < len(out); t = it() {
		class := Class(t.Type)
		for j, p := range strings.Split(t.Value, "\n") {
			if j > 0 {
				row, col = row+1, 0
				if row >= len(out) {
					break
				}
			}
			if p != "" && class != "" {
				out[row] = appendSpan(out[row], Span{Start: col, End: col + len(p), Class: class})
			}
			col += len(p)
		}
	}
	return out
}

func appendSpan(spans []Span, s Span) []Span {
	if n := len(spans); n > 0 && spans[n-1].End == s.Start && spans[n-1].Class == s.Class {
		spans[n-1].End = s.End
		return spans
	}
	return append(spans, s)
}
