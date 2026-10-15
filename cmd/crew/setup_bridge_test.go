package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestSetupBridgeAgentRunsTheBridgeWithThisShellsEnvironment(t *testing.T) {
	env := map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin", "CREW_HOME": "/Users/me/.crew"}
	a, remove, err := setupBridgeAgent([]string{"--remote-bin", "~/.local/bin/crew", "me@my.vps"}, func(k string) string { return env[k] }, "/usr/local/bin/crew", "/Users/me", 501)
	if err != nil || remove {
		t.Fatalf("remove %v err %v", remove, err)
	}
	if !strings.HasPrefix(a.Label, "dev.crew.bridge.me-my.vps-") || a.Dir != "/Users/me/Library/LaunchAgents" || a.UID != 501 {
		t.Fatalf("agent %+v", a)
	}
	if want := []string{"/usr/local/bin/crew", "notify", "bridge", "--remote-bin", "~/.local/bin/crew", "me@my.vps"}; !reflect.DeepEqual(a.Program, want) {
		t.Fatalf("program %q", a.Program)
	}
	if !reflect.DeepEqual(a.Env, env) || a.Log != "/Users/me/.crew/bridge-"+strings.TrimPrefix(a.Label, "dev.crew.bridge.")+".log" {
		t.Fatalf("env %v log %q", a.Env, a.Log)
	}
}

func TestSetupBridgeDefaultsHomeAndOmitsTheDefaultRemoteBin(t *testing.T) {
	a, _, err := setupBridgeAgent([]string{"vps"}, func(string) string { return "" }, "/bin/crew", "/Users/me", 501)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/bin/crew", "notify", "bridge", "vps"}; !reflect.DeepEqual(a.Program, want) {
		t.Fatalf("program %q", a.Program)
	}
	if a.Env["CREW_HOME"] != "/Users/me/.crew" || a.Log != "/Users/me/.crew/bridge-vps.log" {
		t.Fatalf("env %v log %q", a.Env, a.Log)
	}
}

func TestSetupBridgeRemoveNamesTheSameAgent(t *testing.T) {
	a, remove, err := setupBridgeAgent([]string{"--remove", "me@vps"}, func(string) string { return "" }, "/bin/crew", "/Users/me", 501)
	installed, _, _ := setupBridgeAgent([]string{"me@vps"}, func(string) string { return "" }, "/bin/crew", "/Users/me", 501)
	if err != nil || !remove || a.Label != installed.Label {
		t.Fatalf("agent %+v remove %v err %v", a, remove, err)
	}
}

func TestSetupBridgeNeedsOneHost(t *testing.T) {
	var stderr bytes.Buffer
	if code := runSetup([]string{"bridge"}, &bytes.Buffer{}, &stderr, func(string) string { return "" }, "/bin/crew"); code != 2 || !strings.Contains(stderr.String(), "usage: crew setup bridge") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestSetupBridgeKeepsHostsThatLookAlikeApart(t *testing.T) {
	label := func(host string) string {
		a, _, err := setupBridgeAgent([]string{host}, func(string) string { return "" }, "/bin/crew", "/Users/me", 501)
		if err != nil {
			t.Fatal(err)
		}
		return a.Label
	}
	if label("me@vps") == label("me-vps") {
		t.Fatalf("me@vps and me-vps share %s", label("me@vps"))
	}
	if got := label("vps.example.com"); got != "dev.crew.bridge.vps.example.com" {
		t.Fatalf("a label-safe host changed: %s", got)
	}
}
