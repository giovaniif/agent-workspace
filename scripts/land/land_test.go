package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T, gh *fakeGH, mode string) *env {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "skills", "verify"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, ".claude", "skills", "verify", "SKILL.md"), []byte("x"), 0o644)
	_ = os.MkdirAll(filepath.Join(repo, "scripts", "land"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, "scripts", "land", "config.json"), []byte(`{"mode":"`+mode+`","repo":"giovaniif/crew"}`), 0o644)
	return &env{sys: gh, repo: repo}
}

func TestCheckLandsAGreenVerifiedPR(t *testing.T) {
	got, code := runJSON(t, testEnv(t, greenPR(), "shadow"), "check", "7")
	if code != 0 || got["landable"] != true {
		t.Fatalf("exit %d landable %v:\n%s", code, got["landable"], blockers(got))
	}
	if got["head"] != headSHA {
		t.Errorf("head %v", got["head"])
	}
}

func TestCheckBlockers(t *testing.T) {
	tests := []struct {
		name string
		edit func(g *fakeGH)
		want string
	}{
		{"draft", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7"] = strings.Replace(g.api["repos/giovaniif/crew/pulls/7"], `"draft":false`, `"draft":true`, 1)
		}, "draft"},
		{"no verdict", func(g *fakeGH) { g.api["repos/giovaniif/crew/issues/7/comments"] = `[]` }, "verdict"},
		{"old verdict", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/issues/7/comments"] = `[{"body":"<!-- land:verdict v=1 sha=bbbb verdict=PASS -->"}]`
		}, "older head"},
		{"fail verdict", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/issues/7/comments"] = `[{"body":"<!-- land:verdict v=1 sha=` + headSHA + ` verdict=HUMAN -->"}]`
		}, "HUMAN"},
		{"red check", func(g *fakeGH) {
			k := "repos/giovaniif/crew/commits/" + headSHA + "/check-runs"
			g.api[k] = strings.Replace(g.api[k], `"name":"tdd","status":"completed","conclusion":"success"`, `"name":"tdd","status":"completed","conclusion":"failure"`, 1)
		}, "tdd"},
		{"missing check", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/commits/"+headSHA+"/status"] = `{"statuses":[]}`
		}, "CodeRabbit"},
		{"unreadable protection", func(g *fakeGH) { delete(g.api, "repos/giovaniif/crew/branches/main/protection") }, "required checks"},
		{"open thread", func(g *fakeGH) {
			g.api["graphql"] = strings.Replace(g.api["graphql"], `"isResolved":true`, `"isResolved":false`, 1)
		}, "thread"},
		{"thread state unknown", func(g *fakeGH) { g.api["graphql"] = `oops` }, "thread"},
		{"changes requested", func(g *fakeGH) {
			g.api["graphql"] = strings.Replace(g.api["graphql"], `"reviewDecision":""`, `"reviewDecision":"CHANGES_REQUESTED"`, 1)
		}, "changes requested"},
		{"not clean", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7"] = strings.Replace(g.api["repos/giovaniif/crew/pulls/7"], `"clean"`, `"behind"`, 1)
		}, "behind"},
		{"no patch", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/daemon/big.go","status":"modified"}]`
		}, "no patch"},
		{"ci change", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":".github/workflows/ci.yml","status":"modified","patch":"+x"}]`
		}, "self-or-CI"},
		{"gate change", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"scripts/land/main.go","status":"modified","patch":"+x"}]`
		}, "self-or-CI"},
		{"skill change", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":".claude/skills/babysit/SKILL.md","status":"modified","patch":"+x"}]`
		}, "self-or-CI"},
		{"split drop", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/adapters/sqlite/migrations/0009_x.sql","status":"added","patch":"@@ -0,0 +2 @@\n+ALTER TABLE sessions\n+  DROP COLUMN name;"}]`
		}, "irreversible"},
		{"rename table", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/adapters/sqlite/migrations/0009_x.sql","status":"added","patch":"+alter table a rename to b;"}]`
		}, "irreversible"},
		{"goreleaser", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":".goreleaser.yaml","status":"modified","patch":"+x"}]`
		}, "irreversible"},
		{"install script", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"scripts/install.sh","status":"modified","patch":"+x"}]`
		}, "irreversible"},
		{"setup code", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/adapters/claude/setup.go","status":"modified","patch":"+x"}]`
		}, "irreversible"},
		{"cleanup code", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/app/cleanup.go","status":"modified","patch":"+x"}]`
		}, "irreversible"},
		{"env file", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"deploy/.env.prod","status":"added","patch":"+X=1"}]`
		}, "irreversible"},
		{"deleted migration", func(g *fakeGH) {
			g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/adapters/sqlite/migrations/0001_init.sql","status":"removed","patch":"-x"}]`
		}, "irreversible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := greenPR()
			tt.edit(g)
			got, code := runJSON(t, testEnv(t, g, "shadow"), "check", "7")
			if code != 1 || got["landable"] != false {
				t.Fatalf("exit %d landable %v", code, got["landable"])
			}
			if !strings.Contains(blockers(got), tt.want) {
				t.Errorf("blockers do not name %q:\n%s", tt.want, blockers(got))
			}
		})
	}
}

