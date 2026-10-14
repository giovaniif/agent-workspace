package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

var claudeAt3 = domain.Session{Harness: domain.HarnessClaude, Pane: "%3"}

func codexHost() *codexPickerHost {
	return &codexPickerHost{
		models:  []string{"GPT-6.1-Sol", "GPT-6-Astra", "GPT-6-Luna"},
		efforts: []string{"Low", "Medium", "High", "Extra high"},
		current: "GPT-6.1-Sol",
	}
}

func TestModelSwitchCodexModelIsPickedFromTheModelPopupKeepingTheEffort(t *testing.T) {
	host := codexHost()
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3", Model: "gpt-6.1-sol", Effort: "high"}
	sws := []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-6-luna"}}
	if err := app.SendSwitches(context.Background(), host, s, sws, 0); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"GPT-6-Luna", "High"}}; !reflect.DeepEqual(host.done, want) {
		t.Fatalf("chose %q, typed %q", host.done, host.typed)
	}
	if host.typed[0] != "paste /model" {
		t.Fatalf("typed %q", host.typed)
	}
}

func TestModelSwitchCodexEffortRepicksTheModelSwitchedToJustBefore(t *testing.T) {
	host := codexHost()
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3", Model: "gpt-6.1-sol", Effort: "medium"}
	sws := []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-6-astra"}, {Kind: domain.SwitchEffort, Value: "xhigh"}}
	if err := app.SendSwitches(context.Background(), host, s, sws, 0); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"GPT-6-Astra", "Medium"}, {"GPT-6-Astra", "Extra high"}}
	if !reflect.DeepEqual(host.done, want) {
		t.Fatalf("chose %q, typed %q", host.done, host.typed)
	}
}

func TestModelSwitchCodexClosesThePickerAndFailsWhenTheValueIsNotListed(t *testing.T) {
	host := codexHost()
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3", Model: "gpt-6.1-sol"}
	err := app.SendSwitches(context.Background(), host, s, []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-9"}}, 0)
	if err == nil || host.popup != nil || len(host.done) != 0 {
		t.Fatalf("err %v, popup %q, chose %q", err, host.popup, host.done)
	}
	if last := host.typed[len(host.typed)-1]; last != "keys Escape" {
		t.Fatalf("typed %q", host.typed)
	}
}

func TestModelSwitchCodexFailsWhenNoPickerOpens(t *testing.T) {
	host := codexHost()
	host.models = nil
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3"}
	if err := app.SendSwitches(context.Background(), host, s, []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-6-luna"}}, 0); err == nil {
		t.Fatalf("no error, typed %q", host.typed)
	}
}

func TestModelSwitchCodexWaitsForTheRedrawBeforeTheNextKeys(t *testing.T) {
	host := codexHost()
	host.lag = 2
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3", Model: "gpt-6.1-sol", Effort: "medium"}
	sws := []domain.Switch{{Kind: domain.SwitchEffort, Value: "xhigh"}}
	if err := app.SendSwitches(context.Background(), host, s, sws, 0); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"GPT-6.1-Sol", "Extra high"}}; !reflect.DeepEqual(host.done, want) {
		t.Fatalf("chose %q, typed %q", host.done, host.typed)
	}
}

func TestModelSwitchCodexClosesThePickerWhenThePaneCannotBeRead(t *testing.T) {
	host := codexHost()
	host.captureErr = errors.New("capture failed")
	s := domain.Session{Harness: domain.HarnessCodex, Pane: "%3", Model: "gpt-6.1-sol"}
	err := app.SendSwitches(context.Background(), host, s, []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-6-luna"}}, 0)
	if err == nil || host.popup != nil {
		t.Fatalf("err %v, popup %q, typed %q", err, host.popup, host.typed)
	}
}

func TestModelSwitchTypesEachCommandAsAPasteThenEnter(t *testing.T) {
	host := &typingHost{}
	sws := []domain.Switch{{Kind: domain.SwitchModel, Value: "opus"}, {Kind: domain.SwitchEffort, Value: "high"}}
	if err := app.SendSwitches(context.Background(), host, claudeAt3, sws, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"%3 paste=true /model opus", "%3 keys Enter",
		"%3 paste=true /effort high", "%3 keys Enter",
	}
	if !reflect.DeepEqual(host.typed, want) {
		t.Fatalf("typed %q", host.typed)
	}
}

func TestModelSwitchStopsAtTheFirstCommandTheHostRefuses(t *testing.T) {
	host := &typingHost{failOn: "/model opus", failErr: errors.New("pane gone")}
	sws := []domain.Switch{{Kind: domain.SwitchModel, Value: "opus"}, {Kind: domain.SwitchEffort, Value: "high"}}
	err := app.SendSwitches(context.Background(), host, claudeAt3, sws, 0)
	if err == nil || len(host.typed) != 0 {
		t.Fatalf("err %v, typed %q", err, host.typed)
	}
}
