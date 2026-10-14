package daemon

import (
	"cmp"
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	wsfs "github.com/giovaniif/agent-workspace/internal/adapters/fs"
	gitadapter "github.com/giovaniif/agent-workspace/internal/adapters/git"
	"github.com/giovaniif/agent-workspace/internal/adapters/github"
	"github.com/giovaniif/agent-workspace/internal/adapters/notify"
	"github.com/giovaniif/agent-workspace/internal/adapters/nvim"
	"github.com/giovaniif/agent-workspace/internal/adapters/omp"
	"github.com/giovaniif/agent-workspace/internal/adapters/onboard"
	"github.com/giovaniif/agent-workspace/internal/adapters/procs"
	"github.com/giovaniif/agent-workspace/internal/adapters/setup"
	"github.com/giovaniif/agent-workspace/internal/adapters/sqlite"
	"github.com/giovaniif/agent-workspace/internal/adapters/tmux"
	"github.com/giovaniif/agent-workspace/internal/adapters/webpush"
	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func Run(ctx context.Context, home string) (err error) {
	lock, err := Acquire(home)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Release()) }()

	store, err := sqlite.Open(filepath.Join(home, "state.db"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()

	noMouse, err := LoadNoMouse(filepath.Join(home, "config.toml"))
	if err != nil {
		log.Printf("[ui] mouse ignored: %v", err)
	}
	host := tmux.New(tmux.Config{
		Socket:     domain.Env(os.Getenv, "TMUX_SOCKET"),
		ConfigPath: filepath.Join(home, "tmux.conf"),
		NoMouse:    noMouse,
	})
	worktreeHome, err := realDir(filepath.Join(home, "worktrees"))
	if err != nil {
		return err
	}
	recipe := app.WorktreeSetup{
		Recipes: setup.Recipes{},
		FS:      setup.FS{},
		Runner:  setup.Shell{Out: os.Stderr},
		Git:     gitadapter.Worktrees{},
		Now:     time.Now,
	}
	runRecipe := func(ctx context.Context, worktree string) error {
		_, err := recipe.Run(ctx, worktree)
		return err
	}
	sounds, err := notify.LoadSounds(filepath.Join(home, "notify.json"))
	if err != nil {
		log.Printf("notify.json ignored: %v", err)
	}
	startDefaults, err := LoadStartDefaults(filepath.Join(home, "config.toml"))
	if err != nil {
		log.Printf("new-session defaults ignored: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	banners := notify.Detect(notify.Click{Self: self, Home: home})
	trash := wsfs.NewTrash(filepath.Join(home, "trash"), 4)
	audit := &wsfs.AuditLog{Path: filepath.Join(home, "cleanup.log")}
	hooks := testHooksFromEnv(os.Getenv)
	cleanup := app.NewCleanup(gitadapter.Worktrees{}, procs.Table{}, trash, audit, filepath.Join(home, "backups"), hooks.cleanupClock)
	sizes := app.NewDiskSizes(wsfs.Du{}, diskWorkers, diskSizeTTL, time.Now)
	opts := []Option{
		WithWorkspaces(wsfs.FS{}, gitadapter.Inspector{}),
		WithHarnesses(host, claude.Adapter{}, codex.Adapter{}, omp.Adapter{}),
		WithSessions(gitadapter.Adder{}, runRecipe, worktreeHome),
		WithProjectSetup(setup.Shell{Out: os.Stderr}),
		WithAutomationPrecheck(setup.Shell{Out: os.Stderr}),
		WithAutomationSchedule(scheduleRecheck),
		WithStartDefaults(startDefaults),
		WithHarnessConfig(readHarnessConfig),
		WithConfig(filepath.Join(home, "config.toml")),
		WithNotifier(banners, notify.New(), sounds),
		WithWorktrees(gitadapter.Worktrees{}, &github.Finder{}),
		WithTitles(app.TitleResolvers{github.Titles{}}),
		WithProcessTable(procs.Table{}),
		WithReview(gitadapter.Review{}),
		WithCleanup(cleanup, DefaultCleanupEvery),
		WithSlotWatch(slotWatchEvery),
		WithDisk(DiskDeps{Sizes: sizes, Volume: wsfs.Volume{}, History: audit, VolumePath: home, DepsStore: DepsStorePath()}),
		WithTerminals(home, nvim.Editor{}),
		WithHunks(gitadapter.Review{}),
		WithOnboarding(onboard.FromEnv(home, self, os.Getenv)),
		WithTranscripts(Transcripts(wsfs.Transcripts{}), wsfs.Transcripts{}),
		WithPush(webpush.New(filepath.Join(home, "vapid"), nil)),
	}
	awayAfter, err := LoadAwayAfter(filepath.Join(home, "config.toml"))
	if err != nil {
		log.Printf("[push] away_after ignored: %v", err)
	}
	if awayAfter > 0 {
		opts = append(opts, WithPresence(host, awayAfter, PresenceEvery))
	}
	if hooks.prPoll > 0 {
		opts = append(opts, WithWorktreePoll(DefaultWorktreePoll, hooks.prPoll))
	}
	d, err := New(store, os.Getpid(), opts...)
	if err != nil {
		return err
	}
	d.SetClientHost(host)

	sock := rpc.SocketPath(home)
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(sock) }()
	if err := os.Chmod(sock, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	return d.Serve(ctx, ln)
}

func Transcripts(files app.TranscriptFiles) app.Transcripts {
	return app.Transcripts{Files: files, Parsers: map[domain.Harness]func() app.TranscriptParser{
		domain.HarnessClaude: func() app.TranscriptParser { return &claude.TranscriptParser{} },
		domain.HarnessCodex:  func() app.TranscriptParser { return &codex.TranscriptParser{} },
	}}
}

func realDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(dir)
}

type testHooks struct {
	clockSkew time.Duration
	prPoll    time.Duration
}

func testHooksFromEnv(getenv func(string) string) testHooks {
	var h testHooks
	if v := domain.Env(getenv, "TEST_CLOCK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			h.clockSkew = d
		} else {
			log.Printf("CREW_TEST_CLOCK ignored: %v", err)
		}
	}
	if v := domain.Env(getenv, "TEST_PR_POLL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			h.prPoll = d
		} else {
			log.Printf("CREW_TEST_PR_POLL ignored: %q", v)
		}
	}
	return h
}

func (h testHooks) cleanupClock() time.Time { return time.Now().Add(h.clockSkew) }

func readHarnessConfig() map[domain.Harness]StartDefaults {
	out := map[domain.Harness]StartDefaults{}
	home, _ := os.UserHomeDir()
	claudeDir := cmp.Or(os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude"))
	if model, effort, err := claude.ReadDefaults(filepath.Join(claudeDir, "settings.json")); err == nil {
		out[domain.HarnessClaude] = StartDefaults{Model: model, Effort: effort}
	}
	codexDir := cmp.Or(os.Getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	if model, effort, err := codex.ReadDefaults(filepath.Join(codexDir, "config.toml")); err == nil {
		out[domain.HarnessCodex] = StartDefaults{Model: model, Effort: effort}
	}
	return out
}
