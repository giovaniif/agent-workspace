package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const sessionsArea = "# Sessions\n\nWhat must be true.\n\n## Setup\n\n```sh\nsetup-cmd\n```\n\n## Recipes\n\n### list shows seeded sessions\n\n```sh\ngood-cmd one\ngood-cmd two\n```\n\nExpect: rows.\n\n### broken flow\n\n```sh\nbad-cmd\n```\n\n### needs a token\n\nNeeds: TEST_TOKEN\n\n```sh\ngood-cmd three\n```\n\n### pair a phone camera (manual: no camera on the server)\n\n## Cleanup\n\n```sh\ncleanup-cmd\n```\n\n## Gotchas\n\n- none\n"

func mapEnv(t *testing.T) (*env, *fakeSys, string) {
	t.Helper()
	e, fs := testEnv(t)
	dir := filepath.Join(e.repo, "features")
	write(t, filepath.Join(dir, "sessions.md"), sessionsArea)
	write(t, filepath.Join(dir, "README.md"), "# index\n")
	fs.handlers["sh"] = func(c cmd) (string, string, int) {
		line := c.args[len(c.args)-1]
		if strings.HasPrefix(line, "bad-cmd") {
			return `{"ok":false}`, "", 1
		}
		return `{"ok":true}`, "", 0
	}
	return e, fs, dir
}

func TestMapRunRecordsPassFailNeedsVarAndManual(t *testing.T) {
	e, fs, _ := mapEnv(t)
	got, code := runJSON(t, e, "map", "run", "sessions")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	want := map[string]string{
		"list shows seeded sessions": "pass",
		"broken flow":                "fail",
		"needs a token":              "needs-var",
		"pair a phone camera":        "manual",
	}
	for _, r := range got["recipes"].([]any) {
		m := r.(map[string]any)
		if want[m["label"].(string)] != m["result"] {
			t.Errorf("%v = %v, want %v", m["label"], m["result"], want[m["label"].(string)])
		}
		delete(want, m["label"].(string))
	}
	if len(want) != 0 {
		t.Errorf("missing recipes %v", want)
	}
	if !fs.ran("sh -c setup-cmd") || !fs.ran("sh -c cleanup-cmd") {
		t.Error("setup or cleanup did not run")
	}
	if fs.ran("sh -c good-cmd three") {
		t.Error("ran a recipe whose variable is not set")
	}
}

func TestMapRunOneRecipeAndStatusReadsBack(t *testing.T) {
	e, fs, _ := mapEnv(t)
	fs.vars["TEST_TOKEN"] = "x"
	got, code := runJSON(t, e, "map", "run", "sessions", "--recipe", "needs a token")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	if n := len(got["recipes"].([]any)); n != 1 {
		t.Errorf("%d recipes ran", n)
	}
	st := mustOK(t, e, "map", "status")
	areas := st["areas"].([]any)
	if len(areas) != 1 {
		t.Fatalf("areas %v", areas)
	}
	a := areas[0].(map[string]any)
	if a["area"] != "sessions" || a["recipes"] != float64(4) || a["pass"] != float64(1) {
		t.Errorf("status %v", a)
	}
}

func TestMapRunUnknownAreaOrRecipe(t *testing.T) {
	e, _, _ := mapEnv(t)
	if _, code := runJSON(t, e, "map", "run", "nope"); code != 2 {
		t.Errorf("unknown area exit %d", code)
	}
	if _, code := runJSON(t, e, "map", "run", "sessions", "--recipe", "nope"); code != 2 {
		t.Errorf("unknown recipe exit %d", code)
	}
}

func TestMapCleanupRunsWhenARecipeFails(t *testing.T) {
	e, fs, _ := mapEnv(t)
	runJSON(t, e, "map", "run", "sessions", "--recipe", "broken flow")
	if !fs.ran("sh -c cleanup-cmd") {
		t.Error("cleanup skipped after a failure")
	}
}
