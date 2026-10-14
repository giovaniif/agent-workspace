//go:build integration

package git_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/git"
	"github.com/giovaniif/crew/internal/domain"
)

func TestReviewListsTurnRefsFromBothPrefixes(t *testing.T) {
	repo := reviewRepo(t)
	head := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	old := "refs/agentws/turns/s1/" + domain.WorktreeKey(repo) + "/2"
	gitOut(t, repo, "update-ref", old, head)
	gitOut(t, repo, "update-ref", domain.TurnRef("s1", repo, 3), head)
	refs, err := git.Review{}.TurnRefs(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{domain.TurnRef("s1", repo, 3), old}
	sort.Strings(refs)
	sort.Strings(want)
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
}
