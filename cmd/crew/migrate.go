package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/adapters/codex"
	"github.com/giovaniif/crew/internal/adapters/launchd"
	"github.com/giovaniif/crew/internal/adapters/omp"
	"github.com/giovaniif/crew/internal/adapters/onboard"
	"github.com/giovaniif/crew/internal/adapters/systemd"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type migration struct {
	UserHome string
	Self     string
	Env      func(string) string
	Log      io.Writer
	Live     func(socket string) bool
	Services bool
	GOOS     string
	UID      int
	Exec     func(ctx context.Context, name string, args ...string) error
}

type pendingService struct {
	path, name, oldPath string
}

func migrateState(services bool, stderr io.Writer) {
	if domain.Env(os.Getenv, "HOME") != "" {
		return
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	m := migration{
		UserHome: user,
		Self:     self,
		Env:      os.Getenv,
		Log:      stderr,
		Live:     socketLive,
		Services: services,
		GOOS:     runtime.GOOS,
		UID:      os.Getuid(),
	}
	m.run(context.Background())
}

func socketLive(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m migration) logf(format string, args ...any) {
	fmt.Fprintf(m.Log, "crew: "+format+"\n", args...)
}

func (m migration) or(key, fallback string) string {
	if v := m.Env(key); v != "" {
		return v
	}
	return fallback
}

func (m migration) exec(ctx context.Context, name string, args ...string) error {
	if m.Exec != nil {
		return m.Exec(ctx, name, args...)
	}
	if m.GOOS == "darwin" {
		return launchd.Exec(ctx, name, args...)
	}
	return systemd.Exec(ctx, name, args...)
}

func (m migration) oldHome() string { return filepath.Join(m.UserHome, domain.LegacyHomeDirName) }
func (m migration) newHome() string { return filepath.Join(m.UserHome, domain.HomeDirName) }

func (m migration) run(ctx context.Context) {
	var started []pendingService
	if m.Services {
		started = m.stopLegacyServices(ctx)
	}
	ready := m.moveHome()
	if ready {
		m.dropStaleRuntimeFiles()
	}
	m.moveClaudeBackup()
	m.rewriteHooks()
	m.rewriteOmpHook()
	m.moveNvim()
	m.retireNvimSetupFile()
	m.dropAliasLink()
	for _, s := range started {
		if ready {
			_ = m.startService(ctx, s)
		} else {
			m.restoreService(ctx, s)
		}
	}
}

func realDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (m migration) moveHome() bool {
	old, home := m.oldHome(), m.newHome()
	switch domain.PlanHomeMove(realDir(old), exists(home), m.Live(rpc.LegacySocketPath(old)) || m.Live(rpc.ListenPath(old))) {
	case domain.HomeMoveDir:
		if err := moveAndLink(old, home); err != nil {
			m.logf("could not move %s to %s: %v", old, home, err)
			return false
		}
		m.logf("moved %s to %s; %s links to it for one release", old, home, old)
	case domain.HomeBlocked:
		m.logf("an agentws daemon still runs from %s, so the state stays there; run 'crew daemon stop', then start crew again to move it to %s", old, home)
		return false
	case domain.HomeBoth:
		m.logf("both %s and %s exist; crew uses %s and leaves %s as it is", old, home, home, old)
	}
	return true
}

func moveAndLink(old, home string) error {
	if err := os.Rename(old, home); err != nil {
		return err
	}
	return os.Symlink(home, old)
}

func (m migration) dropStaleRuntimeFiles() {
	home := m.newHome()
	if m.Live(rpc.LegacySocketPath(home)) {
		return
	}
	for _, name := range []string{"agentws.sock", "agentws.pid", "agentws.lock"} {
		_ = os.Remove(filepath.Join(home, name))
	}
}

func (m migration) moveClaudeBackup() {
	settings := filepath.Join(m.or("CLAUDE_CONFIG_DIR", filepath.Join(m.UserHome, ".claude")), "settings.json")
	old := settings + ".agentws-backup"
	if !exists(old) || exists(claude.BackupPath(settings)) {
		return
	}
	if err := os.Rename(old, claude.BackupPath(settings)); err != nil {
		m.logf("could not move %s to %s: %v", old, claude.BackupPath(settings), err)
		return
	}
	m.logf("moved %s to %s", old, claude.BackupPath(settings))
}

func (m migration) rewriteHooks() {
	settings := filepath.Join(m.or("CLAUDE_CONFIG_DIR", filepath.Join(m.UserHome, ".claude")), "settings.json")
	if legacyHooksIn(settings) {
		if err := claude.Setup(settings, m.Self); err != nil {
			m.logf("could not rewrite the hooks in %s: %v", settings, err)
		} else {
			m.logf("rewrote the agentws hooks in %s to run %s (backup: %s)", settings, m.Self, claude.BackupPath(settings))
		}
	}
	codexDir := m.or("CODEX_HOME", filepath.Join(m.UserHome, ".codex"))
	hooks := filepath.Join(codexDir, "hooks.json")
	if legacyHooksIn(hooks) {
		res, err := codex.Setup(codex.SetupConfig{Dir: codexDir, Command: m.Self})
		if err != nil {
			m.logf("could not rewrite the hooks in %s: %v", hooks, err)
		} else {
			m.logf("rewrote the agentws hooks in %s to run %s (backup: %s)", hooks, m.Self, res.Backup)
		}
	}
}

func legacyHooksIn(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && domain.HasLegacyHook(string(b))
}

func (m migration) moveNvim() {
	data := m.or("XDG_DATA_HOME", filepath.Join(m.UserHome, ".local", "share"))
	old, dir := filepath.Join(data, "agentws"), filepath.Join(data, "crew")
	if !realDir(old) || exists(dir) {
		return
	}
	if err := moveAndLink(old, dir); err != nil {
		m.logf("could not move %s to %s: %v", old, dir, err)
		return
	}
	m.logf("moved %s to %s; %s links to it for one release", old, dir, old)
}

func (m migration) envWithHome(k string) string {
	if k == "HOME" {
		return m.UserHome
	}
	return m.Env(k)
}

func (m migration) retireNvimSetupFile() {
	probe := onboard.FromEnv(m.newHome(), m.Self, m.envWithHome)
	old := filepath.Join(probe.NvimConfigDir, "plugin", "agentws.lua")
	current, err := os.ReadFile(old)
	if err != nil || !domain.IsLegacyNvimSetupFile(string(current)) {
		return
	}
	if _, err := probe.InstallNvim(context.Background()); err != nil {
		m.logf("could not replace %s: %v", old, err)
		return
	}
	if err := os.Rename(old, old+".bak"); err != nil {
		m.logf("could not move %s aside: %v", old, err)
		return
	}
	m.logf("replaced %s with %s (backup: %s.bak)", old, filepath.Join(probe.NvimConfigDir, "plugin", "crew.lua"), old)
}

func (m migration) rewriteOmpHook() {
	dir, err := omp.AgentDir(m.envWithHome)
	if err != nil || !omp.HasLegacyHook(dir) {
		return
	}
	res, err := omp.Setup(omp.SetupConfig{Dir: dir, Command: m.Self})
	if err != nil {
		m.logf("could not replace the hook in %s: %v", omp.LegacyHookFile(dir), err)
		return
	}
	m.logf("replaced the agentws hook %s with %s (backup: %s.bak)", omp.LegacyHookFile(dir), res.File, omp.LegacyHookFile(dir))
}

func (m migration) dropAliasLink() {
	link := filepath.Join(filepath.Dir(m.Self), "agentws")
	target, err := os.Readlink(link)
	if err != nil {
		return
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	if filepath.Clean(target) != filepath.Clean(m.Self) {
		return
	}
	if err := os.Remove(link); err != nil {
		m.logf("could not remove the agentws link %s: %v", link, err)
		return
	}
	m.logf("removed the agentws link %s", link)
}

func (m migration) serviceDir() string {
	if m.GOOS == "darwin" {
		return filepath.Join(m.UserHome, "Library", "LaunchAgents")
	}
	return filepath.Join(m.or("XDG_CONFIG_HOME", filepath.Join(m.UserHome, ".config")), "systemd", "user")
}

func (m migration) stopLegacyServices(ctx context.Context) []pendingService {
	dir := m.serviceDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var pending []pendingService
	for _, e := range entries {
		newName, ok := domain.LegacyServiceName(e.Name())
		if !ok || strings.HasSuffix(e.Name(), ".bak") {
			continue
		}
		oldPath, newPath := filepath.Join(dir, e.Name()), filepath.Join(dir, newName)
		if exists(newPath) {
			m.logf("both %s and %s exist; left %s as it is", oldPath, newPath, oldPath)
			continue
		}
		if err := m.moveService(ctx, oldPath, newPath); err != nil {
			m.logf("could not move %s to %s: %v", oldPath, newPath, err)
			continue
		}
		m.logf("replaced %s with %s (backup: %s.bak)", oldPath, newPath, oldPath)
		pending = append(pending, pendingService{path: newPath, name: newName, oldPath: oldPath})
	}
	return pending
}

func (m migration) moveService(ctx context.Context, oldPath, newPath string) error {
	b, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(oldPath+".bak", b, 0o644); err != nil {
		return err
	}
	content := domain.RewriteServiceFile(string(b), m.Self, m.oldHome(), m.newHome())
	if err := os.WriteFile(newPath, []byte(content), 0o644); err != nil {
		return err
	}
	if err := m.stopService(ctx, filepath.Base(oldPath)); err != nil {
		return errors.Join(err, os.Remove(newPath))
	}
	return os.Remove(oldPath)
}

func (m migration) stopService(ctx context.Context, file string) error {
	if m.GOOS == "darwin" {
		label := strings.TrimSuffix(file, ".plist")
		_ = m.exec(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d/%s", m.UID, label))
		return nil
	}
	return m.exec(ctx, "systemctl", "--user", "disable", "--now", file)
}

func (m migration) startService(ctx context.Context, s pendingService) error {
	var err error
	if m.GOOS == "darwin" {
		err = m.exec(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", m.UID), s.path)
	} else {
		err = m.exec(ctx, "systemctl", "--user", "daemon-reload")
		if err == nil {
			err = m.exec(ctx, "systemctl", "--user", "enable", "--now", s.name)
		}
	}
	if err != nil {
		m.logf("could not start %s: %v", s.path, err)
	}
	return err
}

func (m migration) restoreService(ctx context.Context, s pendingService) {
	b, err := os.ReadFile(s.oldPath + ".bak")
	if err == nil {
		err = os.WriteFile(s.oldPath, b, 0o644)
	}
	if err == nil {
		err = os.Remove(s.path)
	}
	if err == nil {
		err = m.startService(ctx, pendingService{path: s.oldPath, name: filepath.Base(s.oldPath)})
	}
	if err != nil {
		m.logf("could not put back %s: %v", s.oldPath, err)
		return
	}
	m.logf("put back %s, because %s did not move; run crew setup again after 'crew daemon stop'", s.oldPath, m.oldHome())
}
