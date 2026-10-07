package onboard

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/giovaniif/agent-workspace/internal/adapters/claude"
	"github.com/giovaniif/agent-workspace/internal/adapters/codex"
	"github.com/giovaniif/agent-workspace/internal/adapters/omp"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

func (p Probe) Remove(_ context.Context, h domain.Harness) (domain.HarnessSetup, error) {
	status, remove, err := p.removal(h)
	if err != nil {
		return domain.HarnessSetup{}, err
	}
	if s := status(); !s.Installed {
		if s.Err != "" {
			return domain.HarnessSetup{}, errors.New(s.Err)
		}
		s.Backup = ""
		return s, nil
	}
	backup, err := backupBeforeRemoval(status().File)
	if err != nil {
		return domain.HarnessSetup{}, err
	}
	if err := remove(); err != nil {
		return domain.HarnessSetup{}, err
	}
	s := status()
	s.Backup = backup
	return s, nil
}

func (p Probe) removal(h domain.Harness) (func() domain.HarnessSetup, func() error, error) {
	switch h {
	case domain.HarnessClaude:
		return p.claudeSetup, func() error { return claude.Remove(p.ClaudeSettings) }, nil
	case domain.HarnessCodex:
		return p.codexSetup, func() error { _, err := codex.Remove(p.codexConfig()); return err }, nil
	case domain.HarnessOmp:
		if p.ompDirErr != nil {
			return nil, nil, p.ompDirErr
		}
		return p.ompSetup, func() error { _, err := omp.Remove(p.ompConfig()); return err }, nil
	}
	return nil, nil, errors.New("no setup for harness " + string(h))
}

func backupBeforeRemoval(file string) (string, error) {
	current, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	backup := file + ".agentws-removed-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
	if err := os.WriteFile(backup, current, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}
