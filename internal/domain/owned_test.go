package domain

import "testing"

const ownedMarker = "// agentws: written by agentws setup omp"

func TestPlanOwnedInstall(t *testing.T) {
	want := ownedMarker + "\nnew\n"
	cases := []struct {
		name    string
		current string
		exists  bool
		plan    OwnedPlan
	}{
		{"no file", "", false, OwnedWrite},
		{"same bytes", want, true, OwnedUnchanged},
		{"stale bytes of ours", ownedMarker + "\nold\n", true, OwnedWrite},
		{"a file of the user's", "export default function () {}\n", true, OwnedConflict},
		{"the marker below the first line", "// mine\n" + ownedMarker + "\n", true, OwnedConflict},
		{"an empty file of the user's", "", true, OwnedConflict},
	}
	for _, c := range cases {
		if got := PlanOwnedInstall(ownedMarker, []byte(c.current), c.exists, []byte(want)); got != c.plan {
			t.Errorf("%s: plan %v, want %v", c.name, got, c.plan)
		}
	}
}

func TestPlanOwnedRemove(t *testing.T) {
	cases := []struct {
		name    string
		current string
		exists  bool
		plan    OwnedPlan
	}{
		{"no file", "", false, OwnedUnchanged},
		{"ours", ownedMarker + "\nold\n", true, OwnedDelete},
		{"only the marker", ownedMarker, true, OwnedDelete},
		{"a file of the user's", "export default function () {}\n", true, OwnedConflict},
	}
	for _, c := range cases {
		if got := PlanOwnedRemove(ownedMarker, []byte(c.current), c.exists); got != c.plan {
			t.Errorf("%s: plan %v, want %v", c.name, got, c.plan)
		}
	}
}
