package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestNotifyFocusCommandFocusesTheSession(t *testing.T) {
	home := shortHome(t)
	t.Setenv("CREW_HOME", home)
	d, err := daemon.New(nopStore{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	c, err := rpc.Dial(rpc.SocketPath(home))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d.Post(daemon.SessionChanged{Session: domain.Session{ID: "s1", Unread: true}})
	<-sub.Diffs

	var stdout, stderr bytes.Buffer
	if code := run([]string{"focus", "s1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	select {
	case diff := <-sub.Diffs:
		if diff.Session == nil || !diff.Session.Focused || diff.Session.Unread {
			t.Fatalf("diff %+v", diff)
		}
	case <-time.After(time.Second):
		t.Fatal("the session was not focused")
	}

	stderr.Reset()
	if code := run([]string{"focus", "nope"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "nope") {
		t.Fatalf("unknown session: exit %d, stderr %q", code, stderr.String())
	}
	if code := run([]string{"focus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("no id: exit %d", code)
	}
}

func TestNotifyAttachNamesTheTerminalApp(t *testing.T) {
	env := map[string]string{"__CFBundleIdentifier": "", "TERM_PROGRAM": "iTerm.app"}
	p := openClientParams("/bin/crew", "/h", "/w", func(k string) string { return env[k] })
	if p.Terminal != "com.googlecode.iterm2" {
		t.Fatalf("terminal %q", p.Terminal)
	}
	if p.Command[0] != "/bin/crew" || p.Env["CREW_HOME"] != "/h" || p.Dir != "/w" {
		t.Fatalf("params %+v", p)
	}
}