func TestCheckIgnoresSafeChangesAndTests(t *testing.T) {
	g := greenPR()
	g.api["repos/giovaniif/crew/pulls/7/files"] = `[{"filename":"internal/app/cleanup_test.go","status":"modified","patch":"+x"},{"filename":"internal/adapters/sqlite/migrations/0009_x.sql","status":"added","patch":"+CREATE TABLE t (id TEXT);\n+CREATE INDEX i ON t(id);"}]`
	got, code := runJSON(t, testEnv(t, g, "shadow"), "check", "7")
	if code != 0 {
		t.Errorf("exit %d:\n%s", code, blockers(got))
	}
}

func TestCheckNeedsTheVerifySkill(t *testing.T) {
	e := testEnv(t, greenPR(), "shadow")
	_ = os.RemoveAll(filepath.Join(e.repo, ".claude"))
	got, code := runJSON(t, e, "check", "7")
	if code != 1 || !strings.Contains(blockers(got), "skill") {
		t.Errorf("exit %d:\n%s", code, blockers(got))
	}
}

func TestVerdictPassNeedsAVerifyingBundleAtTheHead(t *testing.T) {
	g := greenPR()
	g.verifyOK = false
	e := testEnv(t, g, "shadow")
	got, code := runJSON(t, e, "verdict", "7", "--verdict", "PASS", "--bundle", "/tmp/ev")
	if code != 1 || len(g.posted) != 0 {
		t.Fatalf("exit %d posted %v:\n%s", code, g.posted, blockers(got))
	}
	g.verifyOK = true
	got, code = runJSON(t, e, "verdict", "7", "--verdict", "PASS", "--bundle", "/tmp/ev", "--notes", "checked both viewports")
	if code != 0 || len(g.posted) != 1 {
		t.Fatalf("exit %d posted %v:\n%s", code, g.posted, blockers(got))
	}
	if !strings.Contains(g.posted[0], "<!-- land:verdict v=1 sha="+headSHA+" verdict=PASS -->") {
		t.Errorf("marker missing: %q", g.posted[0])
	}
}

func TestVerdictPassRejectsABundleForAnotherHead(t *testing.T) {
	g := greenPR()
	g.api["head"] = "cccc"
	got, code := runJSON(t, testEnv(t, g, "shadow"), "verdict", "7", "--verdict", "PASS", "--bundle", "/tmp/ev")
	if code != 1 || !strings.Contains(blockers(got), "head") || len(g.posted) != 0 {
		t.Errorf("exit %d:\n%s", code, blockers(got))
	}
}

