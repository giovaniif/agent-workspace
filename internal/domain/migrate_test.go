package domain

import "testing"

func TestResolveHomePrefersEnvThenCrewThenAnUnmovedAgentwsDir(t *testing.T) {
	cases := []struct {
		name string
		f    HomeFacts
		want string
	}{
		{"env wins", HomeFacts{Env: "/x", User: "/u", NewExists: true, OldDir: true}, "/x"},
		{"crew home when it exists", HomeFacts{User: "/u", NewExists: true, OldDir: true}, "/u/.crew"},
		{"an unmoved agentws dir until the move", HomeFacts{User: "/u", OldDir: true}, "/u/.agentws"},
		{"crew home on a fresh install", HomeFacts{User: "/u"}, "/u/.crew"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveHome(c.f); got != c.want {
				t.Fatalf("ResolveHome(%+v) = %q, want %q", c.f, got, c.want)
			}
		})
	}
}

func TestPlanHomeMoveNeverMovesUnderALiveDaemonOrOverACrewHome(t *testing.T) {
	cases := []struct {
		name                     string
		oldDir, newExists, alive bool
		want                     HomeMove
	}{
		{"nothing to move", false, false, false, HomeStay},
		{"already moved", false, true, false, HomeStay},
		{"moves an agentws dir", true, false, false, HomeMoveDir},
		{"refuses while the old daemon runs", true, false, true, HomeBlocked},
		{"keeps both when crew home exists", true, true, false, HomeBoth},
		{"keeps both even with a live daemon", true, true, true, HomeBoth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PlanHomeMove(c.oldDir, c.newExists, c.alive); got != c.want {
				t.Fatalf("PlanHomeMove(%v, %v, %v) = %v, want %v", c.oldDir, c.newExists, c.alive, got, c.want)
			}
		})
	}
}

func TestTmuxSocketAdoptsTheOldServerWhileItHasAgentPanes(t *testing.T) {
	cases := []struct {
		name, env string
		legacy    bool
		want      string
	}{
		{"env wins", "dev", true, "dev"},
		{"old server with panes", "", true, "agentws"},
		{"new server otherwise", "", false, "crew"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TmuxSocket(c.env, c.legacy); got != c.want {
				t.Fatalf("TmuxSocket(%q, %v) = %q, want %q", c.env, c.legacy, got, c.want)
			}
		})
	}
}

func TestHasLegacyHookFindsHooksThatRunTheAgentwsBinary(t *testing.T) {
	cases := []struct {
		name, content string
		want          bool
	}{
		{"plain path", `"command": "/home/u/.local/bin/agentws hook --harness claude --event Stop"`, true},
		{"quoted path", `"command": "'/opt/my apps/agentws' hook --harness codex --event Stop"`, true},
		{"bare name", `"command": "agentws hook --harness claude --event Stop"`, true},
		{"crew hook", `"command": "/home/u/.local/bin/crew hook --harness claude --event Stop"`, false},
		{"no hooks", `{"model": "opus"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasLegacyHook(c.content); got != c.want {
				t.Fatalf("HasLegacyHook(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestRewriteServiceFileRenamesTheServiceBinaryAndHome(t *testing.T) {
	unit := "[Service]\nExecStart=/home/u/.local/bin/agentws daemon\nEnvironment=AGENTWS_HOME=/home/u/.agentws\nStandardOutput=append:/home/u/.agentws/daemon-service.log\n# agentws-daemon\n"
	want := "[Service]\nExecStart=/home/u/.local/bin/crew daemon\nEnvironment=CREW_HOME=/home/u/.crew\nStandardOutput=append:/home/u/.crew/daemon-service.log\n# crew-daemon\n"
	if got := RewriteServiceFile(unit, "/home/u/.local/bin/crew", "/home/u/.agentws", "/home/u/.crew"); got != want {
		t.Fatalf("unit:\n%s\nwant:\n%s", got, want)
	}
	plist := "<string>dev.agentws.bridge.vps</string>\n<string>/Applications/x/agentws</string>\n<string>--remote-bin</string>\n<string>agentws</string>\n<string>agentws-serve</string>\n"
	wantPlist := "<string>dev.crew.bridge.vps</string>\n<string>/usr/local/bin/crew</string>\n<string>--remote-bin</string>\n<string>agentws</string>\n<string>crew-serve</string>\n"
	if got := RewriteServiceFile(plist, "/usr/local/bin/crew", "/Users/u/.agentws", "/Users/u/.crew"); got != wantPlist {
		t.Fatalf("plist:\n%s\nwant:\n%s", got, wantPlist)
	}
}

func TestLegacyServiceNameMapsEachOldNameToItsCrewName(t *testing.T) {
	cases := map[string]string{
		"agentws-daemon.service":          "crew-daemon.service",
		"agentws-serve.service":           "crew-serve.service",
		"dev.agentws.daemon.plist":        "dev.crew.daemon.plist",
		"dev.agentws.bridge.my-vps.plist": "dev.crew.bridge.my-vps.plist",
	}
	for old, want := range cases {
		if got, ok := LegacyServiceName(old); !ok || got != want {
			t.Fatalf("LegacyServiceName(%q) = %q, %v, want %q", old, got, ok, want)
		}
	}
	for _, other := range []string{"crew-daemon.service", "foo.service", "dev.other.plist"} {
		if _, ok := LegacyServiceName(other); ok {
			t.Fatalf("LegacyServiceName(%q) matched", other)
		}
	}
}
