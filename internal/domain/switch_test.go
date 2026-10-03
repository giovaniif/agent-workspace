package domain

import (
	"reflect"
	"testing"
	"time"
)

var switchT0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestModelSwitchIsSentAtOnceWhenTheAgentIsBetweenTools(t *testing.T) {
	for _, state := range []AgentState{StateIdle, StateDone, StateWaiting} {
		s := Session{Harness: HarnessClaude, State: state}.RequestSwitch(SwitchModel, "opus")
		s, sent := s.Dispatch(switchT0)
		want := []Switch{{Kind: SwitchModel, Value: "opus", SentAt: switchT0}}
		if !reflect.DeepEqual(sent, want) {
			t.Fatalf("%s: sent %+v", state, sent)
		}
		if !reflect.DeepEqual(s.Switches, want) {
			t.Fatalf("%s: session keeps %+v", state, s.Switches)
		}
	}
}

func TestModelSwitchWaitsWhileTheAgentRunsATool(t *testing.T) {
	for _, state := range []AgentState{StateRunning, StatePermission} {
		s := Session{Harness: HarnessClaude, State: state}.RequestSwitch(SwitchModel, "opus")
		s, sent := s.Dispatch(switchT0)
		if len(sent) != 0 || len(s.Switches) != 1 || !s.Switches[0].SentAt.IsZero() {
			t.Fatalf("%s: sent %+v, session %+v", state, sent, s.Switches)
		}
	}
}

func TestModelSwitchQueuedWhileRunningIsSentAtTheNextDone(t *testing.T) {
	s := Session{Harness: HarnessClaude, State: StateRunning}.RequestSwitch(SwitchEffort, "high")
	s, _ = s.Dispatch(switchT0)
	s, _ = s.Apply(HarnessEvent{Kind: EventStop})
	_, sent := s.Dispatch(switchT0.Add(time.Minute))
	if len(sent) != 1 || sent[0].Kind != SwitchEffort || sent[0].SentAt != switchT0.Add(time.Minute) {
		t.Fatalf("sent %+v", sent)
	}
}

func TestModelSwitchQueuedWhileRunningIsSentWhenTheAgentWaits(t *testing.T) {
	s := Session{Harness: HarnessClaude, State: StateRunning}.RequestSwitch(SwitchModel, "opus")
	s, _ = s.Apply(HarnessEvent{Kind: EventWaitingForInput})
	if _, sent := s.Dispatch(switchT0); len(sent) != 1 {
		t.Fatalf("sent %+v", sent)
	}
}

func TestModelSwitchIsNeverSentTwice(t *testing.T) {
	s := Session{Harness: HarnessClaude, State: StateIdle}.RequestSwitch(SwitchModel, "opus")
	s, _ = s.Dispatch(switchT0)
	if _, sent := s.Dispatch(switchT0.Add(time.Second)); len(sent) != 0 {
		t.Fatalf("resent %+v", sent)
	}
}

func TestModelSwitchNewerRequestReplacesAnUnsentOneOfTheSameKind(t *testing.T) {
	s := Session{Harness: HarnessClaude, State: StateRunning}.
		RequestSwitch(SwitchModel, "opus").
		RequestSwitch(SwitchEffort, "low").
		RequestSwitch(SwitchModel, "sonnet")
	want := []Switch{{Kind: SwitchEffort, Value: "low"}, {Kind: SwitchModel, Value: "sonnet"}}
	if !reflect.DeepEqual(s.Switches, want) {
		t.Fatalf("switches %+v", s.Switches)
	}
}

func TestModelSwitchNewRequestRetiresASentUnconfirmedOneOfTheSameKind(t *testing.T) {
	s := sentSession(SwitchModel, "opus").RequestSwitch(SwitchModel, "haiku")
	want := []Switch{{Kind: SwitchModel, Value: "haiku"}}
	if !reflect.DeepEqual(s.Switches, want) {
		t.Fatalf("switches %+v", s.Switches)
	}
}

func TestModelSwitchModelNamesMatchExactlyByFirstWordOrWithoutVersion(t *testing.T) {
	cases := []struct {
		requested, reported string
		confirmed           bool
	}{
		{"opus", "Opus 4.7", true},
		{"opus", "opus-5.5", true},
		{"sonnet", "opus-5.5", false},
		{"gpt-6", "gpt-6.1-sol", false},
		{"gpt-5", "GPT-5", true},
		{"gpt-5", "gpt-5-codex", false},
		{"gpt-5-codex", "gpt-5", false},
		{"sonnet", "Opus 4.7", false},
	}
	for _, c := range cases {
		s := sentSession(SwitchModel, c.requested).Report(StatusReport{Model: c.reported})
		if got := len(s.Switches) == 0; got != c.confirmed {
			t.Errorf("%q vs %q: confirmed %v, want %v", c.requested, c.reported, got, c.confirmed)
		}
	}
}