func TestVerdictDryRunAndHumanNeedNoBundle(t *testing.T) {
	g := greenPR()
	got, code := runJSON(t, testEnv(t, g, "shadow"), "verdict", "7", "--verdict", "HUMAN", "--notes", "touches cleanup", "--dry-run")
	if code != 0 || len(g.posted) != 0 {
		t.Fatalf("exit %d posted %v", code, g.posted)
	}
	if !strings.Contains(got["comment"].(string), "verdict=HUMAN") {
		t.Errorf("comment %v", got["comment"])
	}
	if _, code := runJSON(t, testEnv(t, g, "shadow"), "verdict", "7", "--verdict", "MAYBE"); code != 2 {
		t.Errorf("bad verdict exit %d", code)
	}
}

func TestMergeInShadowOnlyWritesTheLedger(t *testing.T) {
	g := greenPR()
	got, code := runJSON(t, testEnv(t, g, "shadow"), "merge", "7")
	if code != 0 || len(g.merged) != 0 {
		t.Fatalf("exit %d merged %v", code, g.merged)
	}
	if got["action"] != "would-land" || len(g.ledger) != 1 || !strings.Contains(g.ledger[0], `"would-land"`) {
		t.Errorf("action %v ledger %v", got["action"], g.ledger)
	}
}

func TestMergeLivePinsTheHeadAndUsesAMergeCommit(t *testing.T) {
	g := greenPR()
	runJSON(t, testEnv(t, g, "live"), "merge", "7")
	if len(g.merged) != 1 || !strings.Contains(g.merged[0], "--merge") || !strings.Contains(g.merged[0], "--match-head-commit "+headSHA) {
		t.Errorf("merged %v", g.merged)
	}
}

func TestMergeBlockedNeverMerges(t *testing.T) {
	g := greenPR()
	g.api["repos/giovaniif/crew/issues/7/comments"] = `[]`
	got, code := runJSON(t, testEnv(t, g, "live"), "merge", "7")
	if code != 1 || len(g.merged) != 0 || got["action"] != "blocked" || len(g.ledger) != 1 {
		t.Errorf("exit %d merged %v action %v ledger %v", code, g.merged, got["action"], g.ledger)
	}
}

func TestMergeOffDoesNothing(t *testing.T) {
	g := greenPR()
	got, _ := runJSON(t, testEnv(t, g, "off"), "merge", "7")
	if got["action"] != "off" || len(g.merged) != 0 || len(g.ledger) != 0 {
		t.Errorf("action %v", got["action"])
	}
}

func TestWatchReportsARedRunOnTheMergeCommit(t *testing.T) {
	g := greenPR()
	g.api["repos/giovaniif/crew/pulls/7"] = `{"number":7,"merged":true,"merge_commit_sha":"mmmm","head":{"sha":"` + headSHA + `"},"base":{"ref":"main"}}`
	g.api["repos/giovaniif/crew/commits/mmmm/check-runs"] = `{"check_runs":[{"name":"build","status":"completed","conclusion":"failure","html_url":"u"}]}`
	got, code := runJSON(t, testEnv(t, g, "shadow"), "watch", "7")
	if code != 1 || !strings.Contains(blockers(got), "build") || got["regression"] != true {
		t.Errorf("exit %d:\n%s", code, blockers(got))
	}
}

func TestLedgerListsDecisions(t *testing.T) {
	g := greenPR()
	g.api["repos/giovaniif/crew/issues/900/comments"] = `[{"body":"{\"pr\":7,\"action\":\"would-land\"}"},{"body":"{\"pr\":8,\"action\":\"blocked\"}"}]`
	got, code := runJSON(t, testEnv(t, g, "shadow"), "ledger", "7")
	if code != 0 || len(got["entries"].([]any)) != 1 {
		t.Errorf("exit %d entries %v", code, got["entries"])
	}
}

func TestUsage(t *testing.T) {
	for _, a := range [][]string{nil, {"nope"}, {"check"}, {"check", "x"}} {
		if _, code := runJSON(t, testEnv(t, greenPR(), "shadow"), a...); code != 2 {
			t.Errorf("%v exit %d", a, code)
		}
	}
}
