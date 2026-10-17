package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type viewport struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Scale  int    `json:"scale"`
}

var viewports = map[string]viewport{
	"mobile":  {"mobile", 390, 844, 2},
	"desktop": {"desktop", 1440, 900, 2},
}

var locatorKeys = []string{"role", "testId", "label", "placeholder", "text"}

func checkLocator(i int, verb string, raw json.RawMessage) error {
	var loc map[string]any
	if err := json.Unmarshal(raw, &loc); err != nil {
		return fmt.Errorf("step %d: %s needs a locator object", i, verb)
	}
	n := 0
	for _, k := range locatorKeys {
		if _, ok := loc[k]; ok {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("step %d: %s needs a locator with one of role+name, testId, label, placeholder, text", i, verb)
	}
	if _, ok := loc["role"]; ok {
		if s, _ := loc["name"].(string); s == "" {
			return fmt.Errorf("step %d: a role locator needs a name", i)
		}
	}
	return nil
}

var webVerbs = map[string]bool{"goto": true, "click": true, "fill": true, "press": true, "wait": true, "expect": true, "shot": true, "aria": true, "eval": true}

var webExtras = map[string]bool{"value": true, "timeout": true, "absent": true, "full": true}

func parseWebSteps(raw string) ([]map[string]json.RawMessage, error) {
	var steps []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, fmt.Errorf("steps must be a JSON list of objects: %v", err)
	}
	for i, s := range steps {
		verb := ""
		for k := range s {
			switch {
			case webVerbs[k]:
				if verb != "" {
					return nil, fmt.Errorf("step %d: give one verb, not %s and %s", i, verb, k)
				}
				verb = k
			case !webExtras[k]:
				return nil, fmt.Errorf("step %d: unknown key %q", i, k)
			}
		}
		if verb == "" {
			return nil, fmt.Errorf("step %d: no verb (goto, click, fill, press, wait, expect, shot, aria, eval)", i)
		}
		switch verb {
		case "click", "expect":
			if err := checkLocator(i, verb, s[verb]); err != nil {
				return nil, err
			}
		case "fill":
			if err := checkLocator(i, verb, s[verb]); err != nil {
				return nil, err
			}
			if _, ok := s["value"]; !ok {
				return nil, fmt.Errorf("step %d: fill needs a value", i)
			}
		case "goto", "shot", "eval", "press":
			var str string
			if err := json.Unmarshal(s[verb], &str); err != nil || str == "" {
				return nil, fmt.Errorf("step %d: %s needs a string", i, verb)
			}
		case "wait":
			var ms int
			if err := json.Unmarshal(s[verb], &ms); err != nil {
				return nil, fmt.Errorf("step %d: wait needs milliseconds", i)
			}
		}
	}
	return steps, nil
}

type driverResult struct {
	Shots []struct {
		Path     string `json:"path"`
		Viewport string `json:"viewport"`
	} `json:"shots"`
	ConsoleErrors  []string `json:"console_errors"`
	PageErrors     []string `json:"page_errors"`
	FailedRequests []string `json:"failed_requests"`
	Aria           []string `json:"aria"`
	Eval           []any    `json:"eval"`
	Video          []string `json:"video"`
	Error          string   `json:"error"`
	FailedStep     *int     `json:"failed_step"`
	Failure        string   `json:"failure"`
}