func TestModelSwitchDoesNotMutateTheCallersSession(t *testing.T) {
	before := Session{Harness: HarnessClaude, State: StateIdle}.RequestSwitch(SwitchModel, "opus")
	_, _ = before.Dispatch(switchT0)
	if !before.Switches[0].SentAt.IsZero() {
		t.Fatal("Dispatch changed the original session")
	}
}

func sentSession(kind SwitchKind, value string) Session {
	s, _ := Session{Harness: HarnessClaude, State: StateIdle, Model: "Sonnet 4.6", Effort: "medium"}.RequestSwitch(kind, value).Dispatch(switchT0)
	return s
}

func TestModelSwitchIsConfirmedByTheNextReportShowingIt(t *testing.T) {
	cases := []struct {
		kind   SwitchKind
		value  string
		report StatusReport
	}{
		{SwitchModel, "opus", StatusReport{Model: "Opus 4.7"}},
		{SwitchEffort, "high", StatusReport{Effort: "high"}},
		{SwitchModel, "gpt-5", StatusReport{Model: "GPT-5"}},
	}
	for _, c := range cases {
		s := sentSession(c.kind, c.value).Report(c.report)
		if len(s.Switches) != 0 || s.SwitchWarning {
			t.Fatalf("%v: switches %+v warning %v", c.report, s.Switches, s.SwitchWarning)
		}
	}
}

func TestModelSwitchWarnsWhenTheNextReportStillShowsTheOldValue(t *testing.T) {
	s := sentSession(SwitchModel, "opus").Report(StatusReport{Model: "Sonnet 4.6", Effort: "medium"})
	if !s.SwitchWarning {
		t.Fatal("no warning")
	}
	s = s.Report(StatusReport{Model: "Opus 4.7"})
	if s.SwitchWarning || len(s.Switches) != 0 {
		t.Fatalf("late confirmation left %+v warning %v", s.Switches, s.SwitchWarning)
	}
}

func TestModelSwitchIgnoresReportsThatSayNothingAboutItsKind(t *testing.T) {
	s := sentSession(SwitchModel, "opus").Report(StatusReport{Effort: "high", ContextLeft: 50, HasContext: true})
	if s.SwitchWarning || len(s.Switches) != 1 {
		t.Fatalf("switches %+v warning %v", s.Switches, s.SwitchWarning)
	}
}

func TestModelSwitchNotYetSentIsNotJudgedByReports(t *testing.T) {
	s := Session{Harness: HarnessClaude, State: StateRunning, Model: "Sonnet 4.6"}.RequestSwitch(SwitchModel, "opus")
	s = s.Report(StatusReport{Model: "Sonnet 4.6"})
	if s.SwitchWarning || len(s.Switches) != 1 {
		t.Fatalf("switches %+v warning %v", s.Switches, s.SwitchWarning)
	}
}

func TestModelSwitchNewRequestClearsAnOldWarning(t *testing.T) {
	s := sentSession(SwitchModel, "opus").Report(StatusReport{Model: "Sonnet 4.6"})
	s = s.RequestSwitch(SwitchModel, "haiku")
	if s.SwitchWarning {
		t.Fatal("warning kept")
	}
}

func TestModelSwitchFailedToSendDropsItAndWarns(t *testing.T) {
	s := sentSession(SwitchModel, "opus")
	s = s.SwitchFailed(s.Switches)
	if len(s.Switches) != 0 || !s.SwitchWarning {
		t.Fatalf("switches %+v warning %v", s.Switches, s.SwitchWarning)
	}
}

func TestModelSwitchCommandIsTheHarnessesOwnSlashCommand(t *testing.T) {
	cases := []struct {
		s    Session
		sw   Switch
		want string
		ok   bool
	}{
		{Session{Harness: HarnessClaude}, Switch{Kind: SwitchModel, Value: "opus"}, "/model opus", true},
		{Session{Harness: HarnessClaude}, Switch{Kind: SwitchEffort, Value: "high"}, "/effort high", true},
		{Session{Harness: HarnessCodex}, Switch{Kind: SwitchModel, Value: "gpt-6-luna"}, "/model", true},
		{Session{Harness: HarnessCodex}, Switch{Kind: SwitchEffort, Value: "high"}, "/model", true},
		{Session{Harness: HarnessOmp, Model: "sonnet"}, Switch{Kind: SwitchModel, Value: "anthropic/opus"}, "/switch anthropic/opus", true},
		{Session{Harness: HarnessOmp, Model: "sonnet"}, Switch{Kind: SwitchEffort, Value: "xhigh"}, "/switch sonnet:xhigh", true},
		{Session{Harness: HarnessOmp}, Switch{Kind: SwitchEffort, Value: "high"}, "", false},
		{Session{Harness: HarnessOmp, Model: "sonnet"}.RequestSwitch(SwitchModel, "opus"), Switch{Kind: SwitchEffort, Value: "low"}, "/switch opus:low", true},
		{Session{Harness: "other"}, Switch{Kind: SwitchModel, Value: "opus"}, "", false},
	}
	for _, c := range cases {
		got, ok := c.s.SwitchCommand(c.sw)
		if got != c.want || ok != c.ok {
			t.Errorf("%s %+v: %q, %v; want %q, %v", c.s.Harness, c.sw, got, ok, c.want, c.ok)
		}
	}
}

