package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/domain"
)

type migrateRig struct {
	user  string
	log   bytes.Buffer
	cmds  []string
	alive bool
	goos  string
	self  string
}

func newMigrateRig(t *testing.T) *migrateRig {
	t.Helper()
	return &migrateRig{user: t.TempDir(), goos: "linux", self: "/new/bin/crew"}
}

func (r *migrateRig) run(services bool) {
	r.log.Reset()
	m := migration{
		UserHome: r.user,
		Self:     r.self,
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
	backups, _ := filepath.Glob(r.path(".codex", "hooks.json.crew-*.bak"))
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

const legacyNvimSetup = "-- agentws: written by agentws setup nvim; agentws setup nvim --remove deletes this file.\nlocal dir = '%s'\nvim.opt.runtimepath:prepend(dir)\ndofile(dir .. '/plugin/agentws.lua')\nrequire('agentws').setup({})\n"

func TestMigrateMovesTheNvimPluginDirAndReplacesTheSetupFile(t *testing.T) {
	r := newMigrateRig(t)
	oldPlugin := r.path(".local", "share", "agentws", "nvim")
	writeFile(t, filepath.Join(oldPlugin, "plugin", "crew.lua"), "-- plugin\n")
	oldSetup := r.path(".config", "nvim", "plugin", "agentws.lua")
	legacy := fmt.Sprintf(legacyNvimSetup, oldPlugin)
	writeFile(t, oldSetup, legacy)
	r.run(false)
	newPlugin := r.path(".local", "share", "crew", "nvim")
	if got := readFile(t, filepath.Join(newPlugin, "plugin", "crew.lua")); got != "-- plugin\n" {
		t.Fatalf("plugin = %q", got)
	}
	if target, err := os.Readlink(r.path(".local", "share", "agentws")); err != nil || target != r.path(".local", "share", "crew") {
		t.Fatalf("old plugin link = %q, %v", target, err)
	}
	if got := readFile(t, r.path(".config", "nvim", "plugin", "crew.lua")); got != domain.NvimSetupFile(newPlugin) {
		t.Fatalf("setup file:\n%s", got)
	}
	if exists(oldSetup) {
		t.Fatal("the old setup file still loads")
	}
	if got := readFile(t, oldSetup+".bak"); got != legacy {
		t.Fatalf("backup:\n%s", got)
	}
	r.run(false)
	if r.log.Len() != 0 {
		t.Fatalf("second run logged %q", r.log.String())
	}
}

func TestMigrateReplacesAnOldNvimSetupFileEvenWhenThePluginDirMovedEarlier(t *testing.T) {
	r := newMigrateRig(t)
	newPlugin := r.path(".local", "share", "crew", "nvim")
	writeFile(t, filepath.Join(newPlugin, "plugin", "crew.lua"), "-- plugin\n")
	oldSetup := r.path(".config", "nvim", "plugin", "agentws.lua")
	writeFile(t, oldSetup, fmt.Sprintf(legacyNvimSetup, r.path(".local", "share", "agentws", "nvim")))
	r.run(false)
	if got := readFile(t, r.path(".config", "nvim", "plugin", "crew.lua")); got != domain.NvimSetupFile(newPlugin) {
		t.Fatalf("setup file:\n%s", got)
	}
	if exists(oldSetup) || !exists(oldSetup+".bak") {
		t.Fatal("the old setup file was not moved aside")
	}
}

func TestMigrateLeavesAnNvimFileOfTheUsersAlone(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, filepath.Join(r.path(".local", "share", "crew", "nvim"), "plugin", "crew.lua"), "-- plugin\n")
	writeFile(t, r.path(".config", "nvim", "plugin", "agentws.lua"), "my own file\n")
	r.run(false)
	if got := readFile(t, r.path(".config", "nvim", "plugin", "agentws.lua")); got != "my own file\n" {
		t.Fatalf("changed: %q", got)
	}
	if exists(r.path(".config", "nvim", "plugin", "crew.lua")) {
		t.Fatal("wrote a setup file next to a file of the user")
	}
}

func TestMigrateReplacesTheOldOmpHookWithABackup(t *testing.T) {
	r := newMigrateRig(t)
	oldHook := r.path(".omp", "agent", "hooks", "post", "agentws.ts")
	legacy := "// agentws: written by agentws setup omp; agentws setup omp --remove deletes this file.\nconst agentws = \"/old/bin/agentws\";\n"
	writeFile(t, oldHook, legacy)
	r.run(false)
	if got := readFile(t, r.path(".omp", "agent", "hooks", "post", "crew.ts")); !strings.Contains(got, `const crew = "/new/bin/crew";`) {
		t.Fatalf("crew hook:\n%s", got)
	}
	if exists(oldHook) {
		t.Fatal("the old hook still runs")
	}
	if got := readFile(t, oldHook+".bak"); got != legacy {
		t.Fatalf("backup:\n%s", got)
	}
}

func TestMigrateLeavesAnOmpHookOfTheUsersAlone(t *testing.T) {
	r := newMigrateRig(t)
	oldHook := r.path(".omp", "agent", "hooks", "post", "agentws.ts")
	writeFile(t, oldHook, "export default () => {}\n")
	r.run(false)
	if got := readFile(t, oldHook); got != "export default () => {}\n" {
		t.Fatalf("changed: %q", got)
	}
	if exists(r.path(".omp", "agent", "hooks", "post", "crew.ts")) {
		t.Fatal("installed a hook next to a hook of the user")
	}
}

func TestMigrateRemovesTheAgentwsLinkToTheBinary(t *testing.T) {
	r := newMigrateRig(t)
	bin := r.path("bin")
	r.self = filepath.Join(bin, "crew")
	writeFile(t, r.self, "binary")
	if err := os.Symlink("crew", filepath.Join(bin, "agentws")); err != nil {
		t.Fatal(err)
	}
	r.run(false)
	if _, err := os.Lstat(filepath.Join(bin, "agentws")); !os.IsNotExist(err) {
		t.Fatalf("link left: %v", err)
	}
	if readFile(t, r.self) != "binary" {
		t.Fatal("the binary changed")
	}
}

func TestMigrateKeepsAnOldAgentwsBinary(t *testing.T) {
	r := newMigrateRig(t)
	bin := r.path("bin")
	r.self = filepath.Join(bin, "crew")
	writeFile(t, r.self, "binary")
	writeFile(t, filepath.Join(bin, "agentws"), "old binary")
	r.run(false)
	if got := readFile(t, filepath.Join(bin, "agentws")); got != "old binary" {
		t.Fatalf("old binary = %q", got)
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

func TestMigrateDropsTheStaleRuntimeFilesOfAnOldDaemon(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".agentws", "state.db"), "sessions")
	for _, name := range []string{"agentws.sock", "agentws.pid", "agentws.lock"} {
		writeFile(t, r.path(".agentws", name), "")
	}
	r.run(false)
	if got := readFile(t, r.path(".crew", "state.db")); got != "sessions" {
		t.Fatalf("state.db = %q", got)
	}
	for _, name := range []string{"agentws.sock", "agentws.pid", "agentws.lock"} {
		if exists(r.path(".crew", name)) {
			t.Errorf("%s is still in ~/.crew", name)
		}
	}
}

func TestMigrateKeepsTheRuntimeFilesWhileAnOldDaemonRuns(t *testing.T) {
	r := newMigrateRig(t)
	r.alive = true
	writeFile(t, r.path(".agentws", "agentws.sock"), "")
	r.run(false)
	if !exists(r.path(".agentws", "agentws.sock")) {
		t.Fatal("the socket of a live daemon was removed")
	}
}

func TestMigrateMovesTheOldClaudeSettingsBackup(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".claude", "settings.json"), "{}")
	writeFile(t, r.path(".claude", "settings.json.agentws-backup"), `{"theme":"light"}`)
	r.run(false)
	if got := readFile(t, claude.BackupPath(r.path(".claude", "settings.json"))); got != `{"theme":"light"}` {
		t.Fatalf("backup = %q", got)
	}
	if exists(r.path(".claude", "settings.json.agentws-backup")) {
		t.Fatal("the old backup name is still there")
	}
}

