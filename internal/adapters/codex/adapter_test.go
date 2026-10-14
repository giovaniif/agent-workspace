package codex

import (
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestAdapterLaunchesCodexAsTheCodexHarness(t *testing.T) {
	var a app.HarnessAdapter = Adapter{}
	if a.Harness() != domain.HarnessCodex {
		t.Fatalf("harness %q", a.Harness())
	}
	got := a.Launch(app.LaunchRequest{Name: "api", Dir: "/w/api", Model: "gpt-6"})
	want := app.PaneSpec{Name: "api", Dir: "/w/api", Command: []string{"codex", "--model", "gpt-6"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAdapterLaunchesItsBinaryWhenSet(t *testing.T) {
	got := Adapter{Binary: "/bin/fake-codex"}.Launch(app.LaunchRequest{Dir: "/w", Prompt: "go"})
	if want := []string{"/bin/fake-codex", "--", "go"}; !reflect.DeepEqual(got.Command, want) {
		t.Fatalf("command %v, want %v", got.Command, want)
	}
}
