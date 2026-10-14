//go:build integration

package tmux_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
)

var _ app.TerminalHost = (*tmux.Host)(nil)

func newHost(t testing.TB) *tmux.Host {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	h := tmux.New(tmux.Config{
		Socket:     fmt.Sprintf("crew-test-%d-%d", os.Getpid(), time.Now().UnixNano()),
		ConfigPath: filepath.Join(dir, "tmux.conf"),
	})
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h
}

func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func catPane(name string) app.PaneSpec {
	return app.PaneSpec{Name: name, Command: []string{"cat"}}
}

func defaultServerSessions(t testing.TB) string {
	t.Helper()
	out, _ := exec.Command("tmux", "ls").CombinedOutput()
	return string(out)
}

func TestThreePanesShownInTurnReceiveAndReturnText(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	var panes []app.PaneID
	for i := range 3 {
		p, err := h.Create(ctx, catPane(fmt.Sprintf("agent-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		panes = append(panes, p)
	}
	for i, p := range panes {
		if err := h.Show(ctx, p, slot); err != nil {
			t.Fatalf("show %d: %v", i, err)
		}
		marker := fmt.Sprintf("hello-from-%d", i)
		if err := h.SendText(ctx, p, marker, false); err != nil {
			t.Fatal(err)
		}
		if err := h.SendKeys(ctx, p, "Enter"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "echo of "+marker, func() bool {
			out, err := h.Capture(ctx, p, 50)
			return err == nil && strings.Count(out, marker) >= 2
		})
	}
	for i, p := range panes {
		out, err := h.Capture(ctx, p, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, fmt.Sprintf("hello-from-%d", i)) {
			t.Errorf("pane %d lost its text: %q", i, out)
		}
	}
}

func TestShowPutsPaneInSlotAndParksThePrevious(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := h.Create(ctx, catPane("a"))
	b, _ := h.Create(ctx, catPane("b"))
	for _, p := range []app.PaneID{a, b, b, a} {
		if err := h.Show(ctx, p, slot); err != nil {
			t.Fatal(err)
		}
		if got := h.ShownIn(ctx, slot); got != p {
			t.Fatalf("slot shows %q, want %q", got, p)
		}
	}
}

func TestShowRebuildsSlotAfterShownPaneDies(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := h.Create(ctx, catPane("a"))
	b, _ := h.Create(ctx, catPane("b"))
	if err := h.Show(ctx, a, slot); err != nil {
		t.Fatal(err)
	}
	if err := h.Kill(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := h.Show(ctx, b, slot); err != nil {
		t.Fatal(err)
	}
	if got := h.ShownIn(ctx, slot); got != b {
		t.Fatalf("slot shows %q, want %q", got, b)
	}
}

func TestSendTextBracketedPasteDelivers5KBIntact(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	out := filepath.Join(t.TempDir(), "received")
	script := fmt.Sprintf(`printf '\033[?2004h'; stty raw -echo; exec cat > %s`, out)
	p, err := h.Create(ctx, app.PaneSpec{Name: "sink", Command: []string{"sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; b.Len() < 5*1024; i++ {
		fmt.Fprintf(&b, "line %04d: the quick brown fox \"jumps\" over $HOME `date` 'again'\n", i)
	}
	text := b.String()
	waitFor(t, "sink to be ready", func() bool {
		_, err := os.Stat(out)
		return err == nil
	})
	if err := h.SendText(ctx, p, text, true); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[200~" + text + "\x1b[201~"
	waitFor(t, "5KB to arrive", func() bool {
		got, _ := os.ReadFile(out)
		return len(got) >= len(want)
	})
	got, _ := os.ReadFile(out)
	if string(got) != want {
		t.Fatalf("received %d bytes, want %d; first difference at %d", len(got), len(want), firstDiff(string(got), want))
	}
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestListReportsOnlyCreatedPanesAndAliveTracksExit(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	if _, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}}); err != nil {
		t.Fatal(err)
	}
	long, _ := h.Create(ctx, catPane("long"))
	short, _ := h.Create(ctx, app.PaneSpec{Name: "short", Command: []string{"sleep", "0.2"}})
	waitFor(t, "short pane to exit", func() bool {
		alive, err := h.Alive(ctx, short)
		return err == nil && !alive
	})
	if alive, _ := h.Alive(ctx, long); !alive {
		t.Fatal("long pane should be alive")
	}
	panes, err := h.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range panes {
		if p.Alive {
			ids = append(ids, string(p.ID))
		}
	}
	sort.Strings(ids)
	if !slices.Equal(ids, []string{string(long)}) {
		t.Fatalf("alive managed panes %v, want only %v (got %v)", ids, long, panes)
	}
}

func TestCreateHonorsDirAndEnv(t *testing.T) {
	ctx := context.Background()
	h := newHost(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Create(ctx, app.PaneSpec{
		Name:    "env",
		Dir:     dir,
		Env:     map[string]string{"CREW_PROBE": "42"},
		Command: []string{"sh", "-c", `echo "$PWD $CREW_PROBE"; sleep 600`},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pane output", func() bool {
		out, _ := h.Capture(ctx, p, 10)
		return strings.Contains(out, dir+" 42")
	})
}

func TestServerIsIsolatedFromTheUserConfigAndDefaultServer(t *testing.T) {
	ctx := context.Background()
	before := defaultServerSessions(t)
	h := newHost(t)
	if _, err := h.Create(ctx, catPane("a")); err != nil {
		t.Fatal(err)
	}
	for option, want := range map[string]string{"status": "off", "prefix": "None"} {
		got, err := h.ShowOption(ctx, option)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", option, got, want)
		}
	}
	if after := defaultServerSessions(t); after != before {
		t.Fatalf("default tmux server changed:\nbefore: %s\nafter: %s", before, after)
	}
}

func BenchmarkShowSwitch(b *testing.B) {
	ctx := context.Background()
	h := newHost(b)
	slot, err := h.OpenClient(ctx, "main", app.PaneSpec{Name: "tui", Command: []string{"sleep", "600"}})
	if err != nil {
		b.Fatal(err)
	}
	var panes []app.PaneID
	for i := range 3 {
		p, err := h.Create(ctx, catPane(fmt.Sprintf("agent-%d", i)))
		if err != nil {
			b.Fatal(err)
		}
		panes = append(panes, p)
	}
	if err := h.Show(ctx, panes[0], slot); err != nil {
		b.Fatal(err)
	}
	var samples []time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := h.Show(ctx, panes[(i+1)%3], slot); err != nil {
			b.Fatal(err)
		}
		samples = append(samples, time.Since(start))
	}
	b.StopTimer()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[len(samples)*95/100]
	b.ReportMetric(float64(p95.Microseconds())/1000, "p95-ms")
	if len(samples) >= 100 && p95 > 60*time.Millisecond {
		b.Fatalf("switch p95 %v exceeds the 60ms budget", p95)
	}
}
