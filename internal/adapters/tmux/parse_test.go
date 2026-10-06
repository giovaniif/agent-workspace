package tmux

import (
	"slices"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/app"
)

func TestParsePanesKeepsOnlyManagedPanes(t *testing.T) {
	out := "%0 0 \n" +
		"%1 0 1\n" +
		"%2 1 1\n" +
		"\n" +
		"%3 0 \n"
	want := []app.PaneInfo{
		{ID: "%1", Alive: true},
		{ID: "%2", Alive: false},
	}
	if got := parsePanes(out); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNewestActivityIsTheLatestClientsInput(t *testing.T) {
	tests := map[string]int64{
		"1791301222\n1791301300\n1791301250\n": 1791301300,
		"1791301222\n":                         1791301222,
		"":                                     0,
		"\n\nnot-a-time\n1791301222\n":         1791301222,
		"0\n":                                  0,
	}
	for in, want := range tests {
		got := newestActivity(in)
		if want == 0 {
			if !got.IsZero() {
				t.Errorf("newestActivity(%q) = %v, want zero", in, got)
			}
			continue
		}
		if got.Unix() != want {
			t.Errorf("newestActivity(%q) = %d, want %d", in, got.Unix(), want)
		}
	}
}

func TestTrimCaptureDropsTrailingBlankLines(t *testing.T) {
	tests := map[string]string{
		"a\nb\n\n\n":   "a\nb",
		"a\n\nb\n":     "a\n\nb",
		"\n\n":         "",
		"  a  \n   \n": "  a  ",
	}
	for in, want := range tests {
		if got := trimCapture(in); got != want {
			t.Errorf("trimCapture(%q) = %q, want %q", in, got, want)
		}
	}
}
