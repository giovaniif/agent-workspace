package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const recipeFile = domain.RepoConfigFile

const modulesDir = "node_modules"

type RecipeSource interface {
	Load(repoDir string) (domain.Recipe, bool, error)
}

type MainCheckouts interface {
	MainCheckout(ctx context.Context, worktree string) (string, error)
}

type SetupFS interface {
	Exists(path string) bool
	Copy(src, dst string) error
	Symlink(target, link string) error
	CloneTree(ctx context.Context, src, dst string) error
	Lockfile(dir string) (domain.Lockfile, error)
	FreeBytes(path string) (int64, error)
}

type CommandRunner interface {
	Run(ctx context.Context, dir string, argv ...string) error
}

type WorktreeSetup struct {
	Recipes RecipeSource
	FS      SetupFS
	Runner  CommandRunner
	Git     MainCheckouts
	Now     func() time.Time
}

type SetupReport struct {
	Lines    []string
	Duration time.Duration
	DiskUsed int64
}

func (s WorktreeSetup) Run(ctx context.Context, worktree string) (SetupReport, error) {
	main, err := s.Git.MainCheckout(ctx, worktree)
	if err != nil {
		return SetupReport{}, err
	}
	if filepath.Clean(main) == filepath.Clean(worktree) {
		return SetupReport{}, fmt.Errorf("%s is the main checkout, not a linked worktree", worktree)
	}
	recipe, found, err := s.Recipes.Load(main)
	if err != nil {
		return SetupReport{}, err
	}
	if !found {
		return SetupReport{Lines: []string{"no recipe in " + filepath.Join(main, recipeFile)}}, nil
	}
	if err := recipe.Validate(); err != nil {
		return SetupReport{}, fmt.Errorf("%s: %w", filepath.Join(main, recipeFile), err)
	}

	run := &setupRun{s: s, ctx: ctx, main: main, worktree: worktree}
	started := s.Now()
	freeBefore, _ := s.FS.FreeBytes(worktree)
	err = run.apply(recipe)
	freeAfter, _ := s.FS.FreeBytes(worktree)
	return SetupReport{Lines: run.lines, Duration: s.Now().Sub(started), DiskUsed: freeBefore - freeAfter}, err
}

type setupRun struct {
	s        WorktreeSetup
	ctx      context.Context
	main     string
	worktree string
	lines    []string
}

func (r *setupRun) log(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *setupRun) apply(recipe domain.Recipe) error {
	for _, p := range recipe.Copy {
		if err := r.place("copy", p, r.s.FS.Copy); err != nil {
			return err
		}
	}
	for _, p := range recipe.Link {
		if err := r.place("link", p, r.s.FS.Symlink); err != nil {
			return err
		}
	}
	if err := r.deps(recipe.Deps); err != nil {
		return err
	}
	for _, command := range recipe.Run {
		if err := r.s.Runner.Run(r.ctx, r.worktree, "sh", "-c", command); err != nil {
			return fmt.Errorf("run %q: %w", command, err)
		}
		r.log("run %s", command)
	}
	return nil
}

func (r *setupRun) place(verb, rel string, do func(src, dst string) error) error {
	src, dst := filepath.Join(r.main, rel), filepath.Join(r.worktree, rel)
	switch {
	case !r.s.FS.Exists(src):
		r.log("skip %s %s: not in the main checkout", verb, rel)
		return nil
	case r.s.FS.Exists(dst):
		r.log("skip %s %s: already in the worktree", verb, rel)
		return nil
	}
	if err := do(src, dst); err != nil {
		return fmt.Errorf("%s %s: %w", verb, rel, err)
	}
	r.log("%s %s", verb, rel)
	return nil
}

func (r *setupRun) deps(mode domain.DepsMode) error {
	if mode == domain.DepsNone {
		return nil
	}
	src, dst := filepath.Join(r.main, modulesDir), filepath.Join(r.worktree, modulesDir)
	mainLock, err := r.s.FS.Lockfile(r.main)
	if err != nil {
		return err
	}
	worktreeLock, err := r.s.FS.Lockfile(r.worktree)
	if err != nil {
		return err
	}
	plan := domain.PlanDeps(mode, domain.DepsInput{
		MainHasModules: r.s.FS.Exists(src),
		Main:           mainLock,
		Worktree:       worktreeLock,
	})
	if plan.Action != domain.DepsSkip && plan.Action != domain.DepsRunInstall && r.s.FS.Exists(dst) {
		r.log("deps skip: %s already in the worktree", modulesDir)
		return nil
	}
	switch plan.Action {
	case domain.DepsCloneModules:
		err = r.s.FS.CloneTree(r.ctx, src, dst)
		if err == nil {
			r.log("deps clone %s", modulesDir)
		}
	case domain.DepsLinkModules:
		err = r.s.FS.Symlink(src, dst)
		if err == nil {
			r.log("deps link %s", modulesDir)
		}
	case domain.DepsRunInstall:
		command := strings.Join(plan.Command, " ")
		err = r.s.Runner.Run(r.ctx, r.worktree, plan.Command...)
		if err == nil {
			r.logInstall(plan.Reason, command)
		}
	default:
		r.log("deps skip: %s", plan.Reason)
	}
	if err != nil {
		return fmt.Errorf("deps %s: %w", mode, err)
	}
	return nil
}

func (r *setupRun) logInstall(reason, command string) {
	if reason == "" {
		r.log("deps install (%s)", command)
		return
	}
	r.log("deps install: %s (%s)", reason, command)
}
