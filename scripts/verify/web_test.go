package main

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

type driverReply struct {
	ConsoleErrors  []string `json:"console_errors"`
	PageErrors     []string `json:"page_errors"`
	FailedRequests []string `json:"failed_requests"`
	Error          string   `json:"error,omitempty"`
	FailedStep     *int     `json:"failed_step,omitempty"`
}

func fakeNode(t *testing.T, fs *fakeSys, size func(viewport string) (int, int), reply driverReply, seen *map[string]any) {
	t.Helper()
	fs.paths["node"] = true
	fs.handlers["node"] = func(c cmd) (string, string, int) {
		var req map[string]any
		if err := json.Unmarshal([]byte(c.stdin), &req); err != nil {
			t.Fatalf("driver stdin is not JSON: %v", err)
		}
		if seen != nil {
			*seen = req
		}
		var shots []map[string]any
		for _, v := range req["viewports"].([]any) {
			name := v.(map[string]any)["name"].(string)
			for _, s := range req["steps"].([]any) {
				if p, ok := s.(map[string]any)["shot"].(string); ok {
					path := req["out_dir"].(string) + "/" + name + "-" + p
					w, h := size(name)
					writePNG(t, path, w, h)
					shots = append(shots, map[string]any{"path": path, "viewport": name})
				}
			}
		}
		out := map[string]any{"shots": shots, "console_errors": reply.ConsoleErrors, "page_errors": reply.PageErrors, "failed_requests": reply.FailedRequests, "error": reply.Error}
		if reply.FailedStep != nil {
			out["failed_step"] = *reply.FailedStep
		}
		b, _ := json.Marshal(out)
		code := 0
		if reply.Error != "" {
			code = 1
		}
		return string(b), "", code
	}
}

func rightSize(v string) (int, int) {
	if v == "mobile" {
		return 780, 1688
	}
	return 2880, 1800
}

func TestWebShotBothViewportsAgainstTheIsolatedServe(t *testing.T) {
	e, fs := testEnv(t)
	isoReady(fs)
	runJSON(t, e, "iso", "up")
	var req map[string]any
	fakeNode(t, fs, rightSize, driverReply{}, &req)
	got, code := runJSON(t, e, "web", "shot", "/", "--viewport", "both", "--out-dir", t.TempDir())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	if n := len(got["shots"].([]any)); n != 2 {
		t.Errorf("%d shots", n)
	}
	st, _ := loadIso(e)
	if req["base"] != st.WebURL {
		t.Errorf("base %v, want %v", req["base"], st.WebURL)
	}
	if auth, _ := req["auth"].(map[string]any); auth == nil || auth["token"] != "tok-1" {
		t.Errorf("auth %v", req["auth"])
	}
	if req["settle_ms"] != float64(700) {
		t.Errorf("settle_ms %v", req["settle_ms"])
	}
	vps := req["viewports"].([]any)
	m := vps[0].(map[string]any)
	if m["width"] != float64(390) || m["height"] != float64(844) || m["scale"] != float64(2) {
		t.Errorf("mobile viewport %v", m)
	}
}

func TestWebShotRejectsAWrongSizeShot(t *testing.T) {
	e, fs := testEnv(t)
	isoReady(fs)
	runJSON(t, e, "iso", "up")
	fakeNode(t, fs, func(string) (int, int) { return 100, 100 }, driverReply{}, nil)
	got, code := runJSON(t, e, "web", "shot", "/", "--viewport", "mobile", "--out-dir", t.TempDir())
	if code != 1 || !strings.Contains(problemText(got), "780x1688") {
		t.Errorf("exit %d: %s", code, problemText(got))
	}
}

func TestWebShotFailsOnConsoleAndPageErrorsAndFailedAPICalls(t *testing.T) {
	e, fs := testEnv(t)
	isoReady(fs)
	runJSON(t, e, "iso", "up")
	fakeNode(t, fs, rightSize, driverReply{ConsoleErrors: []string{"boom"}, PageErrors: []string{"TypeError: x"}, FailedRequests: []string{"GET /api/v1/workspaces 500"}}, nil)
	got, code := runJSON(t, e, "web", "shot", "/", "--viewport", "desktop", "--out-dir", t.TempDir())
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	text := problemText(got)
	for _, want := range []string{"boom", "TypeError", "/api/v1/workspaces 500"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems %q lack %q", text, want)
		}
	}
	if len(got["failed_requests"].([]any)) != 1 {
		t.Errorf("failed_requests %v", got["failed_requests"])
	}
}

func TestWebDriveValidatesSteps(t *testing.T) {
	tests := []struct {
		steps string
		want  string
	}{
		{`[{"click":{"role":"button"}}]`, "name"},
		{`[{"click":{}}]`, "locator"},
		{`[{"fly":"/"}]`, "fly"},
		{`[{"fill":{"label":"Code"}}]`, "value"},
		{`{}`, "list"},
	}
	for _, tt := range tests {
		e, fs := testEnv(t)
		isoReady(fs)
		runJSON(t, e, "iso", "up")
		got, code := runJSON(t, e, "web", "drive", "/", "--steps", tt.steps)
		if code != 2 || !strings.Contains(problemText(got), tt.want) {
			t.Errorf("%s: exit %d: %s", tt.steps, code, problemText(got))
		}
	}
}

func TestWebDriveFailureReportsTheStep(t *testing.T) {
	e, fs := testEnv(t)
	isoReady(fs)
	runJSON(t, e, "iso", "up")
	step := 1
	fakeNode(t, fs, rightSize, driverReply{Error: "no visible match for role=button name=Go", FailedStep: &step}, nil)
	got, code := runJSON(t, e, "web", "drive", "/", "--steps", `[{"goto":"/"},{"click":{"role":"button","name":"Go"}}]`, "--out-dir", t.TempDir())
	if code != 1 || got["failed_step"] != float64(1) || !strings.Contains(problemText(got), "Go") {
		t.Errorf("exit %d: %v %s", code, got["failed_step"], problemText(got))
	}
}

func TestWebFakeModeStartsTheFakeAPIWithoutIso(t *testing.T) {
	e, fs := testEnv(t)
	var req map[string]any
	fakeNode(t, fs, rightSize, driverReply{}, &req)
	got, code := runJSON(t, e, "web", "shot", "/", "--fake", "--viewport", "mobile", "--out-dir", t.TempDir())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, problemText(got))
	}
	if len(fs.started) != 1 || envOf(fs.started[0])["CREW_FAKE_API"] != "1" {
		t.Errorf("fake API not started: %v", fs.started)
	}
	if len(fs.killed) != 1 {
		t.Error("fake API not stopped")
	}
	if req["auth"] != nil {
		t.Errorf("fake mode sent auth %v", req["auth"])
	}
}
