package app

import (
	"context"
	"errors"

	"github.com/giovaniif/crew/internal/domain"
)

type TitleResolver interface {
	Title(ctx context.Context, task domain.Task) (string, error)
}

type TitleResolvers []TitleResolver

func (rs TitleResolvers) Title(ctx context.Context, task domain.Task) (string, error) {
	var errs []error
	for _, r := range rs {
		title, err := r.Title(ctx, task)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if title != "" {
			return title, nil
		}
	}
	return "", errors.Join(errs...)
}
