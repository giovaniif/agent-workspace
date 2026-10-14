package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestNamingResolversTakeTheFirstAnswer(t *testing.T) {
	task := domain.Task{Source: domain.TaskPR, Ref: "api#1"}
	boom := errors.New("boom")
	cases := []struct {
		name      string
		resolvers app.TitleResolvers
		want      string
		wantErr   error
	}{
		{"first answer wins", app.TitleResolvers{titleFake{title: "One"}, titleFake{title: "Two"}}, "One", nil},
		{"a resolver with no answer is skipped", app.TitleResolvers{titleFake{}, titleFake{title: "Two"}}, "Two", nil},
		{"a failing resolver does not hide a later answer", app.TitleResolvers{titleFake{err: boom}, titleFake{title: "Two"}}, "Two", nil},
		{"every resolver failing reports the error", app.TitleResolvers{titleFake{err: boom}, titleFake{}}, "", boom},
		{"nobody answering is not an error", app.TitleResolvers{titleFake{}, titleFake{}}, "", nil},
		{"no resolvers", nil, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.resolvers.Title(context.Background(), task)
			if got != c.want || !errors.Is(err, c.wantErr) {
				t.Errorf("Title = %q, %v; want %q, %v", got, err, c.want, c.wantErr)
			}
		})
	}
}
