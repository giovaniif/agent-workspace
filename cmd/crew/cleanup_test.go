package main

import (
	"bytes"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

func TestCleanupPrintPlan(t *testing.T) {
	items := []rpc.CleanupItem{
		{Path: "/w/api-a", Branch: "a", Action: domain.CleanupRemove, Reason: "PR #1 merged"},
		{Path: "/w/api-b", Branch: "b", Action: domain.CleanupBackupThenAsk, Reason: "2 uncommitted changes"},
		{Path: "/w/api-x", Action: domain.CleanupKeep, Reason: "not merged"},
		{Path: "/w/web-c", Branch: "c", Action: domain.CleanupKeep, Reason: "in use by zsh (pid 8)"},
	}
	var out bytes.Buffer
	printCleanup(&out, items)
	want := "remove           /w/api-a  a  PR #1 merged\n" +
		"backup_then_ask  /w/api-b  b  2 uncommitted changes\n" +
		"keep             /w/api-x  -  not merged\n" +
		"keep             /w/web-c  c  in use by zsh (pid 8)\n" +
		"1 to remove, 1 to back up and ask, 2 kept\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestCleanupPrintRunShowsOutcomes(t *testing.T) {
	items := []rpc.CleanupItem{
		{Path: "/w/api-a", Branch: "a", Action: domain.CleanupRemove, Reason: "PR #1 merged", Outcome: "removed"},
		{Path: "/w/api-x", Action: domain.CleanupKeep, Reason: "not merged", Outcome: "kept"},
	}
	var out bytes.Buffer
	printCleanup(&out, items)
	want := "remove  /w/api-a  a  PR #1 merged  removed\n" +
		"keep    /w/api-x  -  not merged    kept\n" +
		"1 to remove, 0 to back up and ask, 1 kept\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestCleanupUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCleanup([]string{"--bogus"}, &out, &errOut); code != 2 {
		t.Errorf("exit = %d, want 2; stderr %q", code, errOut.String())
	}
}
