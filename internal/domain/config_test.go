package domain

import "testing"

func TestConfigLiteralValidatesEachKey(t *testing.T) {
	cases := []struct {
		key, value, want string
		ok               bool
	}{
		{"launcher.max_parallel", "4", "4", true},
		{"launcher.max_parallel", "0", "", false},
		{"launcher.max_parallel", "four", "", false},
		{"fallback.threshold", "20", "20", true},
		{"fallback.threshold", "0", "0", true},
		{"fallback.threshold", "100", "100", true},
		{"fallback.threshold", "101", "", false},
		{"fallback.threshold", "-1", "", false},
		{"push.away_after", "2m", `"2m"`, true},
		{"push.away_after", "0", `"0"`, true},
		{"push.away_after", "-1m", "", false},
		{"push.away_after", "soon", "", false},
		{"defaults.claude.model", "opus", `"opus"`, true},
		{"defaults.claude.model", "opus[1m]", `"opus[1m]"`, true},
		{"defaults.claude.model", `op"us`, "", false},
		{"defaults.claude.model", "op us", "", false},
		{"defaults.claude.effort", "xhigh", `"xhigh"`, true},
		{"defaults.claude.effort", "turbo", "", false},
		{"defaults.codex.model", "gpt-6-sol", `"gpt-6-sol"`, true},
		{"defaults.nope.model", "x", "", false},
		{"theme.blue", "#1E66F5", `"#1E66F5"`, true},
		{"theme.added_bg", "#a6e3a1", `"#a6e3a1"`, true},
		{"theme.blue", "blue", "", false},
		{"theme.blue", "#12345", "", false},
		{"theme.purple", "#123456", "", false},
		{"linear.token", "secret", "", false},
		{"launcher", "4", "", false},
		{"", "4", "", false},
	}
	for _, c := range cases {
		got, err := ConfigLiteral(c.key, c.value)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ConfigLiteral(%q, %q) = %q, %v; want %q ok=%v", c.key, c.value, got, err, c.want, c.ok)
		}
	}
}

func TestConfigLiteralOfAnEmptyValueUnsetsAKnownKey(t *testing.T) {
	got, err := ConfigLiteral("defaults.codex.effort", "")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ConfigLiteral("linear.token", ""); err == nil {
		t.Fatal("an unknown key was accepted for unsetting")
	}
}

func TestConfigKeysListEverySettableKey(t *testing.T) {
	keys := ConfigKeys()
	for _, want := range []string{"launcher.max_parallel", "fallback.threshold", "push.away_after", "defaults.claude.model", "defaults.codex.effort", "theme.deleted_bg"} {
		found := false
		for _, k := range keys {
			found = found || k == want
		}
		if !found {
			t.Errorf("ConfigKeys lacks %q", want)
		}
	}
	for _, k := range keys {
		if _, err := ConfigLiteral(k, ""); err != nil {
			t.Errorf("listed key %q cannot be unset: %v", k, err)
		}
	}
}

func TestSetTOMLValue(t *testing.T) {
	cases := []struct {
		name, src, key, literal, want string
	}{
		{
			name: "empty file gets the table",
			src:  "", key: "launcher.max_parallel", literal: "4",
			want: "[launcher]\nmax_parallel = 4\n",
		},
		{
			name: "replaces the key in its table and keeps comments and other keys",
			src:  "# mine\n[launcher]\n# how many\nmax_parallel = 2 # was two\nother = true\n\n[ui]\nmouse = false\n",
			key:  "launcher.max_parallel", literal: "4",
			want: "# mine\n[launcher]\n# how many\nmax_parallel = 4\nother = true\n\n[ui]\nmouse = false\n",
		},
		{
			name: "adds the key at the end of an existing table",
			src:  "[defaults.claude]\nmodel = \"opus\"\n\n[ui]\nmouse = false\n",
			key:  "defaults.claude.effort", literal: `"high"`,
			want: "[defaults.claude]\nmodel = \"opus\"\neffort = \"high\"\n\n[ui]\nmouse = false\n",
		},
		{
			name: "same key name in another table is left alone",
			src:  "[defaults.codex]\nmodel = \"gpt-6-sol\"\n",
			key:  "defaults.claude.model", literal: `"opus"`,
			want: "[defaults.codex]\nmodel = \"gpt-6-sol\"\n\n[defaults.claude]\nmodel = \"opus\"\n",
		},
		{
			name: "file without a trailing newline",
			src:  "[ui]\nmouse = false",
			key:  "theme.blue", literal: `"#000000"`,
			want: "[ui]\nmouse = false\n\n[theme]\nblue = \"#000000\"\n",
		},
		{
			name: "header with spaces and a comment",
			src:  "[ theme ] # colours\nbase = \"#111111\"\n",
			key:  "theme.base", literal: `"#222222"`,
			want: "[ theme ] # colours\nbase = \"#222222\"\n",
		},
		{
			name: "empty literal removes the key",
			src:  "[push]\naway_after = \"2m\"\nkeep = 1\n",
			key:  "push.away_after", literal: "",
			want: "[push]\nkeep = 1\n",
		},
		{
			name: "removing a missing key changes nothing",
			src:  "# only a comment\n",
			key:  "push.away_after", literal: "",
			want: "# only a comment\n",
		},
		{
			name: "array tables end the section",
			src:  "[launcher]\n[[things]]\nmax_parallel = 9\n",
			key:  "launcher.max_parallel", literal: "3",
			want: "[launcher]\nmax_parallel = 3\n[[things]]\nmax_parallel = 9\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SetTOMLValue(c.src, c.key, c.literal)
			if got != c.want {
				t.Fatalf("got\n%q\nwant\n%q", got, c.want)
			}
			if again := SetTOMLValue(got, c.key, c.literal); again != got {
				t.Fatalf("not idempotent:\n%q\n%q", got, again)
			}
		})
	}
}
