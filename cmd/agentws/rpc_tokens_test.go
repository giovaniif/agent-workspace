package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func reviewLine(text string) string {
	b, _ := json.Marshal(map[string]any{"Kind": "add", "Old": 0, "New": 1, "Text": text})
	return string(b)
}

func reviewFile(path string, texts ...string) string {
	lines := make([]string, len(texts))
	for i, t := range texts {
		lines[i] = reviewLine(t)
	}
	return `{"Path":"` + path + `","Status":"modified","Hunks":[{"Header":"@@ -0,0 +1 @@","Lines":[` + strings.Join(lines, ",") + `]}]}`
}

func reviewResult() string {
	files := []string{
		reviewFile("main.go", "package main", "", "func main() {", "\tfmt.Println(\"hi\", 42) // greet", "}"),
		reviewFile("web/app.ts", "export const total = (n: number): string => `${n}`;", "/* sum */ let x = 1 + 2;"),
		reviewFile("README.md", "# agentws", "- run `make build`", "<!-- note -->"),
		reviewFile("notes.unknownext", "just some text"),
	}
	return `{"scope":"branch","worktrees":[{"Worktree":{"ID":"w1","Repo":"api"},"From":"main","Files":[` + strings.Join(files, ",") + `],"Err":""}],"viewed":[],"draft":{}}`
}

func runReviewOpen(t *testing.T, request string) (daemonGot, stdout string) {
	t.Helper()
	home := shortHome(t)
	t.Setenv("AGENTWS_HOME", home)
	ln := fakeSocket(t, home)
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(c)
		first, _ := r.ReadString('\n')
		got <- first
		_, _ = io.WriteString(c, `{"v":1,"id":3,"result":`+reviewResult()+"}\n")
		_ = c.Close()
	}()
	inR, inW := io.Pipe()
	defer func() { _ = inW.Close() }()
	go func() { _, _ = io.WriteString(inW, request+"\n") }()
	var out, stderr bytes.Buffer
	if code := runRPC(nil, inR, &out, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	return <-got, out.String()
}

const tokensRequest = `{"v":1,"id":3,"method":"review.open","params":{"session":"s1","scope":"branch","tokens":true}}`

func TestRpcReviewOpenTokensAddsSpansPerLine(t *testing.T) {
	_, out := runReviewOpen(t, tokensRequest)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(out), "", "  "); err != nil {
		t.Fatalf("stdout %q: %v", out, err)
	}
	const golden = "testdata/review_tokens.json.golden"
	if *updateGolden {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if pretty.String() != string(want) {
		t.Errorf("spans changed; run with -update and review the diff.\ngot:\n%s\nwant:\n%s", pretty.String(), want)
	}
}

func TestRpcReviewOpenTokensLeavesAFileWithNoLexerWithoutSpans(t *testing.T) {
	_, out := runReviewOpen(t, tokensRequest)
	var resp struct {
		Error  any `json:"error"`
		Result struct {
			Worktrees []struct {
				Files []struct {
					Path  string
					Hunks []struct {
						Lines []map[string]json.RawMessage
					}
				}
			} `json:"worktrees"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil || resp.Error != nil || len(resp.Result.Worktrees) != 1 {
		t.Fatalf("stdout %q", out)
	}
	sawSpans := false
	for _, f := range resp.Result.Worktrees[0].Files {
		for _, l := range f.Hunks[0].Lines {
			_, has := l["Spans"]
			if f.Path == "notes.unknownext" && has {
				t.Fatalf("unknown file got spans: %s", l["Spans"])
			}
			sawSpans = sawSpans || has
		}
	}
	if !sawSpans {
		t.Fatalf("no line got spans: %q", out)
	}
}

func TestRpcReviewOpenWithoutTokensPassesTheReplyThrough(t *testing.T) {
	daemonGot, out := runReviewOpen(t, `{"v":1,"id":3,"method":"review.open","params":{"session":"s1","scope":"branch"}}`)
	if !strings.Contains(daemonGot, `"method":"review.open"`) {
		t.Fatalf("daemon got %q", daemonGot)
	}
	if out != `{"v":1,"id":3,"result":`+reviewResult()+"}\n" {
		t.Fatalf("reply was changed: %q", out)
	}
}
