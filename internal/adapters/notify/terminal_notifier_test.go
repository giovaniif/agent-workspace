package notify_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/notify"
	"github.com/giovaniif/crew/internal/domain"
)

func TestNotifyTerminalNotifierGroupsAndFocusesOnClick(t *testing.T) {
	r := &recorder{}
	n := notify.TerminalNotifier{Run: r.run, Bin: "/opt/bin/terminal-notifier", Self: "/usr/local/bin/agentws", Home: "/tmp/it's home"}
	b := domain.Banner{Title: "fix login", Body: "needs permission: Bash", Sound: "Glass", Group: "s1", Terminal: "com.mitchellh.ghostty"}
	if err := n.Notify(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt/bin/terminal-notifier",
		"-title", "fix login", "-message", "needs permission: Bash", "-group", "s1",
		"-sound", "Glass", "-activate", "com.mitchellh.ghostty",
		"-execute", `CREW_HOME='/tmp/it'\''s home' '/usr/local/bin/agentws' focus 's1'`,
	}
	if len(r.calls) != 1 || !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("argv\n got %q\nwant %q", r.calls, want)
	}
}

func TestNotifyTerminalNotifierLeavesOutWhatIsUnknown(t *testing.T) {
	r := &recorder{}
	n := notify.TerminalNotifier{Run: r.run, Bin: "terminal-notifier", Self: "/bin/agentws", Home: "/h"}
	if err := n.Notify(context.Background(), domain.Banner{Title: "claude", Body: "done"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"terminal-notifier", "-title", "claude", "-message", "done"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("argv %q", r.calls[0])
	}
}

func TestNotifyTerminalNotifierEscapesAMessageItWouldParse(t *testing.T) {
	r := &recorder{}
	n := notify.TerminalNotifier{Run: r.run, Bin: "terminal-notifier"}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t", Body: "[x] -done"}); err != nil {
		t.Fatal(err)
	}
	if got := r.calls[0][4]; got != `\[x] -done` {
		t.Fatalf("message %q", got)
	}
}

func TestNotifyTerminalNotifierRemovesTheGroup(t *testing.T) {
	r := &recorder{}
	n := notify.TerminalNotifier{Run: r.run, Bin: "terminal-notifier"}
	if err := n.Remove(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"terminal-notifier", "-remove", "s1"}; !reflect.DeepEqual(r.calls, [][]string{want}) {
		t.Fatalf("calls %q", r.calls)
	}
}

func TestNotifyTerminalNotifierReturnsRunnerError(t *testing.T) {
	want := errors.New("boom")
	n := notify.TerminalNotifier{Run: (&recorder{err: want}).run, Bin: "terminal-notifier"}
	if err := n.Notify(context.Background(), domain.Banner{Title: "t", Body: "b"}); !errors.Is(err, want) {
		t.Fatalf("notify err %v", err)
	}
	if err := n.Remove(context.Background(), "g"); !errors.Is(err, want) {
		t.Fatalf("remove err %v", err)
	}
}

func TestNotifyOsascriptRemoveRunsNothing(t *testing.T) {
	r := &recorder{}
	if err := (notify.Osascript{Run: r.run}).Remove(context.Background(), "s1"); err != nil || len(r.calls) != 0 {
		t.Fatalf("calls %v err %v", r.calls, err)
	}
}

func TestNotifySelectPrefersTerminalNotifierFallingBackToOsascript(t *testing.T) {
	found := func(name string) (string, error) { return "/opt/bin/" + name, nil }
	got, ok := notify.Select(found, notify.Click{Self: "/bin/agentws", Home: "/h", FocusCmd: "ssh -T 'vps' agentws focus"}).(notify.Fallback)
	if !ok {
		t.Fatalf("got %#v", got)
	}
	tn, ok := got.Primary.(notify.TerminalNotifier)
	if !ok || tn.Bin != "/opt/bin/terminal-notifier" || tn.Self != "/bin/agentws" || tn.Home != "/h" || tn.FocusCmd != "ssh -T 'vps' agentws focus" || tn.Run == nil {
		t.Fatalf("primary %#v", got.Primary)
	}
	if o, ok := got.Secondary.(notify.Osascript); !ok || o.Run == nil {
		t.Fatalf("secondary %#v", got.Secondary)
	}
}

func TestNotifySelectUsesTerminalNotifierAloneWithoutOsascript(t *testing.T) {
	onlyTN := func(name string) (string, error) {
		if name == "terminal-notifier" {
			return "/opt/bin/terminal-notifier", nil
		}
		return "", errors.New("not found")
	}
	if got, ok := notify.Select(onlyTN, notify.Click{}).(notify.TerminalNotifier); !ok || got.Bin != "/opt/bin/terminal-notifier" {
		t.Fatalf("got %#v", got)
	}
}

func TestNotifySelectFallsBackToOsascript(t *testing.T) {
	onlyOsascript := func(name string) (string, error) {
		if name == "osascript" {
			return "/usr/bin/osascript", nil
		}
		return "", errors.New("not found")
	}
	if got, ok := notify.Select(onlyOsascript, notify.Click{}).(notify.Osascript); !ok || got.Run == nil {
		t.Fatalf("got %#v", got)
	}
}
