package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

func TestPrintApprovalsListsEachPendingApproval(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	list := []domain.Approval{
		{ID: "a1", Mate: "helper", Requester: "U0GUEST01", At: at, State: domain.ApprovalPending,
			Action: domain.Action{Kind: domain.ActionCreatePR, Tool: "Bash", Input: `{"command":"gh pr create"}`}},
	}
	var out bytes.Buffer
	printApprovals(&out, list, at.Add(time.Hour))
	got := out.String()
	for _, want := range []string{"ID", "MATE", "ACTION", "BY", "EXPIRES", "a1", "helper", "create pr", "U0GUEST01", "23h", `{"command":"gh pr create"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

func TestPrintApprovalsWithNoneSaysSo(t *testing.T) {
	var out bytes.Buffer
	printApprovals(&out, nil, time.Now())
	if out.String() != "no pending approvals\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestRunApprovalRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"approve"}, {"deny"}, {"list", "x"}, {"approve", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := runApproval(args, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: crew approval") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut.String())
		}
	}
}
