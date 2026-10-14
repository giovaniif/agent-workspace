package git

import (
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

func TestWorktreeDetectParseWorktreeList(t *testing.T) {
	out := strings.Join([]string{
		"worktree /w/api", "HEAD 1111", "branch refs/heads/main", "",
		"worktree /w/api-feat", "HEAD 2222", "branch refs/heads/feat/x", "",
		"worktree /w/api-detached", "HEAD 3333", "detached", "",
		"worktree /w/api-gone", "HEAD 4444", "branch refs/heads/g", "prunable gitdir file points to non-existent location", "",
		"worktree /w/api-locked", "HEAD 5555", "branch refs/heads/l", "locked", "",
		"",
	}, "\x00")
	got := parseWorktreeList([]byte(out))
	want := domain.RepoListing{Main: "/w/api", Worktrees: []domain.ListedWorktree{
		{Path: "/w/api-feat", Branch: "feat/x"},
		{Path: "/w/api-detached"},
		{Path: "/w/api-locked", Branch: "l"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWorktreeDetectParseWorktreeListBareMain(t *testing.T) {
	out := "worktree /w/api.git\x00bare\x00\x00worktree /w/api-a\x00HEAD 1\x00branch refs/heads/a\x00\x00"
	got := parseWorktreeList([]byte(out))
	want := domain.RepoListing{Main: "/w/api.git", Worktrees: []domain.ListedWorktree{{Path: "/w/api-a", Branch: "a"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
