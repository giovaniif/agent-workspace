package app_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"
)

const transcriptPath = "/t/s1.jsonl"

const transcriptBody = "u p1 fix the test\n" +
	"a a1 reading\n" +
	"call c1 go test\n" +
	"a a2 waiting\n" +
	"ok c1 PASS\n" +
	"a2 a3 done\n" +
	"u p2 next\n" +
	"call c2 ls\n"

func transcripts(files *memFiles, span, reach int64) app.Transcripts {
	return app.Transcripts{Files: files, Parsers: lineParsers(), Span: span, Reach: reach}
}

func ids(msgs []domain.Message) []string {
	var out []string
	for _, m := range msgs {
		s := m.ID
		if m.Tool != nil {
			s += "=" + string(m.Tool.Status)
		}
		out = append(out, s)
	}
	return out
}

func lineEnd(body, line string) int64 {
	return int64(strings.Index(body, line) + len(line))
}

func allPages(t *testing.T, tr app.Transcripts, limit int) [][]domain.Message {
	t.Helper()
	var pages [][]domain.Message
	var before int64
	for i := 0; i < 50; i++ {
		page, err := tr.Page(domain.HarnessClaude, transcriptPath, before, limit)
		if err != nil {
			t.Fatal(err)
		}
		pages = append([][]domain.Message{page.Messages}, pages...)
		if page.Before == 0 {
			return pages
		}
		if before != 0 && page.Before >= before {
			t.Fatalf("page before %d did not move back from %d", page.Before, before)
		}
		before = page.Before
	}
	t.Fatal("paging never reached the first message")
	return nil
}

func TestTranscriptPageWalksBackToTheFirstMessageWithoutGapsOrDuplicates(t *testing.T) {
	want := []string{"p1", "a1", "c1=done", "a2", "a3", "a3:1", "p2", "c2=running"}
	for _, limit := range []int{1, 2, 3, 100} {
		for _, span := range []int64{4, 1 << 20} {
			pages := allPages(t, transcripts(newMemFiles(transcriptPath, transcriptBody), span, 0), limit)
			var got []domain.Message
			for _, p := range pages {
				if len(p) == 0 {
					t.Fatalf("limit %d span %d: empty page in %v", limit, span, pages)
				}
				got = append(got, p...)
			}
			if !reflect.DeepEqual(ids(got), want) {
				t.Fatalf("limit %d span %d: pages %v, want %v", limit, span, ids(got), want)
			}
		}
	}
}

