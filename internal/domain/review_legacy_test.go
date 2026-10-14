package domain

import (
	"reflect"
	"testing"
)

func TestTurnsUnderTheOldRefPrefixStillCount(t *testing.T) {
	wt := "/src/api-feat"
	key := WorktreeKey(wt)
	old3 := "refs/agentws/turns/s1/" + key + "/3"
	old4 := "refs/agentws/turns/s1/" + key + "/4"
	refs := []string{old3, old4, TurnRef("s1", wt, 5)}
	if latest, n := LatestTurn(refs, "s1", wt); latest != TurnRef("s1", wt, 5) || n != 5 {
		t.Errorf("LatestTurn = %q, %d", latest, n)
	}
	if latest, n := LatestTurn(refs[:2], "s1", wt); latest != old4 || n != 4 {
		t.Errorf("LatestTurn with only old refs = %q, %d", latest, n)
	}
	if got, want := OlderTurns(refs, "s1", wt, 5), []string{old3, old4}; !reflect.DeepEqual(got, want) {
		t.Errorf("OlderTurns = %v, want %v", got, want)
	}
	if got := TurnsOfWorktree(refs, wt); !reflect.DeepEqual(got, refs) {
		t.Errorf("TurnsOfWorktree = %v, want %v", got, refs)
	}
}
