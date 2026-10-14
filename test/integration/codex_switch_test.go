//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestModelSwitchCodexPickerIsWalkedInARealPane(t *testing.T) {
	for _, bin := range []string{"tmux", "python3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	script, err := filepath.Abs(filepath.Join("testdata", "fake-codex-picker.py"))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	out := filepath.Join(home, "chosen")
	socket := fmt.Sprintf("agentws-it-%d-%d", os.Getpid(), time.Now().UnixNano())
	host := tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")})
	t.Cleanup(func() { _ = host.Close(context.Background()) })
	ctx := context.Background()
	pane, err := host.Create(ctx, app.PaneSpec{Name: "codex", Dir: home, Command: []string{"python3", script}, Env: map[string]string{"FAKE_CODEX_OUT": out}})
	if err != nil {
		t.Fatal(err)
	}
	if !eventually(t, 5*time.Second, func() bool {
		screen, _ := host.Capture(ctx, pane, 40)
		return strings.Contains(screen, "earlier output")
	}) {
		t.Fatal("fake codex did not start")
	}

	s := domain.Session{Harness: domain.HarnessCodex, Pane: string(pane), Model: "gpt-6.1-sol", Effort: "high"}
	sws := []domain.Switch{{Kind: domain.SwitchModel, Value: "gpt-6-luna"}, {Kind: domain.SwitchEffort, Value: "xhigh"}}
	if err := app.SendSwitches(ctx, host, s, sws, app.PasteSettle); err != nil {
		screen, _ := host.Capture(ctx, pane, 40)
		t.Fatalf("%v\n%s", err, screen)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := "GPT-6-Luna High\nGPT-6-Luna Extra high\n"; string(got) != want {
		t.Fatalf("chose %q", got)
	}
}
