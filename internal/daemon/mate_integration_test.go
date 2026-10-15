//go:build integration

package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/adapters/secrets"
	"github.com/giovaniif/crew/internal/adapters/tmux"
	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestMateThreadIntegrationMentionResultReplyAndRestart(t *testing.T) {
	for _, bin := range []string{"git", "tmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@example.com"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@example.com"}} {
		t.Setenv(kv[0], kv[1])
	}
	home, err := filepath.EvalSymlinks(shortDir(t))
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(home, "src")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	api := filepath.Join(scratch, "api")
	web := filepath.Join(scratch, "web")
	cloneRepo(t, scratch, api)
	cloneRepo(t, scratch, web)
	config := filepath.Join(home, "config.toml")
	body := fmt.Sprintf("[[mate]]\nname = \"helper\"\nprompt = \"You help.\"\nagent = \"claude\"\nrepos = [%q]\nowner = \"U0OWNER01\"\n", api)
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgent := filepath.Join(home, "fake-agent")
	if err := os.WriteFile(fakeAgent, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	socket := fmt.Sprintf("crew-mate-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		_ = tmux.New(tmux.Config{Socket: socket, ConfigPath: filepath.Join(home, "tmux.conf")}).Close(context.Background())
	})
	hub := &fakeSlackHub{names: map[string]string{"U0GUEST01": "Gus"}}
	serve := func() liveDaemon {
		return serveLive(t, home, socket, fakeAgent,
			daemon.WithConfig(config),
			daemon.WithMateSecrets(secrets.Files{Dir: filepath.Join(home, "secrets")}),
			daemon.WithSlack(hub.connect, time.Hour),
			daemon.WithFirstPromptGrace(time.Hour))
	}
	live := serve()
	ctx := context.Background()
	for _, root := range []string{api, web} {
		if err := live.c.Call(ctx, rpc.MethodWorkspaceAdd, rpc.WorkspaceAddParams{Path: root}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := setTokens(live.c, "helper", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	m := hub.live(t)
	hub.send(m, app.SlackEvent{Kind: app.SlackMention, Team: "T0TEAM001", Channel: "C0CHAN001", User: "U0GUEST01", Text: "<@U0BOT0001> fix the login", TS: "1700.0001", ThreadTS: "1700.0001"})

	var session domain.Session
	waitFor(t, func() bool {
		for _, s := range live.sessions(t) {
			if s.Mate != nil {
				session = s
				return true
			}
		}
		return false
	})
	if session.Mate.Authority != "U0GUEST01" || !strings.HasPrefix(session.Dir, filepath.Join(home, "worktrees", "api")) {
		t.Fatalf("mate session %+v", session)
	}
	if n := len(strings.Split(gitIn(t, web, "worktree", "list"), "\n")); n != 1 {
		t.Fatalf("a repo out of the mate got %d worktrees", n)
	}

	stop := rpc.Hook{Harness: "claude", Event: "Stop", Pane: session.Pane, At: time.Now(), Payload: json.RawMessage(`{"last_assistant_message":"Fixed the login."}`)}
	if err := live.c.Call(ctx, rpc.MethodHook, stop, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, p := range hub.snapshot()[0].posts {
			if p.thread == "1700.0001" && p.text == "Fixed the login." {
				return true
			}
		}
		return false
	})

	live.cancel()
	live = serve()
	waitFor(t, func() bool { return len(hub.snapshot()) == 2 })
	m = hub.live(t)
	hub.send(m, app.SlackEvent{Kind: app.SlackThreadMessage, Team: "T0TEAM001", Channel: "C0CHAN001", User: "U0OWNER01", Text: "thanks, also the tests", TS: "1700.0009", ThreadTS: "1700.0001"})
	waitFor(t, func() bool {
		s := live.sessions(t)[session.ID]
		return s.Mate != nil && s.Mate.Authority == "U0OWNER01"
	})
	mates := 0
	for _, s := range live.sessions(t) {
		if s.Mate != nil {
			mates++
		}
	}
	if mates != 1 {
		t.Fatalf("%d mate sessions, want the same one", mates)
	}
}
