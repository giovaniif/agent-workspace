//go:build integration

package daemon_test

import (
	"context"

	"github.com/giovaniif/crew/internal/domain"
)

type noPRs struct{}

func (noPRs) PRs(context.Context, []string) (map[string][]domain.PullRequest, error) {
	return nil, nil
}
