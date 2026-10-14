package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const claudeDialog = `● Bash(touch notes.txt)

────────────────────────────────────────────────────────────────────────────────
 Bash command

   touch notes.txt

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for touch commands in /tmp/project
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel
`

const codexDialog = `  Would you like to run the following command?

  $ touch notes.txt

› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands that start with ` + "`touch`" + ` (p)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`

func promptSetup(t *testing.T, harness domain.Harness, state domain.AgentState, screens ...string) (*rpc.Client, *fakeHost) {
	t.Helper()
	host := &fakeHost{screens: screens}
	d, path := start(t, &memStore{}, daemon.WithHarnesses(host, claude.Adapter{}, codex.Adapter{}))
	c := dial(t, path)
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "a", Harness: harness, Pane: "%3", State: state}})
	next(t, sub.Diffs)
	return c, host
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	var rerr *rpc.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("error %v is not an rpc error", err)
	}
	return rerr.Code
}

func TestSessionPromptReturnsTheClaudeDialogAndItsChoices(t *testing.T) {
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	got, err := c.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.Prompt{
		Text: "Bash command\n\ntouch notes.txt\n\nDo you want to proceed?",
		Choices: []rpc.PromptChoice{
			{ID: "1", Label: "Yes"},
			{ID: "2", Label: "Yes, and don't ask again for touch commands in /tmp/project"},
			{ID: "3", Label: "No, and tell Claude what to do differently"},
		},
	}
	if got.ID == "" {
		t.Fatal("the prompt has no id")
	}
	got.ID = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSessionPromptIDChangesWithTheDialog(t *testing.T) {
	first, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	a, err := first.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, strings.ReplaceAll(claudeDialog, "touch notes.txt", "rm -rf build"))
	b, err := other.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	c, err := again.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.ID != c.ID {
		t.Fatalf("ids %q %q %q", a.ID, b.ID, c.ID)
	}
}

func TestSessionPromptReturnsTheCodexDialogAndItsChoices(t *testing.T) {
	c, _ := promptSetup(t, domain.HarnessCodex, domain.StatePermission, codexDialog)
	got, err := c.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Would you like to run the following command?\n\n$ touch notes.txt" || len(got.Choices) != 3 || got.Choices[2].Label != "No, and tell Codex what to do differently" {
		t.Fatalf("got %+v", got)
	}
}

func TestSessionPromptIsNotFoundWhenNoDialogIsShowingAndTheSessionIsNotAskingPermission(t *testing.T) {
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StateRunning, "● working on it\n")
	if _, err := c.SessionPrompt(context.Background(), "a"); errorCode(t, err) != rpc.CodeNotFound {
		t.Fatalf("error %v", err)
	}
}

func TestSessionPromptReturnsTheRawPaneWhenPermissionIsPendingButTheDialogIsNotRecognized(t *testing.T) {
	screen := "Allow this unusual request?\n  [a] allow  [d] deny\n"
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, screen)
	got, err := c.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	want := rpc.Prompt{Choices: []rpc.PromptChoice{}, Raw: "Allow this unusual request?\n  [a] allow  [d] deny"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSessionPromptRawKeepsOnlyTheLastVisibleLinesWithoutTrailingBlanks(t *testing.T) {
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, strings.Join(lines, "\n")+"\n\n   \n")
	got, err := c.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(got.Raw, "\n")
	if len(raw) != 40 || raw[0] != "line 21" || raw[39] != "line 60" {
		t.Fatalf("raw has %d lines from %q to %q", len(raw), raw[0], raw[len(raw)-1])
	}
}

func TestSessionPromptIsNotFoundForAnUnknownSession(t *testing.T) {
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	if _, err := c.SessionPrompt(context.Background(), "nope"); errorCode(t, err) != rpc.CodeNotFound {
		t.Fatalf("error %v", err)
	}
}

func TestSessionAnswerPressesTheChoicesKeysWhileThePermissionIsPending(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	if err := c.SessionAnswer(context.Background(), "a", "3"); err != nil {
		t.Fatal(err)
	}
	if got, want := host.typedNow(), []string{"%3 keys 3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

func TestSessionAnswerUsesTheCodexShortcut(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessCodex, domain.StatePermission, codexDialog)
	if err := c.SessionAnswer(context.Background(), "a", "3"); err != nil {
		t.Fatal(err)
	}
	if got, want := host.typedNow(), []string{"%3 keys Escape"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

func TestSessionAnswerAfterTheSessionMovedOnIsRefusedAndSendsNothing(t *testing.T) {
	for _, state := range []domain.AgentState{domain.StateRunning, domain.StateIdle, domain.StateWaiting, domain.StateDone} {
		t.Run(string(state), func(t *testing.T) {
			c, host := promptSetup(t, domain.HarnessClaude, state, claudeDialog)
			err := c.SessionAnswer(context.Background(), "a", "1")
			if errorCode(t, err) != rpc.CodeStale {
				t.Fatalf("error %v", err)
			}
			if typed := host.typedNow(); len(typed) != 0 {
				t.Fatalf("typed %q", typed)
			}
		})
	}
}

func TestSessionAnswerIsNotFoundWhenTheDialogIsGone(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessClaude, domain.StatePermission, "● done\n")
	if err := c.SessionAnswer(context.Background(), "a", "1"); errorCode(t, err) != rpc.CodeNotFound {
		t.Fatalf("error %v", err)
	}
	if typed := host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionAnswerWithAnUnknownChoiceIsABadRequestAndSendsNothing(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	if err := c.SessionAnswer(context.Background(), "a", "9"); errorCode(t, err) != rpc.CodeBadRequest {
		t.Fatalf("error %v", err)
	}
	if typed := host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionAnswerIsNotFoundForAnUnknownSession(t *testing.T) {
	c, _ := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	if err := c.SessionAnswer(context.Background(), "nope", "1"); errorCode(t, err) != rpc.CodeNotFound {
		t.Fatalf("error %v", err)
	}
}

func TestSessionAnswerForAPromptThatIsNoLongerTheOneShowingIsStaleAndSendsNothing(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog)
	err := c.Call(context.Background(), rpc.MethodSessionAnswer, rpc.AnswerParams{Session: "a", Choice: "1", Prompt: "not-the-showing-prompt"}, nil)
	if errorCode(t, err) != rpc.CodeStale {
		t.Fatalf("error %v", err)
	}
	if typed := host.typedNow(); len(typed) != 0 {
		t.Fatalf("typed %q", typed)
	}
}

func TestSessionAnswerForTheShowingPromptPressesTheKeys(t *testing.T) {
	c, host := promptSetup(t, domain.HarnessClaude, domain.StatePermission, claudeDialog, claudeDialog)
	shown, err := c.SessionPrompt(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), rpc.MethodSessionAnswer, rpc.AnswerParams{Session: "a", Choice: "3", Prompt: shown.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := host.typedNow(), []string{"%3 keys 3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typed %q, want %q", got, want)
	}
}
