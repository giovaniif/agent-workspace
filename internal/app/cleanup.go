package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type WorktreeGitFacts struct {
	InDefault   bool
	OnDefault   bool
	Uncommitted int
	ModifiedAt  time.Time
	Fingerprint string
}

type CleanupGit interface {
	CleanupFacts(ctx context.Context, w domain.Worktree) (WorktreeGitFacts, error)
	Backup(ctx context.Context, w domain.Worktree, dir string) error
	CreateBranch(ctx context.Context, w domain.Worktree, name string) (string, error)
	Prune(ctx context.Context, repo string) error
}

type WorktreeHolders interface {
	Holders(ctx context.Context, paths []string) (map[string][]string, error)
}

type Trash interface {
	Move(path string) error
	Purge()
}

type CleanupAudit interface {
	Record(CleanupRecord)
}

type CleanupRecord struct {
	At      time.Time            `json:"at"`
	Path    string               `json:"path"`
	Branch  string               `json:"branch,omitempty"`
	Action  domain.CleanupAction `json:"action"`
	Reason  string               `json:"reason"`
	Outcome string               `json:"outcome"`
}

type SessionActivity struct {
	Live         bool
	LastActivity time.Time
	Project      string
}

type CleanupResult struct {
	Decision domain.CleanupDecision
	Outcome  string
}

type Cleanup struct {
	git        CleanupGit
	procs      WorktreeHolders
	trash      Trash
	audit      CleanupAudit
	backupRoot string
	now        func() time.Time

	mu       sync.Mutex
	backedUp map[string]string
	usedDirs map[string]bool
}

func NewCleanup(git CleanupGit, procs WorktreeHolders, trash Trash, audit CleanupAudit, backupRoot string, now func() time.Time) *Cleanup {
	return &Cleanup{git: git, procs: procs, trash: trash, audit: audit, backupRoot: backupRoot, now: now, backedUp: map[string]string{}, usedDirs: map[string]bool{}}
}

type plannedWorktree struct {
	decision    domain.CleanupDecision
	fingerprint string
}

func (c *Cleanup) Plan(ctx context.Context, wts []domain.Worktree, activity func(domain.Worktree) SessionActivity) []domain.CleanupDecision {
	planned := c.plan(ctx, wts, activity)
	out := make([]domain.CleanupDecision, len(planned))
	for i, p := range planned {
		out[i] = p.decision
	}
	return out
}

func (c *Cleanup) plan(ctx context.Context, wts []domain.Worktree, activity func(domain.Worktree) SessionActivity) []plannedWorktree {
	paths := make([]string, len(wts))
	for i, w := range wts {
		paths[i] = w.Path
	}
	holders, procErr := c.procs.Holders(ctx, paths)
	gitFacts := make([]WorktreeGitFacts, len(wts))
	gitErrs := make([]error, len(wts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, refreshParallelism)
	for i := range wts {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			gitFacts[i], gitErrs[i] = c.git.CleanupFacts(ctx, wts[i])
		}()
	}
	wg.Wait()
	out := make([]plannedWorktree, len(wts))
	for i, w := range wts {
		out[i] = c.decide(w, gitFacts[i], gitErrs[i], holders[w.Path], procErr, activity(w))
	}
	return out
}

func (c *Cleanup) decide(w domain.Worktree, g WorktreeGitFacts, gitErr error, holders []string, procErr error, act SessionActivity) plannedWorktree {
	f := domain.CleanupFacts{
		InDefault:    g.InDefault,
		OnDefault:    g.OnDefault,
		Uncommitted:  g.Uncommitted,
		Holders:      holders,
		SessionLive:  act.Live,
		LastActivity: latest(g.ModifiedAt, act.LastActivity),
		Project:      act.Project,
	}
	switch {
	case procErr != nil:
		f.Unknown = "process check failed: " + procErr.Error()
	case gitErr != nil:
		f.Unknown = gitErr.Error()
	}
	return plannedWorktree{decision: domain.PlanCleanup(w, f, c.now()), fingerprint: g.Fingerprint}
}

func (c *Cleanup) recheck(ctx context.Context, planned []plannedWorktree, holders map[string][]string, procErr error, activity func(domain.Worktree) SessionActivity) []string {
	stale := make([]string, len(planned))
	if procErr != nil {
		return stale
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, refreshParallelism)
	for i, p := range planned {
		w := p.decision.Worktree
		if p.decision.Action != domain.CleanupRemove || len(holders[w.Path]) > 0 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			stale[i] = c.stillRemovable(ctx, p, activity(w))
		}()
	}
	wg.Wait()
	return stale
}

