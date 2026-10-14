package codex

import (
	"os"
	"testing"

	"github.com/giovaniif/crew/internal/app"
)

func envOf(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func parents(tree map[int]int) func(int) (int, bool) {
	return func(pid int) (int, bool) {
		p, ok := tree[pid]
		return p, ok
	}
}

func TestPaneComesFromTmuxPaneWhenSet(t *testing.T) {
	got, ok := ResolvePane(envOf(map[string]string{"TMUX_PANE": "%7"}), 500, parents(nil), nil)
	if !ok || got != "%7" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestPaneFallsBackToTheAncestorThatIsAPaneProcess(t *testing.T) {
	tree := map[int]int{500: 400, 400: 300, 300: 1}
	panes := map[int]app.PaneID{300: "%3", 999: "%9"}
	got, ok := ResolvePane(envOf(nil), 500, parents(tree), panes)
	if !ok || got != "%3" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestPaneWalkPrefersTheNearestAncestor(t *testing.T) {
	tree := map[int]int{500: 400, 400: 300, 300: 1}
	panes := map[int]app.PaneID{400: "%4", 300: "%3"}
	if got, _ := ResolvePane(envOf(nil), 500, parents(tree), panes); got != "%4" {
		t.Fatalf("got %q", got)
	}
}

func TestPaneWalkGivesUpAtTheRootOrOnACycle(t *testing.T) {
	panes := map[int]app.PaneID{300: "%3"}
	if _, ok := ResolvePane(envOf(nil), 500, parents(map[int]int{500: 1}), panes); ok {
		t.Fatal("resolved a pane outside the tree")
	}
	if _, ok := ResolvePane(envOf(nil), 500, parents(map[int]int{500: 400, 400: 500}), panes); ok {
		t.Fatal("resolved a pane on a cycle")
	}
	if _, ok := ResolvePane(envOf(nil), 0, parents(nil), panes); ok {
		t.Fatal("resolved pid 0")
	}
}

func TestProcessParentOfThisProcessIsTheRealParent(t *testing.T) {
	got, ok := ProcessParent(os.Getpid())
	if !ok || got != os.Getppid() {
		t.Fatalf("got %d, %v; want %d", got, ok, os.Getppid())
	}
	if _, ok := ProcessParent(1 << 30); ok {
		t.Fatal("found a parent for a pid that does not exist")
	}
}
