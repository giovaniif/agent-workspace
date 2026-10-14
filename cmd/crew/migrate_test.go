package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

type migrateRig struct {
	user  string
	log   bytes.Buffer
	cmds  []string
	alive bool
	goos  string
}

func newMigrateRig(t *testing.T) *migrateRig {
	t.Helper()
	return &migrateRig{user: t.TempDir(), goos: "linux"}
}

func (r *migrateRig) run(services bool) {
	r.log.Reset()
	m := migration{
		UserHome: r.user,
		Self:     "/new/bin/crew",
		Env:      func(string) string { return "" },
		Log:      &r.log,
		Live:     func(string) bool { return r.alive },
		Services: services,
		GOOS:     r.goos,
		UID:      501,
		Exec: func(_ context.Context, name string, args ...string) error {
			r.cmds = append(r.cmds, name+" "+strings.Join(args, " "))
			return nil
		},
	}
	m.run(context.Background())
}

func (r *migrateRig) path(parts ...string) string {
	return filepath.Join(append([]string{r.user}, parts...)...)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateMovesTheAgentwsHomeAndLeavesALink(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".agentws", "state.db"), "sessions")
	writeFile(t, r.path(".agentws", "config.toml"), "[serve]\n")
	r.run(false)
	if got := readFile(t, r.path(".crew", "state.db")); got != "sessions" {
		t.Fatalf("state.db = %q", got)
	}
	if target, err := os.Readlink(r.path(".agentws")); err != nil || target != r.path(".crew") {
		t.Fatalf("~/.agentws link = %q, %v", target, err)
	}
	if !strings.Contains(r.log.String(), "moved "+r.path(".agentws")+" to "+r.path(".crew")) {
		t.Fatalf("log %q", r.log.String())
	}
	r.run(false)
	if r.log.Len() != 0 {
		t.Fatalf("second run logged %q", r.log.String())
	}
	if got := readFile(t, r.path(".agentws", "config.toml")); got != "[serve]\n" {
		t.Fatalf("config through the link = %q", got)
	}
}

func TestMigrateRefusesToMoveTheHomeWhileTheOldDaemonRuns(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".agentws", "state.db"), "sessions")
	r.alive = true
	r.run(false)
	if info, err := os.Lstat(r.path(".agentws")); err != nil || !info.IsDir() {
		t.Fatalf("~/.agentws changed: %v", err)
	}
	if _, err := os.Stat(r.path(".crew")); !os.IsNotExist(err) {
		t.Fatalf("~/.crew made while the old daemon runs: %v", err)
	}
	if !strings.Contains(r.log.String(), "crew daemon stop") {
		t.Fatalf("log %q", r.log.String())
	}
}

func TestMigrateKeepsBothHomesWhenACrewHomeExists(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".agentws", "state.db"), "old")
	writeFile(t, r.path(".crew", "state.db"), "new")
	r.run(false)
	if readFile(t, r.path(".agentws", "state.db")) != "old" || readFile(t, r.path(".crew", "state.db")) != "new" {
		t.Fatal("a home changed")
	}
	if !strings.Contains(r.log.String(), "both") {
		t.Fatalf("log %q", r.log.String())
	}
}

func TestMigrateRewritesAgentwsHooksToCrewWithABackup(t *testing.T) {
	r := newMigrateRig(t)
	settings := r.path(".claude", "settings.json")
	writeFile(t, settings, "{}\n")
	if err := claude.Setup(settings, "/old/bin/agentws"); err != nil {
		t.Fatal(err)
	}
	if _, err := codex.Setup(codex.SetupConfig{Dir: r.path(".codex"), Command: "/old/bin/agentws"}); err != nil {
		t.Fatal(err)
	}
	r.run(false)
	for _, file := range []string{settings, r.path(".codex", "hooks.json")} {
		got := readFile(t, file)
		if domain.HasLegacyHook(got) || !strings.Contains(got, "/new/bin/crew hook --harness") {
			t.Fatalf("%s:\n%s", file, got)
		}
	}
	if _, err := os.Stat(claude.BackupPath(settings)); err != nil {
		t.Fatalf("no claude backup: %v", err)
	}
	backups, _ := filepath.Glob(r.path(".codex", "hooks.json.agentws-*.bak"))
	if len(backups) == 0 {
		t.Fatal("no codex backup")
	}
	r.run(false)
	if r.log.Len() != 0 {
		t.Fatalf("second run logged %q", r.log.String())
	}
}

func TestMigrateLeavesHarnessConfigWithoutAgentwsHooksAlone(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".claude", "settings.json"), `{"model": "opus"}`)
	r.run(false)
	if got := readFile(t, r.path(".claude", "settings.json")); got != `{"model": "opus"}` {
		t.Fatalf("settings changed: %s", got)
	}
	if _, err := os.Stat(r.path(".codex", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("codex hooks made: %v", err)
	}
}

