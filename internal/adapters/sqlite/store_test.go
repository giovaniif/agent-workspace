package sqlite

import (
	"bufio"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func reopen(t *testing.T, s *Store, path string) *Store {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestRoundTripsEveryDomainType(t *testing.T) {
	s, path := openTemp(t)
	ws := domain.Workspace{Root: "/src/shop", Kind: domain.WorkspaceOrchestration, Repos: []domain.Repo{{Name: "api", Path: "/src/shop/api"}, {Name: "web", Path: "/src/shop/web"}}}
	task := domain.Task{ID: "t1", Source: domain.TaskLinear, Ref: "#42", Text: "fix login", IssueTitle: "Login fails", PinnedName: "login"}
	wt := domain.Worktree{ID: "w1", Repo: "api", Path: "/wt/api-42", Branch: "42-login", PR: &domain.PullRequest{Number: 7, Title: "fix login", URL: "https://example.com/pr/7"}, SubtaskSlug: "api"}
	wtNoPR := domain.Worktree{ID: "w2", Repo: "web", Path: "/wt/web-42", Branch: "42-web"}
	sess := domain.Session{ID: "s1", TaskID: "t1", Harness: domain.HarnessCodex, Model: "m", Effort: "high", Transcript: "/home/dev/.codex/sessions/rollout-1.jsonl", State: domain.StatePermission, Unread: true, Focused: true, Muted: true, WorktreeIDs: []string{"w1", "w2"}, Usage: domain.Usage{ContextLeftPercent: 40, LimitUsedPercent: 12}}

	s.PutWorkspace(ws)
	s.PutTask(task)
	s.PutWorktree(wt)
	s.PutWorktree(wtNoPR)
	s.PutSession(sess)

	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Workspaces, []domain.Workspace{ws}) {
		t.Errorf("workspaces = %+v", snap.Workspaces)
	}
	if !reflect.DeepEqual(snap.Tasks, []domain.Task{task}) {
		t.Errorf("tasks = %+v", snap.Tasks)
	}
	if !reflect.DeepEqual(snap.Worktrees, []domain.Worktree{wt, wtNoPR}) {
		t.Errorf("worktrees = %+v", snap.Worktrees)
	}
	if !reflect.DeepEqual(snap.Sessions, []domain.Session{sess}) {
		t.Errorf("sessions = %+v", snap.Sessions)
	}
}

func TestSessionCreationChoicesSurviveARestartAndOlderRowsHaveNone(t *testing.T) {
	s, path := openTemp(t)
	created := domain.Session{ID: "s1", Harness: domain.HarnessOmp, Model: "live", StartedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), StartModel: "anthropic/opus", StartEffort: "off", StartWorkspace: "/src/api"}
	s.PutSession(created)
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Sessions, []domain.Session{created}) {
		t.Errorf("sessions = %+v", snap.Sessions)
	}

	src, err := os.ReadFile("testdata/v1.db")
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(old, src, 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := Open(old)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = o.Close() }()
	osnap, err := o.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(osnap.Sessions) != 1 || !osnap.Sessions[0].StartedAt.IsZero() || osnap.Sessions[0].StartModel != "" || osnap.Sessions[0].StartWorkspace != "" {
		t.Errorf("older session = %+v, want no creation choices", osnap.Sessions)
	}
}

func TestLaterPutReplacesEarlier(t *testing.T) {
	s, _ := openTemp(t)
	s.PutSession(domain.Session{ID: "s1", State: domain.StateRunning})
	s.PutSession(domain.Session{ID: "s1", State: domain.StateDone})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].State != domain.StateDone {
		t.Errorf("sessions = %+v", snap.Sessions)
	}
}

