package claude_test

import (
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestLaunchRunsClaudeWithTheModelAndEffortInTheSessionDir(t *testing.T) {
	var a app.HarnessAdapter = claude.Adapter{}
	if a.Harness() != domain.HarnessClaude {
		t.Fatalf("harness %q", a.Harness())
	}
	got := a.Launch(app.LaunchRequest{Name: "api-42", Dir: "/home/dev/api", Model: "sonnet", Effort: "high"})
	want := app.PaneSpec{Name: "api-42", Dir: "/home/dev/api", Command: []string{"claude", "--model", "sonnet", "--effort", "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLaunchLeavesDefaultsToClaude(t *testing.T) {
	got := claude.Adapter{Binary: "/opt/claude"}.Launch(app.LaunchRequest{Dir: "/w"})
	if !reflect.DeepEqual(got.Command, []string{"/opt/claude"}) {
		t.Fatalf("command %v", got.Command)
	}
}

func TestLaunchResumesTheClaudeSessionByID(t *testing.T) {
	got := claude.Adapter{}.Launch(app.LaunchRequest{Dir: "/w", Model: "opus", Effort: "high", Resume: "4c98"})
	want := []string{"claude", "--resume", "4c98", "--model", "opus", "--effort", "high"}
	if !reflect.DeepEqual(got.Command, want) {
		t.Fatalf("command %v, want %v", got.Command, want)
	}
}
