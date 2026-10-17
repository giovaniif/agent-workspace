package tui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/tui"
)

func withMates(st rpc.State) rpc.State {
	st.Mates = domain.MateBoard{Mates: []domain.MateStatus{
		{Name: "helper", State: domain.MateConnected},
		{Name: "webby", State: domain.MatePaused},
	}}
	st.MateThreads = []domain.MateLink{{Mate: "helper", Team: "T1", Channel: "C1", ThreadTS: "1700.0001", SessionID: st.Sessions[0].ID}}
	return st
}

func matesModel(t *testing.T, st rpc.State) (tui.Model, *fakeCaller) {
	t.Helper()
	c := &fakeCaller{asks: []domain.Approval{{
		ID: "a1", SessionID: st.Sessions[0].ID, Mate: "helper", Requester: "Gus",
		Action: domain.Action{Kind: domain.ActionMerge, Tool: "Bash", Input: "gh pr merge 12"}, At: clock(), State: domain.ApprovalPending,
	}}}
	m := tui.New(tui.Options{Theme: tui.Latte(), Now: clock, Calls: c})
	m = update(m, tea.WindowSizeMsg{Width: 48, Height: 40})
	m = update(m, tui.StateMsg(st))
	return pressCmd(m, key("C")), c
}

func lastCall(c *fakeCaller) call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[len(c.calls)-1]
}

func TestGoldenMates(t *testing.T) {
	m, _ := matesModel(t, withMates(fixture(1, 0)))
	golden.RequireEqual(t, screen(m))
}

func TestMatesViewShowsEachMateItsThreadsAndThePendingApprovals(t *testing.T) {
	m, c := matesModel(t, withMates(fixture(1, 0)))
	out := screen(m)
	for _, want := range []string{"MATES", "helper", "connected", "webby", "paused", "APPROVALS", "helper: merge", "Gus", "gh pr merge 12"} {
		if want == "gh pr merge 12" {
			m = press(m, "j", "j")
			out = screen(m)
		}
		if !strings.Contains(out, want) {
			t.Fatalf("mates view lacks %q:\n%s", want, out)
		}
	}
	if got := c.methods(); !slices.Equal(got, []string{rpc.MethodApprovalList}) {
		t.Fatalf("calls %v", got)
	}
}

func TestMatesViewPausesAndResumesTheSelectedMate(t *testing.T) {
	m, c := matesModel(t, withMates(fixture(1, 0)))
	m = pressCmd(m, key("p"))
	if got := lastCall(c); got.method != rpc.MethodMatePause || got.params != (rpc.MateNameParams{Name: "helper"}) {
		t.Fatalf("call %+v", got)
	}
	m = press(m, "j")
	pressCmd(m, key("p"))
	if got := lastCall(c); got.method != rpc.MethodMateResume || got.params != (rpc.MateNameParams{Name: "webby"}) {
		t.Fatalf("call %+v", got)
	}
}

func TestMatesKillSwitchAsksThenStopsAllMates(t *testing.T) {
	m, c := matesModel(t, withMates(fixture(1, 0)))
	m = pressCmd(m, key("X"))
	if !strings.Contains(screen(m), "stop all mates? y/n") {
		t.Fatalf("no confirm:\n%s", screen(m))
	}
	pressCmd(m, key("y"))
	if got := lastCall(c); got.method != rpc.MethodMateStopAll || got.params != (rpc.MateStopAllParams{Stop: true}) {
		t.Fatalf("call %+v", got)
	}
}

func TestMatesKillSwitchShowsOnTheSidebarAndStartsAgainWithoutAConfirm(t *testing.T) {
	m, c := matesModel(t, withMates(fixture(1, 0)))
	m = update(m, tui.DiffMsg(rpc.Diff{Mates: &domain.MateBoard{Stopped: true, Mates: []domain.MateStatus{{Name: "helper", State: domain.MateConnected, Guard: domain.GuardStopped}}}}))
	if out := screen(m); !strings.Contains(out, "all mates are stopped") || !strings.Contains(out, "connected · stopped") {
		t.Fatalf("panel does not show the stop:\n%s", out)
	}
	m = pressCmd(m, key("X"))
	if got := lastCall(c); got.method != rpc.MethodMateStopAll || got.params != (rpc.MateStopAllParams{Stop: false}) {
		t.Fatalf("call %+v", got)
	}
	m = press(m, "esc")
	if out := screen(m); !strings.Contains(out, "all mates stopped") {
		t.Fatalf("sidebar does not show the stop:\n%s", out)
	}
}

func TestMatesViewApprovesAndDeniesTheSelectedApproval(t *testing.T) {
	for _, c := range []struct {
		key     string
		approve bool
	}{{"y", true}, {"n", false}} {
		m, f := matesModel(t, withMates(fixture(1, 0)))
		m = press(m, "j", "j")
		pressCmd(m, key(c.key))
		var decided []call
		f.mu.Lock()
		for _, x := range f.calls {
			if x.method == rpc.MethodApprovalDecide {
				decided = append(decided, x)
			}
		}
		f.mu.Unlock()
		if len(decided) != 1 || decided[0].params != (rpc.ApprovalDecideParams{ID: "a1", Approve: c.approve}) {
			t.Fatalf("%s: decided %+v", c.key, decided)
		}
		if got := f.methods(); got[len(got)-1] != rpc.MethodApprovalList {
			t.Fatalf("%s: approvals not loaded again: %v", c.key, got)
		}
	}
}

func TestMatesViewClosesWithEsc(t *testing.T) {
	m, _ := matesModel(t, withMates(fixture(1, 0)))
	m = press(m, "esc")
	if out := screen(m); strings.Contains(out, "APPROVALS") {
		t.Fatalf("still open:\n%s", out)
	}
}

func TestMatesViewLoadsTheApprovalsAgainAfterAFailedDecision(t *testing.T) {
	m, f := matesModel(t, withMates(fixture(1, 0)))
	f.err, f.failOn = errors.New("the approval expired"), rpc.MethodApprovalDecide
	m = press(m, "j", "j")
	m = pressCmd(m, key("y"))
	if got := f.methods(); got[len(got)-1] != rpc.MethodApprovalList {
		t.Fatalf("approvals not loaded again: %v", got)
	}
	if out := screen(m); !strings.Contains(out, "the approval expired") || !strings.Contains(out, "helper: merge") {
		t.Fatalf("panel:\n%s", out)
	}
}