func TestDeleteWorkspaceRemovesTheRowAndBeatsAnUnflushedPut(t *testing.T) {
	s, path := openTemp(t)
	s.PutWorkspace(domain.Workspace{Root: "/a"})
	s.PutWorkspace(domain.Workspace{Root: "/b"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	s.DeleteWorkspace("/a")
	s.PutWorkspace(domain.Workspace{Root: "/c"})
	s.DeleteWorkspace("/c")
	s.DeleteWorkspace("/b")
	s.PutWorkspace(domain.Workspace{Root: "/b", Kind: domain.WorkspaceSingle})

	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Workspaces) != 1 || snap.Workspaces[0].Root != "/b" || snap.Workspaces[0].Kind != domain.WorkspaceSingle {
		t.Errorf("workspaces = %+v, want only /b", snap.Workspaces)
	}
}

func TestWorktreeDetectDeleteWorktreeRemovesTheRow(t *testing.T) {
	s, path := openTemp(t)
	s.PutWorktree(domain.Worktree{ID: "/a"})
	s.PutWorktree(domain.Worktree{ID: "/b"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	s.DeleteWorktree("/a")
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Worktrees) != 1 || snap.Worktrees[0].ID != "/b" {
		t.Errorf("worktrees = %+v, want only /b", snap.Worktrees)
	}
}

func TestDeleteSessionRemovesTheRowAndItsEvents(t *testing.T) {
	s, path := openTemp(t)
	s.PutSession(domain.Session{ID: "gone"})
	s.PutSession(domain.Session{ID: "kept"})
	s.PutEvent(domain.SessionEvent{SessionID: "gone", Detail: "flushed"})
	s.PutEvent(domain.SessionEvent{SessionID: "kept", Detail: "kept"})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	s.PutEvent(domain.SessionEvent{SessionID: "gone", Detail: "unflushed"})
	s.DeleteSession("gone")
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != "kept" {
		t.Errorf("sessions = %+v, want only kept", snap.Sessions)
	}
	if len(snap.Events) != 1 || snap.Events[0].SessionID != "kept" {
		t.Errorf("events = %+v, want only kept's", snap.Events)
	}
}

func TestWritesReachDiskWithoutExplicitFlush(t *testing.T) {
	s, path := openTemp(t)
	s.PutTask(domain.Task{ID: "t1"})
	time.Sleep(3 * FlushInterval)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("tasks on disk = %d, want 1", n)
	}
}

func TestOpenUpgradesOlderVersion(t *testing.T) {
	src, err := os.ReadFile("testdata/v1.db")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if v, err := s.Version(); err != nil || v != LatestVersion() || v < 2 {
		t.Fatalf("version = %d, %v; want %d", v, err, LatestVersion())
	}
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != "s-old" {
		t.Errorf("sessions from v1 = %+v", snap.Sessions)
	}
	s.PutWorktree(domain.Worktree{ID: "w1"})
	if err := s.Flush(); err != nil {
		t.Fatalf("v2 table not usable after upgrade: %v", err)
	}
}

func TestReopeningLatestKeepsVersion(t *testing.T) {
	s, path := openTemp(t)
	r := reopen(t, s, path)
	if v, err := r.Version(); err != nil || v != LatestVersion() {
		t.Fatalf("version = %d, %v", v, err)
	}
}

func TestDefaultPathHonoursAgentwsHome(t *testing.T) {
	t.Setenv("AGENTWS_HOME", "/tmp/aw")
	if p, _ := DefaultPath(); p != "/tmp/aw/state.db" {
		t.Errorf("path = %q", p)
	}
	t.Setenv("AGENTWS_HOME", "")
	t.Setenv("HOME", "/home/u")
	if p, _ := DefaultPath(); p != "/home/u/.agentws/state.db" {
		t.Errorf("path = %q", p)
	}
}

const writeLoopEnv = "AGENTWS_SQLITE_WRITE_LOOP"

func TestMain(m *testing.M) {
	if path := os.Getenv(writeLoopEnv); path != "" {
		runWriteLoop(path)
		return
	}
	os.Exit(m.Run())
}

func runWriteLoop(path string) {
	s, err := Open(path)
	if err != nil {
		os.Exit(3)
	}
	for i := 1; ; i++ {
		s.PutSession(domain.Session{ID: "s1", Model: strconv.Itoa(i)})
		_, _ = os.Stdout.WriteString(strconv.Itoa(i) + "\n")
		time.Sleep(time.Millisecond)
	}
}

func TestKilledProcessLosesAtMostLast100ms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), writeLoopEnv+"="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	type mark struct {
		seq int
		at  time.Time
	}
	marks := make(chan mark, 1<<16)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			n, _ := strconv.Atoi(sc.Text())
			marks <- mark{n, time.Now()}
		}
		close(marks)
	}()

	time.Sleep(700 * time.Millisecond)
	killedAt := time.Now()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	mustHave := 0
	for m := range marks {
		if m.at.Before(killedAt.Add(-100 * time.Millisecond)) {
			mustHave = m.seq
		}
	}
	_ = cmd.Wait()
	if mustHave == 0 {
		t.Fatal("write loop produced nothing")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity_check = %q, %v", check, err)
	}
	_ = db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	snap, err := s.Load()
	if err != nil || len(snap.Sessions) != 1 {
		t.Fatalf("sessions = %+v, %v", snap.Sessions, err)
	}
	got, _ := strconv.Atoi(snap.Sessions[0].Model)
	if got < mustHave {
		t.Errorf("persisted seq %d, want >= %d (written more than 100 ms before kill)", got, mustHave)
	}
}

