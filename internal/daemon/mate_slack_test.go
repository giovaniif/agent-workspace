package daemon_test

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/secrets"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const slackMates = globalMates + `
[[mate]]
name = "helper"
prompt = "Help."
agent = "claude"
`

func slackDaemon(t *testing.T, hub *fakeSlackHub) (*rpc.Client, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(slackMates), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sock := start(t, &memStore{},
		daemon.WithWorkspaces(shop, shopGit),
		daemon.WithConfig(path),
		daemon.WithMateSecrets(secrets.Files{Dir: filepath.Join(home, "secrets")}),
		daemon.WithSlack(hub.connect, time.Hour),
	)
	c := dial(t, sock)
	if _, err := c.WorkspaceAdd(context.Background(), "/shop"); err != nil {
		t.Fatal(err)
	}
	return c, path
}

func mateStates(t *testing.T, c *rpc.Client) map[string]rpc.MateState {
	t.Helper()
	out := map[string]rpc.MateState{}
	for _, s := range listMates(t, c).States {
		out[s.Name] = s
	}
	return out
}

func waitState(t *testing.T, c *rpc.Client, name string, want domain.MateConnection) rpc.MateState {
	t.Helper()
	var got rpc.MateState
	waitFor(t, func() bool {
		got = mateStates(t, c)[name]
		return got.State == want
	})
	return got
}

func TestMateWorkerConnectsEachEnabledMateThatHasTokens(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	for _, name := range []string{"reviewer", "builder"} {
		if _, err := setTokens(c, name, knownBot, knownApp); err != nil {
			t.Fatal(err)
		}
	}
	waitState(t, c, "reviewer", domain.MateConnected)
	states := mateStates(t, c)
	if states["builder"].State != domain.MatePaused || states["helper"].State != domain.MateNoTokens {
		t.Errorf("states %+v", states)
	}
	mates := hub.snapshot()
	if len(mates) != 1 || mates[0].bot != knownBot || mates[0].appTok != knownApp {
		t.Errorf("connections %+v", mates)
	}
}

func TestMateClearTokensDisconnectsTheMate(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	hub.live(t)
	if err := c.Call(context.Background(), rpc.MethodMateClearTokens, rpc.MateClearTokensParams{Name: "reviewer"}, nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, c, "reviewer", domain.MateNoTokens)
	waitFor(t, func() bool { m := hub.snapshot(); return len(m) == 1 && m[0].stopped })
}

func TestMateNewTokensReconnectTheMate(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	hub.live(t)
	if _, err := setTokens(c, "reviewer", knownBot+"2", knownApp); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		m := hub.snapshot()
		return len(m) == 2 && m[0].stopped && !m[1].stopped && m[1].bot == knownBot+"2"
	})
}

func TestMateTokenErrorShowsOnTheMateStateAndNotInTheLog(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	hub := &fakeSlackHub{fail: "slack rejected the app token: invalid_auth"}
	c, _ := slackDaemon(t, hub)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	got := waitState(t, c, "reviewer", domain.MateError)
	if !strings.Contains(got.Error, "invalid_auth") {
		t.Errorf("state %+v", got)
	}
	if strings.Contains(logs.String(), "zz") {
		t.Errorf("the log holds a token:\n%s", logs.String())
	}
}

func claim(t *testing.T, c *rpc.Client, name string) rpc.MateClaim {
	t.Helper()
	var got rpc.MateClaim
	if err := c.Call(context.Background(), rpc.MethodMateClaim, rpc.MateClaimParams{Name: name}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func ownerOf(t *testing.T, c *rpc.Client, name string) string {
	t.Helper()
	var m rpc.Mate
	if err := c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{Name: name}, &m); err != nil {
		t.Fatal(err)
	}
	return m.Owner
}

func TestMateClaimFromADirectMessageWritesTheOwnerOneTime(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	if _, err := setTokens(c, "helper", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	m := hub.live(t)
	got := claim(t, c, "helper")
	if got.Name != "helper" || len(got.Code) != domain.PairCodeLength || time.Until(got.ExpiresAt) < 9*time.Minute {
		t.Fatalf("claim %+v", got)
	}
	hub.send(m, app.SlackEvent{Kind: app.SlackDirectMessage, Channel: "D0DIRECT1", User: "U0OWNER01", Text: strings.ToLower(got.Code), TS: "1700.0001", ThreadTS: "1700.0001"})
	waitFor(t, func() bool { return ownerOf(t, c, "helper") == "U0OWNER01" })
	posts := hub.snapshot()[0].posts
	if len(posts) != 1 || posts[0].channel != "D0DIRECT1" || !strings.Contains(posts[0].text, "owner") {
		t.Errorf("posts %+v", posts)
	}
	hub.send(m, app.SlackEvent{Kind: app.SlackDirectMessage, Channel: "D0DIRECT2", User: "U0OTHER01", Text: got.Code, TS: "1700.0002", ThreadTS: "1700.0002"})
	time.Sleep(50 * time.Millisecond)
	if owner := ownerOf(t, c, "helper"); owner != "U0OWNER01" {
		t.Errorf("a used code changed the owner to %q", owner)
	}
}

func TestMateClaimIgnoresAMentionAndAWrongCode(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	if _, err := setTokens(c, "helper", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	m := hub.live(t)
	got := claim(t, c, "helper")
	hub.send(m, app.SlackEvent{Kind: app.SlackMention, Channel: "C0CHAN001", User: "U0OTHER01", Text: got.Code, TS: "1700.0001", ThreadTS: "1700.0001"})
	hub.send(m, app.SlackEvent{Kind: app.SlackDirectMessage, Channel: "D0DIRECT1", User: "U0OTHER01", Text: "WRONG234", TS: "1700.0002", ThreadTS: "1700.0002"})
	time.Sleep(50 * time.Millisecond)
	if owner := ownerOf(t, c, "helper"); owner != "" {
		t.Errorf("owner %q", owner)
	}
	if posts := hub.snapshot()[0].posts; len(posts) != 0 {
		t.Errorf("posts %+v", posts)
	}
}

func TestMateClaimNeedsAnExistingMate(t *testing.T) {
	hub := &fakeSlackHub{}
	c, _ := slackDaemon(t, hub)
	err := c.Call(context.Background(), rpc.MethodMateClaim, rpc.MateClaimParams{Name: "ghost"}, nil)
	if callCode(err) != rpc.CodeNotFound {
		t.Errorf("err = %v", err)
	}
}
