package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

func TestReviewApplyHunkStagesOrRevertsItsPatch(t *testing.T) {
	f := domain.ParseDiff(oneFileDiff)[0]
	patch, _ := domain.HunkPatch(f, f.Hunks[0])
	g := &fakeHunkGit{}
	ctx := context.Background()
	if err := app.ApplyHunk(ctx, g, "/wt", f, f.Hunks[0], domain.HunkStage); err != nil {
		t.Fatal(err)
	}
	if len(g.staged) != 1 || len(g.reverted) != 0 {
		t.Fatalf("stage: staged %q reverted %q", g.staged, g.reverted)
	}
	if err := app.ApplyHunk(ctx, g, "/wt", f, f.Hunks[0], domain.HunkRevert); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/wt\n" + patch}; !reflect.DeepEqual(g.staged, want) || !reflect.DeepEqual(g.reverted, want) {
		t.Errorf("staged %q reverted %q", g.staged, g.reverted)
	}
}

func TestReviewApplyHunkRefusesUnknownActionsAndUnsupportedFiles(t *testing.T) {
	f := domain.ParseDiff(oneFileDiff)[0]
	g := &fakeHunkGit{}
	ctx := context.Background()
	if err := app.ApplyHunk(ctx, g, "/wt", f, f.Hunks[0], "drop"); err == nil {
		t.Error("an unknown action was applied")
	}
	bin := domain.FileDiff{Path: "i.png", Binary: true}
	if err := app.ApplyHunk(ctx, g, "/wt", bin, domain.Hunk{}, domain.HunkRevert); !errors.Is(err, domain.ErrHunkUnsupported) {
		t.Errorf("binary: %v", err)
	}
	if len(g.staged)+len(g.reverted) != 0 {
		t.Errorf("git was asked: %q %q", g.staged, g.reverted)
	}
}

func TestReviewSendPastesThePromptThenPressesEnter(t *testing.T) {
	host := &typingHost{}
	if err := app.SendPrompt(context.Background(), host, "%4", "line one\nline two", 0); err != nil {
		t.Fatal(err)
	}
	if want := []string{"%4 paste=true line one\nline two", "%4 keys Enter"}; !reflect.DeepEqual(host.typed, want) {
		t.Fatalf("typed %q", host.typed)
	}
	failing := &typingHost{failOn: "p", failErr: errors.New("pane gone")}
	if err := app.SendPrompt(context.Background(), failing, "%4", "p", 0); err == nil || len(failing.typed) != 0 {
		t.Fatalf("err %v typed %q", err, failing.typed)
	}
}
