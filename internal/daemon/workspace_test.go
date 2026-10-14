package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
	"github.com/giovaniif/crew/internal/version"
)

var shop = fakeFS{
	markers: map[string]domain.GitMarker{"/shop": domain.GitNone, "/solo": domain.GitDir},
	children: map[string][]domain.Child{"/shop": {
		{Name: "api", Path: "/shop/api", Git: domain.GitDir},
		{Name: "web", Path: "/shop/web", Git: domain.GitDir},
		{Name: "api-wt", Path: "/shop/api-wt", Git: domain.GitFile},
	}},
}

var shopGit = fakeGit{"/shop/api": {DefaultBranch: "main", Branch: "feat", ChangedFiles: 2}}

func startWorkspaces(t *testing.T, store app.Store) (*rpc.Client, string) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	_, path := start(t, store, daemon.WithWorkspaces(shop, shopGit), daemon.WithClock(clock.Now))
	return dial(t, path), path
}

func TestWorkspaceAddDiscoversReposAndRefreshesThemInTheBackground(t *testing.T) {
	c, _ := startWorkspaces(t, &memStore{})
	ctx := context.Background()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}

	ws, err := c.WorkspaceAdd(ctx, "/shop")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != domain.WorkspaceOrchestration || len(ws.Repos) != 2 || ws.Repos[0].Name != "api" || ws.Repos[1].Name != "web" {
		t.Fatalf("added = %+v", ws)
	}
	if ws.Repos[0].Branch != "" {
		t.Errorf("add ran git before answering: %+v", ws.Repos[0])
	}

	first := next(t, sub.Diffs)
	if first.Workspace == nil || first.Workspace.Root != "/shop" || first.Workspace.Repos[0].Branch != "" {
		t.Fatalf("first diff = %+v", first)
	}
	second := next(t, sub.Diffs)
	want := domain.Repo{Name: "api", Path: "/shop/api", DefaultBranch: "main", Branch: "feat", ChangedFiles: 2}
	if second.Workspace == nil || !reflect.DeepEqual(second.Workspace.Repos[0], want) {
		t.Fatalf("second diff = %+v", second)
	}
	if second.Workspace.Repos[1].Branch != "" {
		t.Errorf("web has no git facts, got %+v", second.Workspace.Repos[1])
	}
}

func TestWorkspaceAddSingleRepo(t *testing.T) {
	c, _ := startWorkspaces(t, &memStore{})
	ws, err := c.WorkspaceAdd(context.Background(), "/solo")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != domain.WorkspaceSingle || len(ws.Repos) != 1 || ws.Repos[0].Name != "solo" {
		t.Errorf("added = %+v", ws)
	}
}