func (c *Cleanup) stillRemovable(ctx context.Context, p plannedWorktree, act SessionActivity) string {
	w := p.decision.Worktree
	g, err := c.git.CleanupFacts(ctx, w)
	if err != nil {
		return err.Error()
	}
	fresh := c.decide(w, g, nil, nil, nil, act)
	if fresh.fingerprint != p.fingerprint || fresh.decision.Action != domain.CleanupRemove {
		return "changed since it was planned"
	}
	return ""
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (c *Cleanup) Execute(ctx context.Context, wts []domain.Worktree, activity func(domain.Worktree) SessionActivity) []CleanupResult {
	c.trash.Purge()
	planned := c.plan(ctx, wts, activity)
	results := make([]CleanupResult, len(planned))
	var removals []string
	for i, p := range planned {
		results[i] = CleanupResult{Decision: p.decision, Outcome: "kept"}
		switch p.decision.Action {
		case domain.CleanupBackupThenAsk:
			results[i].Outcome = c.backup(ctx, p)
		case domain.CleanupRemove:
			removals = append(removals, p.decision.Worktree.Path)
		}
	}
	var holders map[string][]string
	var procErr error
	if len(removals) > 0 {
		holders, procErr = c.procs.Holders(ctx, removals)
	}
	stale := c.recheck(ctx, planned, holders, procErr, activity)
	pruned := map[string]bool{}
	var repos []string
	for i, r := range results {
		d := r.Decision
		if d.Action != domain.CleanupRemove {
			continue
		}
		if procErr != nil {
			results[i].Outcome = "kept: process check failed: " + procErr.Error()
			continue
		}
		if h := holders[d.Worktree.Path]; len(h) > 0 {
			results[i].Outcome = "kept: in use by " + h[0]
			continue
		}
		switch why := stale[i]; {
		case why != "":
			results[i].Outcome = "kept: " + why
		default:
			if err := c.trash.Move(d.Worktree.Path); err != nil {
				results[i].Outcome = "failed: " + err.Error()
				break
			}
			results[i].Outcome = "removed"
			if !pruned[d.Worktree.Repo] {
				pruned[d.Worktree.Repo] = true
				repos = append(repos, d.Worktree.Repo)
			}
		}
	}
	for _, repo := range repos {
		_ = c.git.Prune(ctx, repo)
	}
	for _, r := range results {
		if r.Outcome != "kept" && r.Outcome != "already backed up" {
			c.record(r)
		}
	}
	return results
}

func (c *Cleanup) RemoveWorktree(ctx context.Context, w domain.Worktree, activity func(domain.Worktree) SessionActivity, withBackup bool) CleanupResult {
	p := c.plan(ctx, []domain.Worktree{w}, activity)[0]
	switch p.decision.Action {
	case domain.CleanupRemove:
		return c.Execute(ctx, []domain.Worktree{w}, activity)[0]
	case domain.CleanupBackupThenAsk:
		if withBackup {
			return c.backupThenRemove(ctx, p, activity)
		}
		return CleanupResult{Decision: p.decision, Outcome: "kept: " + p.decision.Reason + ", back it up first"}
	default:
		return CleanupResult{Decision: p.decision, Outcome: "kept: " + p.decision.Reason}
	}
}

func (c *Cleanup) backupThenRemove(ctx context.Context, p plannedWorktree, activity func(domain.Worktree) SessionActivity) CleanupResult {
	w := p.decision.Worktree
	res := CleanupResult{Decision: p.decision, Outcome: c.backup(ctx, p)}
	if strings.HasPrefix(res.Outcome, "failed") || strings.Contains(res.Outcome, "branch failed") {
		c.record(res)
		return res
	}
	res.Outcome += c.removeAfterBackup(ctx, p, activity)
	if strings.HasSuffix(res.Outcome, ", removed") {
		_ = c.git.Prune(ctx, w.Repo)
	}
	c.record(res)
	return res
}

func (c *Cleanup) removeAfterBackup(ctx context.Context, p plannedWorktree, activity func(domain.Worktree) SessionActivity) string {
	w := p.decision.Worktree
	holders, err := c.procs.Holders(ctx, []string{w.Path})
	switch {
	case err != nil:
		return ", kept: process check failed: " + err.Error()
	case len(holders[w.Path]) > 0:
		return ", kept: in use by " + holders[w.Path][0]
	}
	g, err := c.git.CleanupFacts(ctx, w)
	if err != nil {
		return ", kept: " + err.Error()
	}
	fresh := c.decide(w, g, nil, nil, nil, activity(w))
	switch {
	case fresh.decision.Action == domain.CleanupKeep:
		return ", kept: " + fresh.decision.Reason
	case fresh.fingerprint != p.fingerprint:
		return ", kept: changed since it was backed up"
	}
	if err := c.trash.Move(w.Path); err != nil {
		return ", failed: " + err.Error()
	}
	return ", removed"
}

func (c *Cleanup) backup(ctx context.Context, p plannedWorktree) string {
	w := p.decision.Worktree
	c.mu.Lock()
	done := p.fingerprint != "" && c.backedUp[w.ID] == p.fingerprint
	c.mu.Unlock()
	if done {
		return "already backed up"
	}
	dir := c.claimBackupDir(w)
	if err := c.git.Backup(ctx, w, dir); err != nil {
		return "failed: " + err.Error()
	}
	outcome := "backed up to " + dir
	if p.decision.BackupBranch != "" {
		name, err := c.git.CreateBranch(ctx, w, p.decision.BackupBranch)
		if err != nil {
			return outcome + ", branch failed: " + err.Error()
		}
		outcome += ", branch " + name
	}
	c.mu.Lock()
	c.backedUp[w.ID] = p.fingerprint
	c.mu.Unlock()
	return outcome
}

func (c *Cleanup) claimBackupDir(w domain.Worktree) string {
	base := filepath.Join(c.backupRoot, c.now().Format("20060102-150405"), filepath.Base(w.Path))
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := base
	for i := 2; c.usedDirs[dir]; i++ {
		dir = fmt.Sprintf("%s-%d", base, i)
	}
	c.usedDirs[dir] = true
	return dir
}

func (c *Cleanup) record(r CleanupResult) {
	d := r.Decision
	c.audit.Record(CleanupRecord{
		At:      c.now(),
		Path:    d.Worktree.Path,
		Branch:  d.Worktree.Branch,
		Action:  d.Action,
		Reason:  d.Reason,
		Outcome: r.Outcome,
	})
}
