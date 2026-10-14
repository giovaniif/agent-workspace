package daemon_test

import (
	"context"
	"errors"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type portsEnv struct {
	path  string
	table *fakeTable
	env   wtEnv
	store *memStore
}

func startPorts(t *testing.T, listeners ...domain.Listener) portsEnv {
	t.Helper()
	store := &memStore{}
	table := &fakeTable{}
	table.set(listeners...)
	store.snap.Workspaces = append(store.snap.Workspaces, domain.Workspace{Root: "/solo", Kind: domain.WorkspaceSingle, Repos: []domain.Repo{{Name: "solo", Path: "/solo"}}})
	env := wtEnv{lister: &fakeLister{listings: map[string]domain.RepoListing{}}, finder: &fakeFinder{prs: map[string][]domain.PullRequest{}}}
	env.lister.set("/solo", domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"}, domain.ListedWorktree{Path: "/solo-web", Branch: "web"})
	_, path := start(t, store,
		daemon.WithWorkspaces(soloFS, fakeGit{}),
		daemon.WithRefreshInterval(0),
		daemon.WithWorktrees(env.lister, env.finder),
		daemon.WithWorktreePoll(50*time.Millisecond, time.Hour),
		daemon.WithProcessTable(table),
		daemon.WithPortsPoll(30*time.Millisecond),
	)
	return portsEnv{path: path, table: table, env: env, store: store}
}

func portsOf(st rpc.State, id string) []domain.Port {
	w, _ := worktree(st, id)
	return w.Ports
}

var (
	apiServer = domain.Listener{Port: 8081, PID: 101, PGID: 100, Command: "node", Cwd: "/solo-feat/apps/api"}
	webServer = domain.Listener{Port: 3000, PID: 201, PGID: 200, Command: "bun", Cwd: "/solo-web"}
)

func TestPortsShowOnTheWorktreeTheirCwdIsIn(t *testing.T) {
	p := startPorts(t, apiServer, webServer, domain.Listener{Port: 5000, PID: 9, PGID: 9, Command: "other", Cwd: "/elsewhere"})
	st := eventually(t, p.path, 2*time.Second, "ports mapped", func(st rpc.State) bool {
		return len(portsOf(st, "/solo-feat")) > 0 && len(portsOf(st, "/solo-web")) > 0
	})
	if got, want := portsOf(st, "/solo-feat"), []domain.Port{{Port: 8081, PID: 101, PGID: 100, Command: "node"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("feat ports = %+v, want %+v", got, want)
	}
	if got, want := portsOf(st, "/solo-web"), []domain.Port{{Port: 3000, PID: 201, PGID: 200, Command: "bun"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("web ports = %+v, want %+v", got, want)
	}
}

func TestPortsDisappearWhenTheListenerDoes(t *testing.T) {
	p := startPorts(t, apiServer)
	eventually(t, p.path, 2*time.Second, "port shown", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 1 })
	p.table.set()
	eventually(t, p.path, 2*time.Second, "port gone", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 0 })
}

func TestPortsOfAWorktreeFoundLaterAreShownAtOnce(t *testing.T) {
	p := startPorts(t, domain.Listener{Port: 4000, PID: 301, PGID: 300, Command: "node", Cwd: "/solo-late"})
	eventually(t, p.path, 2*time.Second, "baseline", func(st rpc.State) bool { _, ok := worktree(st, "/solo-feat"); return ok })
	p.env.lister.set("/solo",
		domain.ListedWorktree{Path: "/solo-feat", Branch: "feat"},
		domain.ListedWorktree{Path: "/solo-late", Branch: "late"})
	eventually(t, p.path, 2*time.Second, "late worktree carries its port", func(st rpc.State) bool {
		return len(portsOf(st, "/solo-late")) == 1
	})
}

func TestPortsAreNotPersisted(t *testing.T) {
	p := startPorts(t, apiServer)
	eventually(t, p.path, 2*time.Second, "port shown", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 1 })
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	for _, w := range p.store.snap.Worktrees {
		if len(w.Ports) > 0 {
			t.Errorf("stored %+v with ports", w)
		}
	}
}

func TestPortsAreNotScannedWithoutWorktrees(t *testing.T) {
	store := &memStore{}
	table := &fakeTable{}
	start(t, store, daemon.WithProcessTable(table), daemon.WithPortsPoll(10*time.Millisecond))
	time.Sleep(200 * time.Millisecond)
	if n := table.scanCount(); n != 0 {
		t.Errorf("scanned %d times with no worktrees", n)
	}
}

func kill(t *testing.T, path string, pgids ...int) ([]int, error) {
	t.Helper()
	return dial(t, path).KillPorts(context.Background(), pgids)
}

func TestPortsKillTerminatesTheGroupAndItsPortGoesOnTheNextRefresh(t *testing.T) {
	p := startPorts(t, apiServer, webServer)
	eventually(t, p.path, 2*time.Second, "ports shown", func(st rpc.State) bool {
		return len(portsOf(st, "/solo-feat")) == 1 && len(portsOf(st, "/solo-web")) == 1
	})
	got, err := kill(t, p.path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{100}) || !reflect.DeepEqual(p.table.killed(), []int{100}) {
		t.Errorf("killed %v, terminated %v; want [100]", got, p.table.killed())
	}
	st := eventually(t, p.path, 2*time.Second, "killed port gone", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 0 })
	if n := len(portsOf(st, "/solo-web")); n != 1 {
		t.Errorf("web has %d ports, want 1", n)
	}
}

func TestPortsKillRefusesGroupsThatServeNoPort(t *testing.T) {
	p := startPorts(t, apiServer)
	eventually(t, p.path, 2*time.Second, "port shown", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 1 })
	_, err := kill(t, p.path, 555)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("got %v, want not_found", err)
	}
	if k := p.table.killed(); len(k) != 0 {
		t.Errorf("terminated %v", k)
	}
}

func TestPortsKillNeverSignalsTheDaemonsOwnGroup(t *testing.T) {
	own := syscall.Getpgrp()
	p := startPorts(t, domain.Listener{Port: 8081, PID: own, PGID: own, Command: "agentws", Cwd: "/solo-feat"})
	eventually(t, p.path, 2*time.Second, "port shown", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 1 })
	if _, err := kill(t, p.path, own); err == nil {
		t.Fatal("kill of the daemon's own group succeeded")
	}
	if k := p.table.killed(); len(k) != 0 {
		t.Errorf("terminated %v", k)
	}
}

func TestPortsKillFailureKeepsThePort(t *testing.T) {
	p := startPorts(t, apiServer)
	eventually(t, p.path, 2*time.Second, "port shown", func(st rpc.State) bool { return len(portsOf(st, "/solo-feat")) == 1 })
	p.table.failTerminate(errors.New("survived"))
	_, err := kill(t, p.path, 100)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeFailed {
		t.Fatalf("got %v, want failed", err)
	}
	if n := len(portsOf(snapshot(t, p.path), "/solo-feat")); n != 1 {
		t.Errorf("feat has %d ports, want 1", n)
	}
}

func TestPortsKillUnavailableWithoutAProcessTable(t *testing.T) {
	_, path := start(t, &memStore{})
	_, err := kill(t, path, 100)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable {
		t.Fatalf("got %v, want unavailable", err)
	}
}
