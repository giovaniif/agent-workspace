package claude_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "statusline", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseStatusReadsModelEffortContextAndEveryLimit(t *testing.T) {
	got, err := claude.ParseStatus(fixture(t, "turn.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.StatusReport{
		Model:       "opus-5.5",
		Effort:      "high",
		ContextLeft: 94,
		HasContext:  true,
		Limits: []domain.RateLimit{
			{Window: "five_hour", UsedPercent: 42, ResetsAt: 1790736600},
			{Window: "seven_day", UsedPercent: 18, ResetsAt: 1791082800},
			{Window: "seven_day_opus", UsedPercent: 62, ResetsAt: 1791082800},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseStatusBeforeTheFirstTurnKnowsNoContextOrLimits(t *testing.T) {
	got, err := claude.ParseStatus(fixture(t, "startup.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.StatusReport{Model: "opus-5.5", Effort: "low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseStatusRejectsNonJSON(t *testing.T) {
	if _, err := claude.ParseStatus([]byte("nope")); err == nil {
		t.Fatal("no error")
	}
}

func TestChainPassesTheInputAndKeepsTheOutputUnchanged(t *testing.T) {
	input := fixture(t, "turn.json")
	var out bytes.Buffer
	err := claude.Chain(context.Background(), `printf '[%s] ' "$(wc -c | tr -d ' ')"; printf 'line two\n'`, input, &out)
	if err != nil {
		t.Fatal(err)
	}
	want := "[" + strconv.Itoa(len(input)) + "] line two\n"
	if out.String() != want {
		t.Fatalf("output %q, want %q", out.String(), want)
	}
}

func TestChainWithoutACommandPrintsNothing(t *testing.T) {
	var out bytes.Buffer
	if err := claude.Chain(context.Background(), "", []byte("{}"), &out); err != nil || out.Len() != 0 {
		t.Fatalf("out %q, err %v", out.String(), err)
	}
}

func TestChainedStatusLineRoundTripsQuotes(t *testing.T) {
	orig := `bash -c 'echo "it'\''s $HOME"'`
	cmd := claude.StatusLineCommand("/a b/crew", orig)
	got, ok := claude.ChainedStatusLine(cmd)
	if !ok || got != orig {
		t.Fatalf("ChainedStatusLine(%q) = %q, %v", cmd, got, ok)
	}
	if _, ok := claude.ChainedStatusLine("~/.claude/statusline.sh"); ok {
		t.Fatal("user command taken as ours")
	}
	if _, ok := claude.ChainedStatusLine("~/bin/my statusline"); ok {
		t.Fatal("user command named statusline taken as ours")
	}
	if got, ok := claude.ChainedStatusLine(claude.StatusLineCommand("/tmp/b001/crew.test", "")); !ok || got != "" {
		t.Fatalf("no chain: %q, %v", got, ok)
	}
}

func TestParseStatusShortensTheModelIDAndFallsBackToTheDisplayName(t *testing.T) {
	cases := []struct{ model, want string }{
		{`{"id":"claude-opus-5-5","display_name":"Opus 5.5"}`, "opus-5.5"},
		{`{"id":"claude-haiku-4-5-20251001","display_name":"Haiku 4.5"}`, "haiku-4.5"},
		{`{"id":"claude-sonnet-5-5[1m]","display_name":"Sonnet 5.5"}`, "sonnet-5.5"},
		{`{"id":"claude-fable-5-1","display_name":"Fable 5.1"}`, "fable-5.1"},
		{`{"display_name":"Opus"}`, "Opus"},
	}
	for _, c := range cases {
		got, err := claude.ParseStatus([]byte(`{"model":` + c.model + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if got.Model != c.want {
			t.Errorf("%s: model %q, want %q", c.model, got.Model, c.want)
		}
	}
}