func TestMigrateKeepsANewClaudeBackupThatAlreadyExists(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, r.path(".claude", "settings.json"), "{}")
	writeFile(t, r.path(".claude", "settings.json.agentws-backup"), "old")
	writeFile(t, claude.BackupPath(r.path(".claude", "settings.json")), "new")
	r.run(false)
	if got := readFile(t, claude.BackupPath(r.path(".claude", "settings.json"))); got != "new" {
		t.Fatalf("backup = %q", got)
	}
	if got := readFile(t, r.path(".claude", "settings.json.agentws-backup")); got != "old" {
		t.Fatalf("old backup = %q", got)
	}
}

func TestMigrateKeepsAnEarlierBackupOfTheOldNvimSetupFile(t *testing.T) {
	r := newMigrateRig(t)
	writeFile(t, filepath.Join(r.path(".local", "share", "crew", "nvim"), "plugin", "crew.lua"), "-- plugin\n")
	oldSetup := r.path(".config", "nvim", "plugin", "agentws.lua")
	legacy := fmt.Sprintf(legacyNvimSetup, r.path(".local", "share", "agentws", "nvim"))
	writeFile(t, oldSetup, legacy)
	writeFile(t, oldSetup+".bak", "earlier backup")
	r.run(false)
	if got := readFile(t, oldSetup+".bak"); got != "earlier backup" {
		t.Fatalf("earlier backup = %q", got)
	}
	if got := readFile(t, oldSetup+".bak.1"); got != legacy {
		t.Fatalf("new backup = %q", got)
	}
}
