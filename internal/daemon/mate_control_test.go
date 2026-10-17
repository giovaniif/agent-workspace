package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func boardOf(t *testing.T, c *rpc.Client) domain.MateBoard {
	t.Helper()
	sub, err := c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return sub.State.Mates
}

func boardState(b domain.MateBoard, name string) domain.MateStatus {
	for _, m := range b.Mates {
		if m.Name == name {
			return m
		}
	}
	return domain.MateStatus{}
}

func TestMateStopAllStopsNewTurnsOfEveryMateAndShowsInTheState(t *testing.T) {
	p := startMatePair(t, "")
	var limits domain.MateLimits
	if err := p.c.Call(context.Background(), rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: true}, &limits); err != nil {
		t.Fatal(err)
	}
	if !limits.Stop {
		t.Fatalf("limits %+v", limits)
	}
	waitFor(t, func() bool {
		b := boardOf(t, p.c)
		return b.Stopped && boardState(b, "helper").Guard == domain.GuardStopped && boardState(b, "webby").Guard == domain.GuardStopped
	})
	p.hub.send(p.helper, mention(mateOwner, "<@U0BOTHELP1> look", "1700.0001"))
	p.hub.send(p.webby, mention(mateOwner, "<@U0BOTWEB01> look", "1700.0002"))
	waitFor(t, func() bool {
		return guardPosts(p.hub.postsOf(p.helper), "All mates are stopped") == 1 && guardPosts(p.hub.postsOf(p.webby), "All mates are stopped") == 1
	})
	if s := p.mateSessions(t); len(s) != 0 {
		t.Fatalf("sessions started while stopped: %+v", s)
	}
	if err := p.c.Call(context.Background(), rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: false}, &limits); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !boardOf(t, p.c).Stopped })
	p.hub.send(p.helper, mention(mateOwner, "<@U0BOTHELP1> look again", "1700.0003"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
}

func TestMateStopAllSendsTheBoardToSubscribers(t *testing.T) {
	p := startMatePair(t, "")
	sub, err := p.c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.c.Call(context.Background(), rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: true}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case d := <-sub.Diffs:
			if d.Mates != nil && d.Mates.Stopped {
				return
			}
		case <-deadline:
			t.Fatal("no mates diff with stopped")
		}
	}
}

func TestMatePauseDisablesOneMateAndResumeConnectsItAgain(t *testing.T) {
	p := startMatePair(t, "")
	var m domain.Mate
	if err := p.c.Call(context.Background(), rpc.MethodMatePause, rpc.MateNameParams{Name: "helper"}, &m); err != nil {
		t.Fatal(err)
	}
	if m.Enabled {
		t.Fatalf("paused mate %+v", m)
	}
	waitFor(t, func() bool { return boardState(boardOf(t, p.c), "helper").State == domain.MatePaused })
	if s := boardState(boardOf(t, p.c), "webby"); s.State != domain.MateConnected {
		t.Fatalf("webby %+v", s)
	}
	var shown domain.Mate
	if err := p.c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{Name: "helper"}, &shown); err != nil || shown.Enabled {
		t.Fatalf("show %+v %v", shown, err)
	}
	if err := p.c.Call(context.Background(), rpc.MethodMateResume, rpc.MateNameParams{Name: "helper"}, &m); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return boardState(boardOf(t, p.c), "helper").State == domain.MateConnected })
}

func TestMatePauseOfAnUnknownMateIsNotFound(t *testing.T) {
	p := startMatePair(t, "")
	err := p.c.Call(context.Background(), rpc.MethodMatePause, rpc.MateNameParams{Name: "ghost"}, nil)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("err %v", err)
	}
}

func TestMateThreadsAreInTheStateWithTheirSessionAndSlackLink(t *testing.T) {
	p := startMatePair(t, "")
	p.hub.send(p.helper, mention(mateGuest, "<@U0BOTHELP1> look", "1700.0001"))
	waitFor(t, func() bool { return p.linked(t, "helper") })
	sub, err := p.c.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.State.MateThreads) != 1 {
		t.Fatalf("threads %+v", sub.State.MateThreads)
	}
	l := sub.State.MateThreads[0]
	sessions := p.mateSessions(t)
	if l.Mate != "helper" || len(sessions) != 1 || l.SessionID != sessions[0].ID || l.URL != "https://app.slack.com/client/T0TEAM001/C0CHAN001/thread/C0CHAN001-1700.0001" {
		t.Fatalf("thread %+v sessions %+v", l, sessions)
	}
}

func TestMateSetupGivesTheManifestAndItsLink(t *testing.T) {
	p := startMatePair(t, "")
	var setup rpc.MateSetup
	if err := p.c.Call(context.Background(), rpc.MethodMateSetup, rpc.MateShowParams{Name: "helper"}, &setup); err != nil {
		t.Fatal(err)
	}
	var m domain.Mate
	if err := p.c.Call(context.Background(), rpc.MethodMateShow, rpc.MateShowParams{Name: "helper"}, &m); err != nil {
		t.Fatal(err)
	}
	if setup.Manifest != domain.MateManifest(m) || setup.URL != domain.MateManifestURL(setup.Manifest) {
		t.Fatalf("setup %+v", setup)
	}
	err := p.c.Call(context.Background(), rpc.MethodMateSetup, rpc.MateShowParams{Name: "ghost"}, &setup)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != rpc.CodeNotFound {
		t.Fatalf("err %v", err)
	}
}
