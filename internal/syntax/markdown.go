package syntax

import (
	. "github.com/alecthomas/chroma/v2" //nolint:staticcheck
)

//nolint:govet
func markdownRules() Rules {
	return Rules{
		"root": {
			{`<!--[\w\W]*?-->`, CommentMultiline, nil},
			{`^(#[^#].+\n)`, ByGroups(GenericHeading), nil},
			{`^(#{2,6}.+\n)`, ByGroups(GenericSubheading), nil},
			{`^(\s*)([*-] )(\[[ xX]\])( .+\n)`, ByGroups(Text, Keyword, Keyword, UsingSelf("inline")), nil},
			{`^(\s*)([*-])(\s)(.+\n)`, ByGroups(Text, Keyword, Text, UsingSelf("inline")), nil},
			{`^(\s*)([0-9]+\.)( .+\n)`, ByGroups(Text, Keyword, UsingSelf("inline")), nil},
			{`^(\s*>\s)(.+\n)`, ByGroups(Keyword, GenericEmph), nil},
			{"^(```\\w*\\n)([\\w\\W]*?)(^```$)", ByGroups(String, Text, String), nil},
			Include("inline"),
		},
		"inline": {
			{`<!--[\w\W]*?-->`, CommentMultiline, nil},
			{`\\.`, Text, nil},
			{`(\s)((\*\*|__).*?)\3((?=\W|\n))`, ByGroups(Text, GenericStrong, GenericStrong, Text), nil},
			{"`[^`]+`", LiteralStringBacktick, nil},
			{`(!?\[)([^]]+)(\])(\()([^)]+)(\))`, ByGroups(Text, NameTag, Text, Text, NameAttribute, Text), nil},
			{`.|\n`, Text, nil},
		},
	}
}
