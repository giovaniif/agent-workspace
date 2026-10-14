package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestReconcilePanesEndsSessionsWhosePaneIsGone(t *testing.T) {
	a := domain.Session{ID: "a", Pane: "%1", State: domain.StateRunning}
	b := domain.Session{ID: "b", Pane: "%2", State: domain.StateWaiting}
	ended := domain.Session{ID: "c", State: domain.StateIdle}
	tests := []struct {
		name  string
		panes []app.PaneInfo
		want  []domain.Session
	}{
		{"pane missing", []app.PaneInfo{{ID: "%1", Alive: true}}, []domain.Session{b.End()}},
		{"pane present but dead", []app.PaneInfo{{ID: "%1", Alive: false}, {ID: "%2", Alive: true}}, []domain.Session{a.End()}},
		{"all panes alive", []app.PaneInfo{{ID: "%1", Alive: true}, {ID: "%2", Alive: true}}, nil},
		{"tmux server gone", nil, []domain.Session{a.End(), b.End()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := app.ReconcilePanes(context.Background(), &fakeHost{panes: tt.panes}, []domain.Session{a, b, ended})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ended %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReconcilePanesLeavesSessionsAloneWhenHostUnreadable(t *testing.T) {
	a := domain.Session{ID: "a", Pane: "%1", State: domain.StateRunning}
	got, err := app.ReconcilePanes(context.Background(), &fakeHost{listErr: errors.New("boom")}, []domain.Session{a})
	if err == nil || got != nil {
		t.Fatalf("ended %v, err %v; want nothing and an error", got, err)
	}
}