func TestWorkspaceListRemembersTheLastUsed(t *testing.T) {
	store := &memStore{}
	c, _ := startWorkspaces(t, store)
	ctx := context.Background()
	for _, p := range []string{"/shop", "/solo", "/shop"} {
		if _, err := c.WorkspaceAdd(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	list, err := c.WorkspaceList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Workspaces) != 2 || list.LastUsed != "/shop" {
		t.Errorf("list = %+v, want 2 workspaces with /shop last used", list)
	}
}

func TestWorkspaceLastUsedSurvivesARestart(t *testing.T) {
	store := &memStore{}
	c, _ := startWorkspaces(t, store)
	ctx := context.Background()
	for _, p := range []string{"/shop", "/solo"} {
		if _, err := c.WorkspaceAdd(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool {
		snap, _ := store.Load()
		return len(snap.Workspaces) >= 2
	})

	_, path := start(t, store, daemon.WithWorkspaces(shop, shopGit))
	list, err := dial(t, path).WorkspaceList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list.LastUsed != "/solo" {
		t.Errorf("last used after restart = %q, want /solo", list.LastUsed)
	}
}

func TestWorkspaceReAddKeepsRefreshedFacts(t *testing.T) {
	c, _ := startWorkspaces(t, &memStore{})
	ctx := context.Background()
	if _, err := c.WorkspaceAdd(ctx, "/shop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		list, _ := c.WorkspaceList(ctx)
		return len(list.Workspaces) == 1 && list.Workspaces[0].Repos[0].Branch == "feat"
	})
	ws, err := c.WorkspaceAdd(ctx, "/shop")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Repos[0].Branch != "feat" {
		t.Errorf("re-add dropped refreshed facts: %+v", ws.Repos[0])
	}
}

func TestWorkspaceRemove(t *testing.T) {
	store := &memStore{}
	c, _ := startWorkspaces(t, store)
	ctx := context.Background()
	if _, err := c.WorkspaceAdd(ctx, "/shop"); err != nil {
		t.Fatal(err)
	}
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WorkspaceRemove(ctx, "/shop"); err != nil {
		t.Fatal(err)
	}
	for {
		d := next(t, sub.Diffs)
		if d.RemovedWorkspace == "/shop" {
			break
		}
	}
	list, err := c.WorkspaceList(ctx)
	if err != nil || len(list.Workspaces) != 0 || list.LastUsed != "" {
		t.Errorf("list = %+v, %v", list, err)
	}
	waitFor(t, func() bool {
		snap, _ := store.Load()
		return len(snap.Workspaces) == 0
	})

	var rerr *rpc.Error
	if err := c.WorkspaceRemove(ctx, "/shop"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Errorf("removing twice: %v", err)
	}
}

func TestWorkspaceRefreshDoesNotResurrectARemovedWorkspace(t *testing.T) {
	c, _ := startWorkspaces(t, &memStore{})
	ctx := context.Background()
	for range 20 {
		if _, err := c.WorkspaceAdd(ctx, "/shop"); err != nil {
			t.Fatal(err)
		}
		if err := c.WorkspaceRemove(ctx, "/shop"); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	list, err := c.WorkspaceList(ctx)
	if err != nil || len(list.Workspaces) != 0 {
		t.Errorf("list = %+v, %v", list, err)
	}
}

func TestWorkspaceAddRejectsBadInput(t *testing.T) {
	c, path := startWorkspaces(t, &memStore{})
	ctx := context.Background()
	var rerr *rpc.Error
	for _, p := range []string{"/missing", "", "relative/dir"} {
		if _, err := c.WorkspaceAdd(ctx, p); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
			t.Errorf("add %q: %v", p, err)
		}
	}
	resp := rawCall(t, path, fmt.Sprintf(`{"v":1,"id":1,"method":"workspace.add","params":"nope","build":%q}`, version.String()))
	if resp.Error == nil || resp.Error.Code != rpc.CodeBadRequest {
		t.Errorf("bad params: %+v", resp)
	}
}

func TestWorkspaceMethodsAreUnknownWithoutWorkspaces(t *testing.T) {
	_, path := start(t, &memStore{})
	var rerr *rpc.Error
	if _, err := dial(t, path).WorkspaceList(context.Background()); !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnknownMethod {
		t.Errorf("err = %v", err)
	}
}

func TestWorkspaceAddDoesNotStallTheLoop(t *testing.T) {
	gate := make(chan struct{})
	slow := shop
	slow.gate = gate
	_, path := start(t, &memStore{}, daemon.WithWorkspaces(slow, shopGit))
	adder, other := dial(t, path), dial(t, path)
	added := make(chan error, 1)
	go func() {
		_, err := adder.WorkspaceAdd(context.Background(), "/shop")
		added <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := other.Status(context.Background()); err != nil {
		t.Fatalf("status while add is blocked on the filesystem: %v", err)
	}
	close(gate)
	if err := <-added; err != nil {
		t.Fatal(err)
	}
}

func TestWorkspacesAreRefreshedPeriodically(t *testing.T) {
	git := &liveGit{}
	git.branch.Store("one")
	_, path := start(t, &memStore{}, daemon.WithWorkspaces(shop, git), daemon.WithRefreshInterval(10*time.Millisecond))
	c := dial(t, path)
	ctx := context.Background()
	if _, err := c.WorkspaceAdd(ctx, "/solo"); err != nil {
		t.Fatal(err)
	}
	branchIs := func(want string) func() bool {
		return func() bool {
			list, _ := c.WorkspaceList(ctx)
			return len(list.Workspaces) == 1 && list.Workspaces[0].Repos[0].Branch == want
		}
	}
	waitFor(t, branchIs("one"))
	git.branch.Store("two")
	waitFor(t, branchIs("two"))
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWorkspaceDirsListsSubfoldersWithoutAddingTheFolder(t *testing.T) {
	c, _ := startWorkspaces(t, &memStore{})
	ctx := context.Background()
	dirs, err := c.WorkspaceDirs(ctx, "/shop")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dirs.Dirs, shop.children["/shop"]) {
		t.Errorf("dirs = %+v", dirs)
	}
	if list, err := c.WorkspaceList(ctx); err != nil || len(list.Workspaces) != 0 {
		t.Errorf("listing a folder registered it: %+v, %v", list, err)
	}
	var rerr *rpc.Error
	if _, err := c.WorkspaceDirs(ctx, "relative"); !errors.As(err, &rerr) || rerr.Code != rpc.CodeBadRequest {
		t.Errorf("relative path: %v", err)
	}
}