func TestTranscriptPageKeepsALinesMessagesTogetherAndPointsBeforeAtItsStart(t *testing.T) {
	tr := transcripts(newMemFiles(transcriptPath, transcriptBody), 0, 0)
	page, err := tr.Page(domain.HarnessClaude, transcriptPath, lineEnd(transcriptBody, "a2 a3 done\n"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(page.Messages), []string{"a3", "a3:1"}) || page.Before != lineEnd(transcriptBody, "ok c1 PASS\n") {
		t.Fatalf("page %v before %d", ids(page.Messages), page.Before)
	}
}

func TestTranscriptPageFinishesACallFromAResultOnANewerPageWithinReach(t *testing.T) {
	before := lineEnd(transcriptBody, "call c1 go test\n")
	page, err := transcripts(newMemFiles(transcriptPath, transcriptBody), 0, 0).Page(domain.HarnessClaude, transcriptPath, before, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Tool.Status != domain.ToolDone || page.Messages[0].Text != "PASS" || page.Messages[0].Cursor != before {
		t.Fatalf("page %+v", page.Messages)
	}
	short, err := transcripts(newMemFiles(transcriptPath, transcriptBody), 0, 4).Page(domain.HarnessClaude, transcriptPath, before, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(short.Messages), []string{"c1=running"}) {
		t.Fatalf("out of reach %v", ids(short.Messages))
	}
}

func TestTranscriptPageNewestHoldsBackAPartialLine(t *testing.T) {
	files := newMemFiles(transcriptPath, "u p1 hi\na a1 par")
	page, err := transcripts(files, 0, 0).Page(domain.HarnessClaude, transcriptPath, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(page.Messages), []string{"p1"}) || page.Before != 0 {
		t.Fatalf("page %v before %d", ids(page.Messages), page.Before)
	}
}

func TestTranscriptPageOfNoFileIsEmpty(t *testing.T) {
	tr := transcripts(newMemFiles("/t/other", "u x hi\n"), 0, 0)
	for _, path := range []string{"", transcriptPath} {
		page, err := tr.Page(domain.HarnessClaude, path, 0, 10)
		if err != nil || len(page.Messages) != 0 || page.Before != 0 {
			t.Fatalf("path %q: page %+v err %v", path, page, err)
		}
	}
}

func TestTranscriptCursorsMustSitOnALineBoundary(t *testing.T) {
	files := newMemFiles(transcriptPath, transcriptBody)
	tr := transcripts(files, 0, 0)
	size := int64(len(transcriptBody))
	for _, before := range []int64{3, size + 1} {
		if _, err := tr.Page(domain.HarnessClaude, transcriptPath, before, 10); !errors.Is(err, app.ErrBadCursor) {
			t.Fatalf("page before %d: %v", before, err)
		}
	}
	for _, after := range []int64{3, size + 1} {
		if _, err := tr.Tail(domain.HarnessClaude, transcriptPath, after); !errors.Is(err, app.ErrBadCursor) {
			t.Fatalf("tail after %d: %v", after, err)
		}
	}
	tail, err := tr.Tail(domain.HarnessClaude, transcriptPath, lineEnd(transcriptBody, "a a1 reading\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tail.Since(size); !errors.Is(err, app.ErrBadCursor) {
		t.Fatalf("since past the tail: %v", err)
	}
	if _, err := tail.Since(5); !errors.Is(err, app.ErrBadCursor) {
		t.Fatalf("since mid-line: %v", err)
	}
}

func TestTranscriptOfAnUnknownHarnessIsRefused(t *testing.T) {
	tr := transcripts(newMemFiles(transcriptPath, transcriptBody), 0, 0)
	if _, err := tr.Page(domain.HarnessCodex, transcriptPath, 0, 10); !errors.Is(err, app.ErrNoTranscriptParser) {
		t.Fatalf("page: %v", err)
	}
	if _, err := tr.Tail(domain.HarnessCodex, transcriptPath, 0); !errors.Is(err, app.ErrNoTranscriptParser) {
		t.Fatalf("tail: %v", err)
	}
}

func TestTranscriptTailReadsAppendedLinesAndAPartialOneOnceComplete(t *testing.T) {
	files := newMemFiles(transcriptPath, "")
	tail, err := transcripts(files, 0, 0).Tail(domain.HarnessClaude, transcriptPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	read := func() []string {
		t.Helper()
		msgs, err := tail.Read()
		if err != nil {
			t.Fatal(err)
		}
		return ids(msgs)
	}
	if got := read(); got != nil {
		t.Fatalf("empty file %v", got)
	}
	files.append(transcriptPath, "u p1 hi\na a1 hal")
	if got := read(); !reflect.DeepEqual(got, []string{"p1"}) {
		t.Fatalf("first read %v", got)
	}
	if got := read(); got != nil {
		t.Fatalf("nothing new %v", got)
	}
	files.append(transcriptPath, "f\n")
	if got := read(); !reflect.DeepEqual(got, []string{"a1"}) {
		t.Fatalf("completed line %v", got)
	}
}

func TestTranscriptTailOfAMissingFileWaitsForIt(t *testing.T) {
	files := newMemFiles("/t/other", "")
	tail, err := transcripts(files, 0, 0).Tail(domain.HarnessClaude, transcriptPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if msgs, err := tail.Read(); err != nil || msgs != nil {
		t.Fatalf("missing file: %v %v", msgs, err)
	}
	files.append(transcriptPath, "u p1 hi\n")
	if msgs, err := tail.Read(); err != nil || !reflect.DeepEqual(ids(msgs), []string{"p1"}) {
		t.Fatalf("created file: %v %v", ids(msgs), err)
	}
}

func TestTranscriptTailFinishesACallFromBeforeItsStart(t *testing.T) {
	body := "u p1 go\ncall c1 go test\n"
	files := newMemFiles(transcriptPath, body)
	tail, err := transcripts(files, 0, 0).Tail(domain.HarnessClaude, transcriptPath, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	files.append(transcriptPath, "ok c1 PASS\n")
	msgs, err := tail.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != "c1" || msgs[0].Tool.Name != "Bash" || msgs[0].Tool.Status != domain.ToolDone || msgs[0].Cursor != int64(len(body)) {
		t.Fatalf("read %+v", msgs)
	}
}

func TestTranscriptTailSinceGivesALaterWatcherTheBacklogAndFinishesItsCalls(t *testing.T) {
	files := newMemFiles(transcriptPath, transcriptBody)
	tail, err := transcripts(files, 0, 1).Tail(domain.HarnessClaude, transcriptPath, int64(len(transcriptBody)))
	if err != nil {
		t.Fatal(err)
	}
	backlog, err := tail.Since(lineEnd(transcriptBody, "call c1 go test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a2", "a3", "a3:1", "p2", "c2=running"}; !reflect.DeepEqual(ids(backlog), want) {
		t.Fatalf("backlog %v, want %v", ids(backlog), want)
	}
	if none, err := tail.Since(int64(len(transcriptBody))); err != nil || none != nil {
		t.Fatalf("since the tail's offset: %v %v", none, err)
	}
	files.append(transcriptPath, "ok c2 listed\n")
	msgs, err := tail.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != "c2" || msgs[0].Tool.Name != "Bash" || msgs[0].Text != "listed" {
		t.Fatalf("read %+v", msgs)
	}
}

func TestTranscriptTailSinceStopsAtWhatTheTailHasRead(t *testing.T) {
	body := "u p1 hi\na a1 there\n"
	files := newMemFiles(transcriptPath, body)
	tail, err := transcripts(files, 0, 0).Tail(domain.HarnessClaude, transcriptPath, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	files.append(transcriptPath, "a a2 unread\n")
	backlog, err := tail.Since(lineEnd(body, "u p1 hi\n"))
	if err != nil || !reflect.DeepEqual(ids(backlog), []string{"a1"}) {
		t.Fatalf("backlog %v %v", ids(backlog), err)
	}
	msgs, err := tail.Read()
	if err != nil || !reflect.DeepEqual(ids(msgs), []string{"a2"}) {
		t.Fatalf("read %v %v", ids(msgs), err)
	}
}
