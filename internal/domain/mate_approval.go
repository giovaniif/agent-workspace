package domain

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	ApprovalApproveAction = "crew_approve"
	ApprovalDenyAction    = "crew_deny"
	approvalInputMax      = 300
)

var (
	ErrApprovalNotOwner = errors.New("only the owner of the mate can decide")
	ErrApprovalExpired  = errors.New("the approval expired")
	ErrApprovalDecided  = errors.New("the approval is already decided")
)

func (a Approval) Expired(now time.Time) bool {
	return now.After(a.At.Add(ApprovalTTL))
}

func DecideApproval(a Approval, by, owner string, allow bool, now time.Time) (Approval, error) {
	switch {
	case owner == "" || by != owner:
		return a, ErrApprovalNotOwner
	case a.State != ApprovalPending:
		return a, ErrApprovalDecided
	case a.Expired(now):
		return a, ErrApprovalExpired
	}
	a.State = ApprovalDenied
	if allow {
		a.State = ApprovalGranted
	}
	return a, nil
}

func OpenApprovals(list []Approval, now time.Time) []Approval {
	out := []Approval{}
	for _, a := range list {
		if a.State == ApprovalPending && !a.Expired(now) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func actionWords(k ActionKind) string {
	return strings.ReplaceAll(string(k), "_", " ")
}

func approvalInput(a Approval) string {
	in := []rune(a.Action.Input)
	if len(in) > approvalInputMax {
		in = append(in[:approvalInputMax-1], '…')
	}
	return string(in)
}

func approvalLine(a Approval) string {
	return actionWords(a.Action.Kind) + " (" + a.Action.Tool + ")\n```" + approvalInput(a) + "```"
}

func ApprovalRequestText(a Approval) string {
	who := "Someone"
	if a.Requester != "" {
		who = "<@" + a.Requester + ">"
	}
	return who + " asked for an action that needs the owner: " + approvalLine(a)
}

func ApprovalOutcomeText(a Approval) string {
	word := "Denied"
	if a.State == ApprovalGranted || a.State == ApprovalUsed {
		word = "Approved"
	}
	return word + " by the owner: " + approvalLine(a)
}

func ApprovalSessionPrompt(a Approval) string {
	what := actionWords(a.Action.Kind) + " with " + a.Action.Tool + " and this input: " + a.Action.Input
	if a.State == ApprovalGranted {
		return "The owner approved this action one time: " + what + ". Run it again with the same input."
	}
	return "The owner said no to this action: " + what + ". Do not run it. Tell the thread what you did instead."
}

func ApprovalPush(a Approval) PushMessage {
	title := a.Mate
	if title == "" {
		title = PushTitleFallback
	}
	return PushMessage{Title: title + " needs approval", Body: actionWords(a.Action.Kind), URL: "/#/approvals", Tag: "approval-" + a.ID}
}
