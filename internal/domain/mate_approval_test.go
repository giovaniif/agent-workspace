package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecideApproval(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	push := Action{Kind: ActionPush, Tool: "Bash", Input: `{"command":"git push"}`}
	pending := Approval{ID: "a1", SessionID: "s", Mate: "ada", Action: push, At: now.Add(-time.Hour), State: ApprovalPending}
	cases := []struct {
		name  string
		a     Approval
		by    string
		owner string
		allow bool
		want  ApprovalState
		err   error
	}{
		{"owner approves", pending, "U1", "U1", true, ApprovalGranted, nil},
		{"owner denies", pending, "U1", "U1", false, ApprovalDenied, nil},
		{"a click of another user does nothing", pending, "U2", "U1", true, ApprovalPending, ErrApprovalNotOwner},
		{"no owner nobody decides", pending, "", "", true, ApprovalPending, ErrApprovalNotOwner},
		{"decided once only", Approval{ID: "a1", Action: push, At: now, State: ApprovalGranted}, "U1", "U1", false, ApprovalGranted, ErrApprovalDecided},
		{"used is decided", Approval{ID: "a1", Action: push, At: now, State: ApprovalUsed}, "U1", "U1", true, ApprovalUsed, ErrApprovalDecided},
		{"expired cannot be approved", Approval{ID: "a1", Action: push, At: now.Add(-ApprovalTTL - time.Second), State: ApprovalPending}, "U1", "U1", true, ApprovalPending, ErrApprovalExpired},
		{"at the limit it can", Approval{ID: "a1", Action: push, At: now.Add(-ApprovalTTL), State: ApprovalPending}, "U1", "U1", true, ApprovalGranted, nil},
	}
	for _, c := range cases {
		got, err := DecideApproval(c.a, c.by, c.owner, c.allow, now)
		if !errors.Is(err, c.err) || got.State != c.want || got.ID != c.a.ID || got.Action != c.a.Action {
			t.Errorf("%s: got %+v %v, want %s %v", c.name, got, err, c.want, c.err)
		}
	}
}

func TestApprovalExpired(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	if (Approval{At: now.Add(-ApprovalTTL)}).Expired(now) {
		t.Fatal("expired at the limit")
	}
	if !(Approval{At: now.Add(-ApprovalTTL - time.Nanosecond)}).Expired(now) {
		t.Fatal("not expired after the limit")
	}
}

func TestApprovalTexts(t *testing.T) {
	a := Approval{ID: "a1", Mate: "ada", Requester: "U2", Action: Action{Kind: ActionCreatePR, Tool: "Bash", Input: `{"command":"gh pr create"}`}}
	req := ApprovalRequestText(a)
	for _, want := range []string{"<@U2>", "create pr", "gh pr create", "owner"} {
		if !strings.Contains(req, want) {
			t.Errorf("request %q has no %q", req, want)
		}
	}
	a.State = ApprovalGranted
	if got := ApprovalOutcomeText(a); !strings.Contains(got, "Approved") || !strings.Contains(got, "create pr") {
		t.Errorf("granted outcome %q", got)
	}
	if got := ApprovalSessionPrompt(a); !strings.Contains(got, "approved") || !strings.Contains(got, "gh pr create") || !strings.Contains(got, "again") {
		t.Errorf("granted prompt %q", got)
	}
	a.State = ApprovalDenied
	if got := ApprovalOutcomeText(a); !strings.Contains(got, "Denied") {
		t.Errorf("denied outcome %q", got)
	}
	if got := ApprovalSessionPrompt(a); !strings.Contains(got, "said no") || strings.Contains(got, "again") {
		t.Errorf("denied prompt %q", got)
	}
}

func TestApprovalPush(t *testing.T) {
	a := Approval{ID: "a1", SessionID: "s1", Mate: "ada", Action: Action{Kind: ActionDeleteBranch}}
	got := ApprovalPush(a)
	want := PushMessage{Title: "ada needs approval", Body: "delete branch", URL: "/#/approvals", Tag: "approval-a1"}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	if got := ApprovalPush(Approval{ID: "a2", Action: Action{Kind: ActionPush}}); got.Title != "crew needs approval" {
		t.Fatalf("no mate %+v", got)
	}
}

func TestOpenApprovals(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	list := []Approval{
		{ID: "a", State: ApprovalPending, At: now},
		{ID: "b", State: ApprovalGranted, At: now},
		{ID: "c", State: ApprovalPending, At: now.Add(-ApprovalTTL - time.Second)},
		{ID: "d", State: ApprovalPending, At: now.Add(-time.Hour)},
	}
	got := OpenApprovals(list, now)
	if len(got) != 2 || got[0].ID != "d" || got[1].ID != "a" {
		t.Fatalf("got %+v", got)
	}
	if got := OpenApprovals(nil, now); got == nil || len(got) != 0 {
		t.Fatalf("empty %#v", got)
	}
}
