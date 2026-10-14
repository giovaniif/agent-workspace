package notify_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/notify"
	"github.com/giovaniif/crew/internal/domain"
)

type recorder struct {
	calls [][]string
	out   string
	err   error
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return []byte(r.out), r.err
}

func TestNotifyPassesTextAsArgumentsNotScript(t *testing.T) {
	r := &recorder{}
	o := notify.Osascript{Run: r.run}
	b := domain.Banner{Title: `fix "login"`, Body: `needs permission`, Sound: "Glass"}
	if err := o.Notify(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || r.calls[0][0] != "osascript" {
		t.Fatalf("calls %v", r.calls)
	}
	args := r.calls[0]
	tail := args[len(args)-3:]
	if !reflect.DeepEqual(tail, []string{`fix "login"`, "needs permission", "Glass"}) {
		t.Fatalf("argv tail %v", tail)
	}
	for _, a := range args[1 : len(args)-3] {
		if strings.Contains(a, "login") || strings.Contains(a, "permission") {
			t.Fatalf("banner text leaked into script line %q", a)
		}
	}
}

func TestNotifyReturnsRunnerError(t *testing.T) {
	want := errors.New("boom")
	o := notify.Osascript{Run: (&recorder{err: want}).run}
	if err := o.Notify(context.Background(), domain.Banner{Title: "t", Body: "b"}); !errors.Is(err, want) {
		t.Fatalf("err %v", err)
	}
}

func TestNotifyTerminalFrontmostRecognizesTerminalApps(t *testing.T) {
	cases := map[string]bool{
		"Terminal\n":    true,
		"iTerm2\n":      true,
		"Ghostty\n":     true,
		"WezTerm\n":     true,
		"ghostty\n":     true,
		"wezterm-gui\n": true,
		"Safari\n":      false,
		"Slack\n":       false,
		"":              false,
		"Terminal2\n":   false,
	}
	for out, want := range cases {
		o := notify.Osascript{Run: (&recorder{out: out}).run}
		if got := o.TerminalFrontmost(context.Background()); got != want {
			t.Errorf("frontmost %q: got %v", out, got)
		}
	}
}

func TestNotifyTerminalFrontmostIsFalseWhenTheQueryFails(t *testing.T) {
	o := notify.Osascript{Run: (&recorder{out: "Terminal\n", err: errors.New("no access")}).run}
	if o.TerminalFrontmost(context.Background()) {
		t.Fatal("a failed query counted as frontmost")
	}
}

func TestNotifyLoadSounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")
	if err := os.WriteFile(path, []byte(`{"sounds":{"permission":"Glass","done":"Hero","bogus":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := notify.LoadSounds(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.AgentState]string{domain.StatePermission: "Glass", domain.StateDone: "Hero"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestNotifyLoadSoundsMissingFileMeansSilence(t *testing.T) {
	got, err := notify.LoadSounds(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestNotifyLoadSoundsRejectsBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.json")
	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := notify.LoadSounds(path); err == nil {
		t.Fatal("bad json accepted")
	}
}
