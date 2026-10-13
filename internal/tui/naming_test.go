package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
	"github.com/giovaniif/agent-workspace/internal/tui"
)

const longName = "Retry failed uploads with exponential backoff and jitter across regions"

var keyCtrlU = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}

func namingState(task domain.Task) rpc.State {
	return rpc.State{
		Tasks:     []domain.Task{task},
		Sessions:  []domain.Session{{ID: "s1", TaskID: task.ID, Harness: domain.HarnessClaude, WorktreeIDs: []string{"w1"}}},
		Worktrees: []domain.Worktree{{ID: "w1", Repo: "api", Branch: "b"}},
	}
}

func namingModel(t *testing.T, task domain.Task) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	st := namingState(task)
	return update(m, tui.StateMsg(st)), c
}

func TestNamingSidebarTruncatesWithAnEllipsisAndTheCardShowsTheFullName(t *testing.T) {
	m, _ := namingModel(t, domain.Task{ID: "t1", Source: domain.TaskPR, Ref: "api#1", PRTitle: longName})
	out := screen(m)
	var row string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, " 1 ") && strings.Contains(l, "CC") {
			row = l
		}
	}
	if !strings.Contains(row, "Retry failed") || !strings.Contains(row, "…") || strings.Contains(row, "jitter") {
		t.Fatalf("session row should be cut with an ellipsis:\n%q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "CC") {
		t.Errorf("the harness tag was pushed out of the row: %q", row)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 48 {
			t.Errorf("line is %d wide, over the sidebar: %q", w, l)
		}
	}
}

func TestNamingSwitchesToThePRTitleWhenOneAppearsUnlessPinned(t *testing.T) {
	task := domain.Task{ID: "t1", Source: domain.TaskText, Text: "Fix login redirect"}
	m, _ := namingModel(t, task)
	if out := screen(m); !strings.Contains(out, "1 Fix login redirect") {
		t.Fatalf("before a PR the row carries the task text:\n%s", out)
	}
	wt := domain.Worktree{ID: "w1", Repo: "api", Branch: "b", PR: &domain.PullRequest{Number: 9, Title: "Redirect after login"}}
	m = update(m, tui.DiffMsg(rpc.Diff{Seq: 1, Worktree: &wt}))
	if out := screen(m); !strings.Contains(out, "1 Redirect after login") {
		t.Fatalf("after the PR the row carries its title:\n%s", out)
	}

	pinned, _ := namingModel(t, domain.Task{ID: "t1", Source: domain.TaskText, Text: "Fix login redirect", PinnedName: "login work"})
	pinned = update(pinned, tui.DiffMsg(rpc.Diff{Seq: 1, Worktree: &wt}))
	if out := screen(pinned); !strings.Contains(out, "1 login work") || strings.Contains(out, "1 Redirect after login") {
		t.Fatalf("a pinned name should stay:\n%s", out)
	}
}

func TestNamingRKeyRenamesAndPins(t *testing.T) {
	m, c := namingModel(t, domain.Task{ID: "t1", Source: domain.TaskText, Text: "Fix login redirect"})
	m = press(m, "R")
	out := screen(m)
	if !strings.Contains(out, "RENAME") || !strings.Contains(out, "Fix login redirect") {
		t.Fatalf("R should open a prompt with the current name:\n%s", out)
	}
	m = update(m, keyCtrlU)
	m = typeText(m, "login rework")
	m = pressCmd(m, keyEnter)
	if len(c.calls) != 1 || c.calls[0].method != rpc.MethodSessionRename ||
		c.calls[0].params != (rpc.SessionRenameParams{ID: "s1", Name: "login rework"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	if out := screen(m); strings.Contains(out, "RENAME") {
		t.Errorf("the prompt should close on enter:\n%s", out)
	}
}

func TestNamingRenamePromptEditsAndCancels(t *testing.T) {
	m, c := namingModel(t, domain.Task{ID: "t1", Text: "tidy the parser"})
	m = press(m, "R")
	m = update(m, keyBack)
	m = typeText(m, "ser")
	if out := screen(m); !strings.Contains(out, "tidy the parser") {
		t.Fatalf("backspace then typing should edit in place:\n%s", out)
	}
	if m = pressCmd(m, keyEsc); len(c.calls) != 0 || strings.Contains(screen(m), "RENAME") {
		t.Fatalf("esc should cancel without a call: %+v", c.calls)
	}
	m = press(m, "R")
	m = update(m, keyCtrlU)
	if m = pressCmd(m, keyEnter); len(c.calls) != 0 {
		t.Fatalf("a blank name should not be sent: %+v", c.calls)
	}
	if out := screen(m); strings.Contains(out, "RENAME") {
		t.Errorf("a blank name closes the prompt:\n%s", out)
	}
	m = press(m, "R", "x")
	if out := screen(m); len(c.calls) != 0 || !strings.Contains(out, "RENAME") || !strings.Contains(out, "tidy the parserx") {
		t.Fatalf("keys typed into the prompt must edit it, not act on the sidebar (calls %+v):\n%s", c.calls, out)
	}
}

func TestNamingRenamePromptKeepsTheCursorVisibleForWideNames(t *testing.T) {
	wide := strings.Repeat("界", 30)
	m, _ := namingModel(t, domain.Task{ID: "t1", Text: "x"})
	m = press(m, "R")
	m = update(m, keyCtrlU)
	m = typeText(m, wide+"end")
	var prompt string
	for _, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, "RENAME") {
			prompt = l
		}
	}
	if !strings.Contains(prompt, "end▏") || !strings.Contains(prompt, "界") {
		t.Errorf("the prompt should end at the cursor with the tail of the text: %q", prompt)
	}
	if w := ansi.StringWidth(prompt); w > 48 {
		t.Errorf("prompt is %d cells wide, over the pane: %q", w, prompt)
	}
}

func TestNamingAKeyUnpinsOnlyAPinnedName(t *testing.T) {
	m, c := namingModel(t, domain.Task{ID: "t1", Text: "tidy the parser", PinnedName: "mine"})
	pressCmd(m, key("A"))
	if len(c.calls) != 1 || c.calls[0].method != rpc.MethodSessionUnpin || c.calls[0].params != (rpc.SessionRef{ID: "s1"}) {
		t.Fatalf("calls %+v", c.calls)
	}
	unpinned, c2 := namingModel(t, domain.Task{ID: "t1", Text: "tidy the parser"})
	pressCmd(unpinned, key("A"))
	if len(c2.calls) != 0 {
		t.Fatalf("nothing pinned, nothing to unpin: %+v", c2.calls)
	}
}

func TestNamingHelpListsRenameAndUnpin(t *testing.T) {
	m, _ := namingModel(t, domain.Task{ID: "t1", Text: "x"})
	out := screen(press(m, "?"))
	for _, want := range []string{"rename and pin", "unpin"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q:\n%s", want, out)
		}
	}
}