func TestMigrateMovesTheNvimPluginDirAndUpdatesTheSetupFile(t *testing.T) {
	r := newMigrateRig(t)
	oldPlugin := r.path(".local", "share", "agentws", "nvim")
	writeFile(t, filepath.Join(oldPlugin, "plugin", "agentws.lua"), "-- plugin\n")
	setupFile := r.path(".config", "nvim", "plugin", "agentws.lua")
	writeFile(t, setupFile, domain.NvimSetupFile(oldPlugin))
	r.run(false)
	newPlugin := r.path(".local", "share", "crew", "nvim")
	if got := readFile(t, filepath.Join(newPlugin, "plugin", "agentws.lua")); got != "-- plugin\n" {
		t.Fatalf("plugin = %q", got)
	}
	if target, err := os.Readlink(r.path(".local", "share", "agentws")); err != nil || target != r.path(".local", "share", "crew") {
		t.Fatalf("old plugin link = %q, %v", target, err)
	}
	if got := readFile(t, setupFile); got != domain.NvimSetupFile(newPlugin) {
		t.Fatalf("setup file:\n%s", got)
	}
	r.run(false)
	if r.log.Len() != 0 {
		t.Fatalf("second run logged %q", r.log.String())
	}
}

func TestMigrateServicesStartsTheCrewUnitOnlyAfterTheOldOneStops(t *testing.T) {
	r := newMigrateRig(t)
	dir := r.path(".config", "systemd", "user")
	writeFile(t, filepath.Join(dir, "agentws-daemon.service"), "[Service]\nExecStart=/old/bin/agentws daemon\nEnvironment=AGENTWS_HOME="+r.path(".agentws")+"\n")
	writeFile(t, r.path(".agentws", "state.db"), "sessions")
	r.run(true)
	got := readFile(t, filepath.Join(dir, "crew-daemon.service"))
	if want := "[Service]\nExecStart=/new/bin/crew daemon\nEnvironment=CREW_HOME=" + r.path(".crew") + "\n"; got != want {
		t.Fatalf("crew unit:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "agentws-daemon.service")); !os.IsNotExist(err) {
		t.Fatalf("old unit left: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agentws-daemon.service.bak")); err != nil {
		t.Fatalf("no backup of the old unit: %v", err)
	}
	want := []string{
		"systemctl --user disable --now agentws-daemon.service",
		"systemctl --user daemon-reload",
		"systemctl --user enable --now crew-daemon.service",
	}
	if strings.Join(r.cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(r.cmds, "\n"), strings.Join(want, "\n"))
	}
	r.cmds = nil
	r.run(true)
	if len(r.cmds) != 0 || r.log.Len() != 0 {
		t.Fatalf("second run: %q, %q", r.cmds, r.log.String())
	}
}

func TestMigrateServicesMovesLaunchdAgentsIncludingBridges(t *testing.T) {
	r := newMigrateRig(t)
	r.goos = "darwin"
	dir := r.path("Library", "LaunchAgents")
	writeFile(t, filepath.Join(dir, "dev.agentws.bridge.vps.plist"), "<string>dev.agentws.bridge.vps</string>\n<string>/old/bin/agentws</string>\n")
	writeFile(t, filepath.Join(dir, "dev.other.plist"), "other")
	r.run(true)
	if got := readFile(t, filepath.Join(dir, "dev.crew.bridge.vps.plist")); got != "<string>dev.crew.bridge.vps</string>\n<string>/new/bin/crew</string>\n" {
		t.Fatalf("crew agent:\n%s", got)
	}
	want := []string{
		"launchctl bootout gui/501/dev.agentws.bridge.vps",
		"launchctl bootstrap gui/501 " + filepath.Join(dir, "dev.crew.bridge.vps.plist"),
	}
	if strings.Join(r.cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(r.cmds, "\n"))
	}
	if readFile(t, filepath.Join(dir, "dev.other.plist")) != "other" {
		t.Fatal("an agent that is not ours changed")
	}
}

func TestMigrateServicesPutsTheOldServiceBackWhenTheHomeCannotMove(t *testing.T) {
	r := newMigrateRig(t)
	dir := r.path(".config", "systemd", "user")
	unit := "[Service]\nExecStart=/old/bin/agentws daemon\n"
	writeFile(t, filepath.Join(dir, "agentws-daemon.service"), unit)
	writeFile(t, r.path(".agentws", "state.db"), "sessions")
	r.alive = true
	r.run(true)
	if got := readFile(t, filepath.Join(dir, "agentws-daemon.service")); got != unit {
		t.Fatalf("old unit:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "crew-daemon.service")); !os.IsNotExist(err) {
		t.Fatalf("crew unit left: %v", err)
	}
	if _, err := os.Stat(r.path(".crew")); !os.IsNotExist(err) {
		t.Fatalf("~/.crew made: %v", err)
	}
	want := []string{
		"systemctl --user disable --now agentws-daemon.service",
		"systemctl --user daemon-reload",
		"systemctl --user enable --now agentws-daemon.service",
	}
	if strings.Join(r.cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(r.cmds, "\n"))
	}
	if !strings.Contains(r.log.String(), "put back") {
		t.Fatalf("log %q", r.log.String())
	}
}