func BenchmarkFlush1000SessionUpdates(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "state.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for b.Loop() {
		start := time.Now()
		for i := range 1000 {
			s.PutSession(domain.Session{ID: "s" + strconv.Itoa(i), State: domain.StateRunning, Model: strconv.Itoa(i)})
		}
		if err := s.Flush(); err != nil {
			b.Fatal(err)
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			b.Fatalf("1000 updates flushed in %v, budget 200ms", d)
		}
	}
}

func TestEventsSurviveARestartInOrder(t *testing.T) {
	s, path := openTemp(t)
	at := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	want := []domain.SessionEvent{
		{SessionID: "s1", At: at, Kind: domain.EventPreToolUse, Tool: "Bash", Detail: "make"},
		{SessionID: "s2", At: at.Add(time.Second), Kind: domain.EventStop, Text: "Done?"},
		{SessionID: "s1", At: at.Add(2 * time.Second), Kind: domain.EventPermissionRequest, Text: "Bash: rm x"},
	}
	for _, ev := range want {
		s.PutEvent(ev)
	}
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Events, want) {
		t.Fatalf("events = %+v, want %+v", snap.Events, want)
	}
}

func TestOnlyTheNewestEventsOfEachSessionAreKept(t *testing.T) {
	s, path := openTemp(t)
	for i := range app.EventsPerSession + 5 {
		s.PutEvent(domain.SessionEvent{SessionID: "busy", Detail: strconv.Itoa(i)})
		if i < 3 {
			s.PutEvent(domain.SessionEvent{SessionID: "quiet", Detail: strconv.Itoa(i)})
		}
	}
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	var busy, quiet []string
	for _, ev := range snap.Events {
		if ev.SessionID == "busy" {
			busy = append(busy, ev.Detail)
		} else {
			quiet = append(quiet, ev.Detail)
		}
	}
	if len(busy) != app.EventsPerSession || busy[0] != "5" || busy[len(busy)-1] != strconv.Itoa(app.EventsPerSession+4) {
		t.Errorf("busy kept %v", busy)
	}
	if !reflect.DeepEqual(quiet, []string{"0", "1", "2"}) {
		t.Errorf("quiet kept %v", quiet)
	}
}

