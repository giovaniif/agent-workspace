package daemon_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/giovaniif/agent-workspace/internal/app"
)

type termFake struct {
	app.TerminalHost
	client *fakeClientHost

	mu      sync.Mutex
	specs   []app.PaneSpec
	alive   map[app.PaneID]bool
	created []app.PaneID
	shown   []app.PaneID
	titles  map[app.PaneID]string
	killed  []app.PaneID
}

func newTermFake(client *fakeClientHost, live ...app.PaneID) *termFake {
	t := &termFake{client: client, alive: map[app.PaneID]bool{}}
	for _, p := range live {
		t.alive[p] = true
	}
	return t
}

func (t *termFake) Create(_ context.Context, spec app.PaneSpec) (app.PaneID, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.specs = append(t.specs, spec)
	id := app.PaneID(fmt.Sprintf("%%t%d", len(t.specs)))
	t.alive[id] = true
	t.created = append(t.created, id)
	return id, nil
}

func (t *termFake) Alive(_ context.Context, pane app.PaneID) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.alive[pane], nil
}

func (t *termFake) List(context.Context) ([]app.PaneInfo, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []app.PaneInfo
	for p, ok := range t.alive {
		out = append(out, app.PaneInfo{ID: p, Alive: ok})
	}
	return out, nil
}

func (t *termFake) Show(_ context.Context, pane app.PaneID, slot app.Slot) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shown = append(t.shown, pane)
	t.client.setShownIn(slot, pane)
	return nil
}

func (t *termFake) kill(pane app.PaneID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alive[pane] = false
}

func (t *termFake) createdSpecs() []app.PaneSpec {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]app.PaneSpec(nil), t.specs...)
}

func (t *termFake) shownPanes() []app.PaneID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]app.PaneID(nil), t.shown...)
}

type fakeEditor struct {
	mu      sync.Mutex
	calls   []string
	missing bool
}

func (e *fakeEditor) Installed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.missing
}

func (e *fakeEditor) setMissing() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.missing = true
}

func (e *fakeEditor) Eval(_ context.Context, socket, expr string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, socket+" "+expr)
	return nil
}

func (e *fakeEditor) evals() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

func (t *termFake) SetTitle(_ context.Context, pane app.PaneID, title string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.titles == nil {
		t.titles = map[app.PaneID]string{}
	}
	t.titles[pane] = title
	return nil
}

func (t *termFake) title(pane app.PaneID) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.titles[pane]
}

func (t *termFake) Kill(_ context.Context, pane app.PaneID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alive[pane] = false
	t.killed = append(t.killed, pane)
	return nil
}

func (t *termFake) killedPanes() []app.PaneID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]app.PaneID(nil), t.killed...)
}