func web(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("web needs shot or drive", "go run ./scripts/verify web shot / --viewport both")
		return
	}
	o, ok := parse(args[1:], []string{"viewport", "steps", "out-dir", "scheme"}, []string{"fake", "video", "installed"}, r)
	if !ok {
		return
	}
	if len(o.pos) != 1 || !strings.HasPrefix(o.pos[0], "/") {
		r.bad("web "+args[0]+" needs one route that starts with /", "go run ./scripts/verify web "+args[0]+" /")
		return
	}
	route := o.pos[0]
	var steps []map[string]json.RawMessage
	switch args[0] {
	case "shot":
		g, _ := json.Marshal(route)
		steps = []map[string]json.RawMessage{{"goto": g}, {"shot": json.RawMessage(`"shot.png"`)}}
	case "drive":
		var err error
		if steps, err = parseWebSteps(o.get("steps", "")); err != nil {
			r.bad(err.Error(), `--steps '[{"click":{"role":"button","name":"New session"}},{"shot":"after.png"}]'`)
			return
		}
		g, _ := json.Marshal(route)
		steps = append([]map[string]json.RawMessage{{"goto": g}}, steps...)
	default:
		r.bad("unknown web command "+args[0], "use shot or drive")
		return
	}
	var vps []viewport
	switch v := o.get("viewport", "both"); v {
	case "both":
		vps = []viewport{viewports["mobile"], viewports["desktop"]}
	case "mobile", "desktop":
		vps = []viewport{viewports[v]}
	default:
		r.bad("unknown viewport "+v, "use mobile, desktop or both")
		return
	}
	dir, err := outDir(e, o, "web")
	if err != nil {
		r.fail(err.Error(), "check the --out-dir")
		return
	}
	req := map[string]any{
		"viewports": vps,
		"steps":     steps,
		"out_dir":   dir,
		"settle_ms": 700,
		"video":     o.bools["video"],
		"installed": o.bools["installed"],
		"scheme":    o.get("scheme", "light"),
	}
	if o.bools["fake"] {
		port := e.sys.freePort()
		base := "http://127.0.0.1:" + strconv.Itoa(port)
		vars := append(os.Environ(), "CREW_FAKE_API=1")
		pid, err := e.sys.start(viteCmd(e, vars, port), filepath.Join(dir, "vite.log"))
		if err != nil {
			r.fail("the fake API did not start: "+err.Error(), "cd web && npm ci")
			return
		}
		defer e.sys.kill(pid)
		if !waitHTTP(e, base+"/api/v1/hello") {
			r.fail("the fake API does not answer on "+base, "read "+filepath.Join(dir, "vite.log"))
			return
		}
		req["base"] = base
		req["auth"] = nil
		r.set("fake", true)
	} else {
		st, ok := needIso(e, r)
		if !ok {
			return
		}
		req["base"] = st.WebURL
		req["auth"] = map[string]any{"token": st.Token, "device": st.Device}
	}
	if _, ok := e.sys.look("ffmpeg"); o.bools["video"] && ok {
		req["ffmpeg"] = true
	}
	b, _ := json.Marshal(req)
	script := filepath.Join(e.repo, "web", "scripts", "verify-drive.ts")
	out, errs, code := e.sys.exec(cmd{dir: filepath.Join(e.repo, "web"), name: "node", args: []string{script}, stdin: string(b)})
	var res driverResult
	if err := json.Unmarshal([]byte(lastJSON(out)), &res); err != nil {
		r.fail("the browser driver gave no result: "+firstLine(out+errs, "Error"), "run go run ./scripts/verify doctor")
		return
	}
	r.set("route", route)
	r.set("base", req["base"])
	r.set("console_errors", nonNil(res.ConsoleErrors))
	r.set("page_errors", nonNil(res.PageErrors))
	r.set("failed_requests", nonNil(res.FailedRequests))
	if res.Aria != nil {
		r.set("aria", res.Aria)
	}
	if res.Eval != nil {
		r.set("eval", res.Eval)
	}
	if res.Video != nil {
		r.set("video", res.Video)
	}
	var shots []map[string]any
	for _, s := range res.Shots {
		shot := map[string]any{"path": s.Path, "viewport": s.Viewport}
		if sum, err := fileHash(s.Path); err == nil {
			shot["sha256"] = sum
		}
		if vp, ok := viewports[s.Viewport]; ok {
			if w, h, err := pngSize(s.Path); err != nil {
				r.fail("cannot read the shot "+s.Path+": "+err.Error(), "run the command again")
			} else if w != vp.Width*vp.Scale || h != vp.Height*vp.Scale {
				r.fail(fmt.Sprintf("the %s shot %s is %dx%d, want %dx%d", s.Viewport, s.Path, w, h, vp.Width*vp.Scale, vp.Height*vp.Scale), "do not take full-page shots for proof; use the fixed viewport")
			}
		}
		shots = append(shots, shot)
	}
	if shots == nil {
		shots = []map[string]any{}
	}
	r.set("shots", shots)
	for _, m := range res.ConsoleErrors {
		r.fail("console error: "+m, "open the route and fix the error, or prove that it is third-party noise")
	}
	for _, m := range res.PageErrors {
		r.fail("page error: "+m, "fix the uncaught error")
	}
	for _, m := range res.FailedRequests {
		r.fail("failed API call: "+m, "read serve.log in the isolated home")
	}
	if res.FailedStep != nil {
		r.set("failed_step", *res.FailedStep)
	}
	if res.Failure != "" {
		r.set("failure", res.Failure)
	}
	if res.Error != "" || code != 0 {
		msg := res.Error
		if msg == "" {
			msg = firstLine(errs, "Error")
		}
		hint := "check the step"
		if res.Failure != "" {
			hint = "read the failure shot and aria snapshot in " + res.Failure
		}
		r.fail("browser: "+msg, hint)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func lastJSON(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "{") {
			return strings.Join(lines[i:], "\n")
		}
	}
	return out
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	c, err := png.DecodeConfig(f)
	return c.Width, c.Height, err
}
