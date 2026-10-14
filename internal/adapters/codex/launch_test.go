package codex

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/giovaniif/crew/internal/app"
)

type fakeHost struct {
	app.TerminalHost
	created []app.PaneSpec
	err     error
}

func (h *fakeHost) Create(_ context.Context, spec app.PaneSpec) (app.PaneID, error) {
	if h.err != nil {
		return "", h.err
	}
	h.created = append(h.created, spec)
	return "%12", nil
}

func TestLaunchStartsCodexWithModelAndEffortInTheWorkspace(t *testing.T) {
	host := &fakeHost{}
	pane, err := Launch(context.Background(), host, LaunchRequest{
		Name: "api-42", Dir: "/work/api", Model: "gpt-6.1-sol", Effort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pane != "%12" || len(host.created) != 1 {
		t.Fatalf("pane %q, created %d", pane, len(host.created))
	}
	spec := host.created[0]
	want := []string{"codex", "--model", "gpt-6.1-sol", "-c", `model_reasoning_effort="high"`}
	if !slices.Equal(spec.Command, want) || spec.Dir != "/work/api" || spec.Name != "api-42" {
		t.Fatalf("spec %+v", spec)
	}
}

func TestLaunchOmitsWhatWasNotChosenAndPassesThePromptLast(t *testing.T) {
	spec := LaunchSpec(LaunchRequest{Name: "n", Dir: "/w", Prompt: "--fix the build"})
	want := []string{"codex", "--", "--fix the build"}
	if !slices.Equal(spec.Command, want) {
		t.Fatalf("command %v", spec.Command)
	}
	bare := LaunchSpec(LaunchRequest{Name: "n", Dir: "/w"})
	if !slices.Equal(bare.Command, []string{"codex"}) {
		t.Fatalf("command %v", bare.Command)
	}
}

func TestLaunchReportsAHostFailure(t *testing.T) {
	boom := errors.New("no tmux")
	if _, err := Launch(context.Background(), &fakeHost{err: boom}, LaunchRequest{Name: "n", Dir: "/w"}); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

func TestLaunchResumesTheCodexThreadByID(t *testing.T) {
	got := Adapter{}.Launch(app.LaunchRequest{Dir: "/w", Model: "gpt-5", Effort: "high", Resume: "t-1"})
	want := []string{"codex", "resume", "--model", "gpt-5", "-c", `model_reasoning_effort="high"`, "t-1"}
	if !slices.Equal(got.Command, want) {
		t.Fatalf("command %v, want %v", got.Command, want)
	}
}
