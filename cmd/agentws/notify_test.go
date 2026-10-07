package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestNotifyBridgeStreamsOverSSHAndFocusesThroughIt(t *testing.T) {
	p, err := parseBridge([]string{"--remote-bin", "~/.local/bin/agentws", "me@vps"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ssh", "-T", "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ServerAliveInterval=15", "me@vps", "~/'.local/bin/agentws' notify stream"}; !reflect.DeepEqual(p.streamArgv(), want) {
		t.Fatalf("stream argv %q", p.streamArgv())
	}
	if want := `ssh -T -o RemoteCommand=none -o RequestTTY=no 'me@vps' ~/'.local/bin/agentws' focus`; p.focusCmd() != want {
		t.Fatalf("focus cmd %q", p.focusCmd())
	}
}

func TestNotifyBridgeDefaultsToAgentwsOnTheRemotePath(t *testing.T) {
	p, err := parseBridge([]string{"vps"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.streamArgv(); got[len(got)-1] != "'agentws' notify stream" {
		t.Fatalf("stream argv %q", got)
	}
}

func TestNotifyBridgeNeedsExactlyOneHost(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		if _, err := parseBridge(args); err == nil {
			t.Fatalf("%q accepted", args)
		}
	}
}

func TestNotifyUnknownSubcommandIsUsage(t *testing.T) {
	var stderr bytes.Buffer
	if code := runNotify([]string{"bogus"}, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage: agentws notify") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}
