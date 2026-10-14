package app_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

func TestRecipeSetupWithoutRecipeDoesNothing(t *testing.T) {
	w := newWorld(nil)
	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Lines, []string{"no recipe in /repos/api/.crew.toml"}) || len(w.ops) != 0 {
		t.Errorf("lines = %q, ops = %q", report.Lines, w.ops)
	}
}

func TestRecipeSetupAppliesCopyLinkDepsAndRunInOrder(t *testing.T) {
	w := newWorld(&domain.Recipe{
		Copy: []string{".env.example"},
		Link: []string{".cache"},
		Deps: domain.DepsClone,
		Run:  []string{"echo ready"},
	})
	w.existing["/repos/api/.env.example"] = true
	w.existing["/repos/api/.cache"] = true
	w.existing["/repos/api/node_modules"] = true
	lock := domain.Lockfile{Name: "bun.lock", Hash: "h1"}
	w.locks["/repos/api"], w.locks["/wt/api-1"] = lock, lock

	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	wantOps := []string{
		"copy /repos/api/.env.example /wt/api-1/.env.example",
		"symlink /repos/api/.cache /wt/api-1/.cache",
		"clone /repos/api/node_modules /wt/api-1/node_modules",
		"run sh -c echo ready in /wt/api-1",
	}
	if !reflect.DeepEqual(w.ops, wantOps) {
		t.Errorf("ops = %q, want %q", w.ops, wantOps)
	}
	wantLines := []string{"copy .env.example", "link .cache", "deps clone node_modules", "run echo ready"}
	if !reflect.DeepEqual(report.Lines, wantLines) {
		t.Errorf("lines = %q, want %q", report.Lines, wantLines)
	}
}

func TestRecipeSetupNeverOverwritesOrInventsFiles(t *testing.T) {
	w := newWorld(&domain.Recipe{
		Copy: []string{".env", "missing.json"},
		Link: []string{".cache"},
	})
	w.existing["/repos/api/.env"] = true
	w.existing["/wt/api-1/.env"] = true
	w.existing["/repos/api/.cache"] = true
	w.existing["/wt/api-1/.cache"] = true

	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.ops) != 0 {
		t.Errorf("ops = %q, want none", w.ops)
	}
	wantLines := []string{
		"skip copy .env: already in the worktree",
		"skip copy missing.json: not in the main checkout",
		"skip link .cache: already in the worktree",
	}
	if !reflect.DeepEqual(report.Lines, wantLines) {
		t.Errorf("lines = %q, want %q", report.Lines, wantLines)
	}
}

func TestRecipeSetupLockfileMismatchInstallsAndLogsWhy(t *testing.T) {
	w := newWorld(&domain.Recipe{Deps: domain.DepsClone})
	w.existing["/repos/api/node_modules"] = true
	w.locks["/repos/api"] = domain.Lockfile{Name: "bun.lock", Hash: "old"}
	w.locks["/wt/api-1"] = domain.Lockfile{Name: "bun.lock", Hash: "new"}

	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	wantOps := []string{"run bun install --frozen-lockfile in /wt/api-1"}
	if !reflect.DeepEqual(w.ops, wantOps) {
		t.Errorf("ops = %q, want %q", w.ops, wantOps)
	}
	want := []string{"deps install: bun.lock differs from the main checkout (bun install --frozen-lockfile)"}
	if !reflect.DeepEqual(report.Lines, want) {
		t.Errorf("lines = %q, want %q", report.Lines, want)
	}
}

func TestRecipeSetupLinksAndSkipsDeps(t *testing.T) {
	cases := []struct {
		name     string
		recipe   domain.Recipe
		modules  bool
		wantOps  []string
		wantLine string
	}{
		{
			"link mode symlinks node_modules",
			domain.Recipe{Deps: domain.DepsLink}, true,
			[]string{"symlink /repos/api/node_modules /wt/api-1/node_modules"},
			"deps link node_modules",
		},
		{
			"skipped deps say why",
			domain.Recipe{Deps: domain.DepsLink}, false,
			nil,
			"deps skip: the main checkout has no node_modules",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(&c.recipe)
			w.existing["/repos/api/node_modules"] = c.modules
			report, err := w.setup().Run(context.Background(), "/wt/api-1")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(w.ops, c.wantOps) || !reflect.DeepEqual(report.Lines, []string{c.wantLine}) {
				t.Errorf("ops = %q, lines = %q", w.ops, report.Lines)
			}
		})
	}
}

func TestRecipeSetupDoesNotReplaceExistingModules(t *testing.T) {
	w := newWorld(&domain.Recipe{Deps: domain.DepsClone})
	w.existing["/repos/api/node_modules"] = true
	w.existing["/wt/api-1/node_modules"] = true

	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deps skip: node_modules already in the worktree"}
	if len(w.ops) != 0 || !reflect.DeepEqual(report.Lines, want) {
		t.Errorf("ops = %q, lines = %q", w.ops, report.Lines)
	}
}

func TestRecipeSetupStopsAtFailingRunCommand(t *testing.T) {
	w := newWorld(&domain.Recipe{Run: []string{"first", "boom", "third"}})
	w.runErr = map[string]error{"sh -c boom": errors.New("exit status 3")}

	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v, want it to name the command and its failure", err)
	}
	if len(w.ops) != 2 {
		t.Errorf("ops = %q, want the third command never run", w.ops)
	}
	if want := []string{"run first"}; !reflect.DeepEqual(report.Lines, want) {
		t.Errorf("lines = %q, want %q", report.Lines, want)
	}
}

func TestRecipeSetupRejectsBadRecipeBeforeTouchingAnything(t *testing.T) {
	w := newWorld(&domain.Recipe{Copy: []string{"../../etc/passwd"}, Run: []string{"echo hi"}})
	if _, err := w.setup().Run(context.Background(), "/wt/api-1"); err == nil {
		t.Fatal("want an error for a path outside the repo")
	}
	if len(w.ops) != 0 {
		t.Errorf("ops = %q, want none", w.ops)
	}
}

func TestRecipeSetupRefusesTheMainCheckoutItself(t *testing.T) {
	w := newWorld(&domain.Recipe{Run: []string{"rm -rf node_modules"}})
	if _, err := w.setup().Run(context.Background(), "/repos/api"); err == nil {
		t.Fatal("want an error when the worktree is the main checkout")
	}
	if len(w.ops) != 0 {
		t.Errorf("ops = %q, want none", w.ops)
	}
}

func TestRecipeSetupReportsDurationAndDiskUsed(t *testing.T) {
	w := newWorld(&domain.Recipe{Run: []string{"echo hi"}})
	report, err := w.setup().Run(context.Background(), "/wt/api-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Duration != 1500*time.Millisecond {
		t.Errorf("Duration = %v, want 1.5s", report.Duration)
	}
	if report.DiskUsed != 10_000_000 {
		t.Errorf("DiskUsed = %d, want 10000000", report.DiskUsed)
	}
}
