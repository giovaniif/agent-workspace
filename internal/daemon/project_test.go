package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/daemon"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

var shopWithEmpty = fakeFS{
	markers:  map[string]domain.GitMarker{"/shop": domain.GitNone, "/solo": domain.GitDir, "/empty": domain.GitNone},
	children: shop.children,
}

func startProjects(t *testing.T, store *memStore) (*rpc.Client, string) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	_, path := start(t, store, daemon.WithWorkspaces(shopWithEmpty, shopGit), daemon.WithClock(clock.Now))
	return dial(t, path), path
}

func TestProjectAddRegistersTheWorkspaceAndSendsADiff(t *testing.T) {
	c, path := startProjects(t, &memStore{})
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.ProjectAdd(ctx, rpc.ProjectAddParams{Path: "/shop/", Setup: " make env "})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Project{Root: "/shop", Name: "shop", Setup: "make env"}
	if p != want {
		t.Fatalf("added = %+v", p)
	}
	for {
		d := next(t, sub.Diffs)
		if d.Project != nil {
			if *d.Project != want {
				t.Errorf("diff project = %+v", d.Project)
			}
			break
		}
	}
	list, err := c.WorkspaceList(ctx)
	if err != nil || len(list.Workspaces) != 1 || list.Workspaces[0].Root != "/shop" {
		t.Errorf("workspaces = %+v, %v", list, err)
	}
	state, err := dial(t, path).Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.State.Projects, []domain.Project{want}) {
		t.Errorf("state projects = %+v", state.State.Projects)
	}
}

func TestProjectAddRejectsFoldersThatAreNotWorkspaces(t *testing.T) {
	c, _ := startProjects(t, &memStore{})
	ctx := context.Background()
	var rerr *rpc.Error
	for _, p := range []string{"/empty", "/missing", "relative/dir", ""} {
		if _, err := c.ProjectAdd(ctx, rpc.ProjectAddParams{Path: p}); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
			t.Errorf("add %q: %v", p, err)
		}
	}
	list, err := c.ProjectList(ctx)
	if err != nil || len(list.Projects) != 0 {
		t.Errorf("projects = %+v, %v", list, err)
	}
}

func TestProjectsSurviveARestartAndCanBeRemoved(t *testing.T) {
	store := &memStore{}
	c, _ := startProjects(t, store)
	ctx := context.Background()
	if _, err := c.ProjectAdd(ctx, rpc.ProjectAddParams{Path: "/solo", Name: "Solo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ProjectAdd(ctx, rpc.ProjectAddParams{Path: "/shop"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snap, _ := store.Load()
		return len(snap.Projects) == 2
	})

	_, path := start(t, store, daemon.WithWorkspaces(shopWithEmpty, shopGit))
	again := dial(t, path)
	list, err := again.ProjectList(ctx)
	want := []domain.Project{{Root: "/shop", Name: "shop"}, {Root: "/solo", Name: "Solo"}}
	if err != nil || !reflect.DeepEqual(list.Projects, want) {
		t.Fatalf("after restart = %+v, %v", list, err)
	}

	sub, err := again.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.ProjectRemove(ctx, "/shop"); err != nil {
		t.Fatal(err)
	}
	for {
		if d := next(t, sub.Diffs); d.RemovedProject == "/shop" {
			break
		}
	}
	waitFor(t, func() bool {
		snap, _ := store.Load()
		return len(snap.Projects) == 1 && snap.Projects[0].Root == "/solo"
	})
	ws, err := again.WorkspaceList(ctx)
	if err != nil || len(ws.Workspaces) != 2 {
		t.Errorf("removing a project dropped its workspace: %+v, %v", ws, err)
	}
	var rerr *rpc.Error
	if err := again.ProjectRemove(ctx, "/shop"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("removing twice: %v", err)
	}
}