func TestModelSwitchOmpEffortWithNoModelStaysQueued(t *testing.T) {
	s := Session{Harness: HarnessOmp, State: StateIdle}.RequestSwitch(SwitchEffort, "high")
	s, sent := s.Dispatch(switchT0)
	if len(sent) != 0 || !reflect.DeepEqual(s.Switches, []Switch{{Kind: SwitchEffort, Value: "high"}}) {
		t.Fatalf("sent %+v, session keeps %+v", sent, s.Switches)
	}
	s.Model = "sonnet"
	if _, sent := s.Dispatch(switchT0); !reflect.DeepEqual(sent, []Switch{{Kind: SwitchEffort, Value: "high", SentAt: switchT0}}) {
		t.Fatalf("once a model is known, sent %+v", sent)
	}
}

func TestModelSwitchEveryCatalogHarnessIsSupported(t *testing.T) {
	efforts := []string{"low", "medium", "high", "xhigh", "max"}
	for _, h := range []Harness{HarnessClaude, HarnessCodex} {
		if !reflect.DeepEqual(SwitchChoices(h, SwitchEffort), efforts) {
			t.Errorf("%s efforts %q", h, SwitchChoices(h, SwitchEffort))
		}
	}
	if got, want := SwitchChoices(HarnessOmp, SwitchEffort), []string{"off", "minimal", "low", "medium", "high", "xhigh"}; !reflect.DeepEqual(got, want) {
		t.Errorf("omp efforts %q", got)
	}
	for _, h := range []Harness{HarnessClaude, HarnessCodex, HarnessOmp} {
		if !SwitchSupported(h) {
			t.Errorf("%s not supported", h)
		}
	}
	if SwitchSupported("other") || SwitchChoices("other", SwitchModel) != nil || SwitchChoices("other", SwitchEffort) != nil {
		t.Fatal("unknown harness supported")
	}
	if got := SwitchChoices(HarnessCodex, SwitchModel); got[0] != "gpt-6.1-sol" {
		t.Fatalf("codex models %q", got)
	}
	if got := SwitchChoices(HarnessClaude, SwitchModel); !reflect.DeepEqual(got, []string{"opus", "sonnet", "haiku"}) {
		t.Fatalf("claude models %q", got)
	}
	if got := SwitchChoices(HarnessOmp, SwitchModel); got != nil {
		t.Fatalf("omp models are typed, got a list %q", got)
	}
}

func TestModelSwitchRequeuedSwitchIsSentAgainAtTheNextDispatch(t *testing.T) {
	s := sentSession(SwitchModel, "opus")
	s = s.Requeue(s.Switches)
	if len(s.Switches) != 1 || !s.Switches[0].SentAt.IsZero() {
		t.Fatalf("switches %+v", s.Switches)
	}
	if _, sent := s.Dispatch(switchT0.Add(time.Minute)); len(sent) != 1 {
		t.Fatalf("sent %+v", sent)
	}
}

func TestModelSwitchAcceptedOnlyBetweenTools(t *testing.T) {
	want := map[AgentState]bool{StateIdle: true, StateDone: true, StateWaiting: true, StateRunning: false, StatePermission: false}
	for state, ok := range want {
		if got := (Session{State: state}).AcceptsSwitch(); got != ok {
			t.Errorf("%s: %v", state, got)
		}
	}
}

func TestModelSwitchAReportFromBeforeTheSwitchDoesNotWarn(t *testing.T) {
	s := sentSession(SwitchEffort, "high")
	stale := s.Report(StatusReport{Effort: "low", At: switchT0.Add(-time.Minute)})
	if stale.SwitchWarning || len(stale.Switches) != 1 {
		t.Fatalf("stale report: %+v", stale)
	}
	fresh := s.Report(StatusReport{Effort: "low", At: switchT0.Add(time.Minute)})
	if !fresh.SwitchWarning {
		t.Fatalf("fresh report: %+v", fresh)
	}
	match := s.Report(StatusReport{Effort: "high", At: switchT0.Add(-time.Minute)})
	if match.SwitchWarning || len(match.Switches) != 0 {
		t.Fatalf("matching report: %+v", match)
	}
}