func TestReviewViewedMarksSurviveARestart(t *testing.T) {
	s, path := openTemp(t)
	a := domain.ViewedMark{Worktree: "/wt/api", Path: "a.go", Blob: "111"}
	s.PutViewed(a)
	s.PutViewed(domain.ViewedMark{Worktree: "/wt/api", Path: "b.go", Blob: "222"})
	s.PutViewed(domain.ViewedMark{Worktree: "/wt/api", Path: "b.go", Blob: "333"})
	s.PutViewed(domain.ViewedMark{Worktree: "/wt/web", Path: "c.go", Blob: "444"})
	s.DeleteViewed(domain.ViewedMark{Worktree: "/wt/web", Path: "c.go"}.Key())

	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ViewedMark{a, {Worktree: "/wt/api", Path: "b.go", Blob: "333"}}
	if !reflect.DeepEqual(snap.Viewed, want) {
		t.Errorf("viewed = %+v, want %+v", snap.Viewed, want)
	}
}

func TestReviewDraftsSurviveARestart(t *testing.T) {
	s, path := openTemp(t)
	c := domain.ReviewComment{Worktree: "/wt/api", Path: "a.go", Start: 3, End: 4, Code: []string{"x", "y"}, Body: "why?"}
	open := domain.ReviewDraft{ID: "s1-2", Session: "s1", Status: domain.DraftQueued, Comments: []domain.ReviewComment{c}}
	sent := domain.ReviewDraft{ID: "s1-1", Session: "s1", Status: domain.DraftSent, Comments: []domain.ReviewComment{c}, SentAt: time.Unix(50, 0).UTC(), Turns: []string{"refs/agentws/turns/s1/k/2"}}
	s.PutDraft(domain.ReviewDraft{ID: "s1-2", Session: "s1", Status: domain.DraftOpen})
	s.PutDraft(open)
	s.PutDraft(sent)

	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.ReviewDraft{sent, open}; !reflect.DeepEqual(snap.Drafts, want) {
		t.Errorf("drafts = %+v, want %+v", snap.Drafts, want)
	}
}

func TestReviewASentDraftWithNoTurnStaysClosedAfterARestart(t *testing.T) {
	s, path := openTemp(t)
	s.PutDraft(domain.ReviewDraft{ID: "s1-1", Session: "s1", Status: domain.DraftSent}.LinkTurn(nil))
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Drafts) != 1 || snap.Drafts[0].AwaitsTurn() {
		t.Errorf("drafts = %+v; a draft closed with no turn must not wait for one again", snap.Drafts)
	}
}

func TestDevicesPersistAndRevokedOnesAreDeleted(t *testing.T) {
	s, path := openTemp(t)
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	phone := domain.Device{ID: "a2b3c4d5", Name: "phone", TokenHash: "abc123", Created: at, LastSeen: at.Add(time.Hour)}
	ipad := domain.Device{ID: "z9y8x7w6", Name: "ipad", TokenHash: "def456", Created: at, LastSeen: at}
	s.PutDevice(phone)
	s.PutDevice(ipad)
	s = reopen(t, s, path)
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Devices, []domain.Device{phone, ipad}) {
		t.Fatalf("devices %+v", snap.Devices)
	}
	s.DeleteDevice(phone.ID)
	snap, err = reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Devices, []domain.Device{ipad}) {
		t.Fatalf("after revoke %+v", snap.Devices)
	}
}

func TestDevicePushSubscriptionPersistsWithItsDevice(t *testing.T) {
	s, path := openTemp(t)
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	sub := domain.PushSubscription{Endpoint: "https://web.push.apple.com/QGx3", P256dh: "BNcR", Auth: "tBHI"}
	s.PutDevice(domain.Device{ID: "a2b3c4d5", Name: "phone", TokenHash: "abc123", Created: at, LastSeen: at, Push: &sub})
	snap, err := reopen(t, s, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Devices) != 1 || snap.Devices[0].Push == nil || *snap.Devices[0].Push != sub {
		t.Fatalf("devices %+v", snap.Devices)
	}
}
