package daemon_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestOpenClientCreatesTheLayoutOnceAndReattachesAfter(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	tui := rpc.OpenClientParams{Command: []string{"crew", "tui"}}

	first, err := c.OpenClient(ctx, tui)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.OpenClient(ctx, tui)
	if err != nil {
		t.Fatal(err)
	}
	if first.Slot != second.Slot || len(host.opened) != 1 {
		t.Fatalf("slots %q, %q after %d opens; want one layout reused", first.Slot, second.Slot, len(host.opened))
	}
	if !slices.Equal(first.Attach, []string{"tmux", "attach", "-t", first.Slot}) {
		t.Fatalf("attach = %v", first.Attach)
	}
	if got := host.opened[0]; !slices.Equal(got.Command, tui.Command) {
		t.Fatalf("tui pane spec = %+v; want the TUI command", got)
	}

	host.close(app.Slot(first.Slot))
	third, err := c.OpenClient(ctx, tui)
	if err != nil {
		t.Fatal(err)
	}
	if third.Slot == first.Slot || len(host.opened) != 2 {
		t.Fatalf("after the layout closed got slot %q, %d opens; want a new layout", third.Slot, len(host.opened))
	}
}

func TestFocusMainMovesFocusToTheSlot(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	var rerr *rpc.Error
	if err := c.FocusMain(ctx); !errors.As(err, &rerr) {
		t.Fatalf("focus before any layout = %v; want an rpc error", err)
	}
	opened, err := c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"crew", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FocusMain(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(host.focused, []app.Slot{app.Slot(opened.Slot)}) {
		t.Fatalf("focused = %v", host.focused)
	}
}

func TestClientMethodsWithoutATerminalHostFail(t *testing.T) {
	_, path := start(t, &memStore{})
	var rerr *rpc.Error
	_, err := dial(t, path).OpenClient(context.Background(), rpc.OpenClientParams{Command: []string{"crew", "tui"}})
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable {
		t.Fatalf("err = %v; want %s", err, rpc.CodeUnavailable)
	}
}

func TestDebugSeedAddsFakeSessionsWithWorktrees(t *testing.T) {
	_, path := start(t, &memStore{})
	c := dial(t, path)
	ctx := context.Background()
	if err := c.DebugSeed(ctx, rpc.DebugSeedParams{Count: 3}); err != nil {
		t.Fatal(err)
	}
	sub, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st := sub.State
	if len(st.Sessions) != 3 || len(st.Tasks) == 0 || len(st.Worktrees) < 3 {
		t.Fatalf("seeded %d sessions, %d tasks, %d worktrees", len(st.Sessions), len(st.Tasks), len(st.Worktrees))
	}
	tasks := map[string]bool{}
	for _, task := range st.Tasks {
		tasks[task.ID] = true
	}
	worktrees := map[string]bool{}
	for _, w := range st.Worktrees {
		worktrees[w.ID] = true
	}
	for _, s := range st.Sessions {
		if !tasks[s.TaskID] {
			t.Errorf("session %s has unknown task %s", s.ID, s.TaskID)
		}
		for _, id := range s.WorktreeIDs {
			if !worktrees[id] {
				t.Errorf("session %s has unknown worktree %s", s.ID, id)
			}
		}
	}
	if err := c.DebugSeed(ctx, rpc.DebugSeedParams{Count: 0}); err == nil {
		t.Fatal("seeding 0 sessions succeeded")
	}
}

func TestClientPopupRunsTheCommandInAPopup(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	p := rpc.ClientPopupParams{Command: []string{"crew", "tui", "--new-session"}, Env: map[string]string{"CREW_HOME": "/h"}}
	if err := c.Call(ctx, rpc.MethodClientPopup, p, nil); err != nil {
		t.Fatal(err)
	}
	if len(host.commandPopups) != 1 || !slices.Equal(host.commandPopups[0].Command, p.Command) || host.commandPopups[0].Env["CREW_HOME"] != "/h" {
		t.Fatalf("popups %+v", host.commandPopups)
	}
	var rerr *rpc.Error
	if err := c.Call(ctx, rpc.MethodClientPopup, rpc.ClientPopupParams{}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Fatalf("empty command = %v; want %s", err, rpc.CodeBadRequest)
	}
}

func TestClientDetachLeavesTheLayout(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	var rerr *rpc.Error
	if err := c.Call(ctx, rpc.MethodClientDetach, struct{}{}, nil); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("detach before any layout = %v; want an rpc error", err)
	}
	opened, err := c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"crew", "tui"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, rpc.MethodClientDetach, struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(host.detached, []app.Slot{app.Slot(opened.Slot)}) {
		t.Fatalf("detached %v", host.detached)
	}
}

func TestClientPopupOpensWhereCrewWasLaunched(t *testing.T) {
	d, path := start(t, &memStore{})
	host := &fakeClientHost{}
	d.SetClientHost(host)
	c := dial(t, path)
	ctx := context.Background()
	if _, err := c.OpenClient(ctx, rpc.OpenClientParams{Command: []string{"crew", "tui"}, Dir: "/work/api"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, rpc.MethodClientPopup, rpc.ClientPopupParams{Command: []string{"crew", "tui", "--new-session"}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(host.commandPopups) != 1 || host.commandPopups[0].Dir != "/work/api" {
		t.Fatalf("popups %+v; want it to start in /work/api", host.commandPopups)
	}
}
