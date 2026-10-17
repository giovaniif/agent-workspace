package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type isoState struct {
	Home      string          `json:"home"`
	Socket    string          `json:"socket"`
	Bin       string          `json:"bin"`
	ServePort int             `json:"serve_port"`
	WebPort   int             `json:"web_port"`
	ServeURL  string          `json:"serve_url"`
	WebURL    string          `json:"web_url"`
	Pids      []int           `json:"pids"`
	Token     string          `json:"token"`
	Device    json.RawMessage `json:"device"`
	Env       []string        `json:"env"`
	Started   time.Time       `json:"started"`
}

func isoPath(e *env) string { return filepath.Join(e.state, "iso.json") }

func loadIso(e *env) (isoState, error) {
	var st isoState
	b, err := os.ReadFile(isoPath(e))
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

func saveIso(e *env, st isoState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(isoPath(e), b, 0o600)
}

func needIso(e *env, r *result) (isoState, bool) {
	st, err := loadIso(e)
	if err != nil {
		r.fail("the isolated crew is not up", "run go run ./scripts/verify iso up")
		return st, false
	}
	return st, true
}

func iso(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("iso needs up, down or seed", "go run ./scripts/verify iso up")
		return
	}
	switch args[0] {
	case "up":
		isoUp(e, args[1:], r)
	case "down":
		isoDown(e, r)
	case "seed":
		isoSeed(e, args[1:], r)
	case "crew":
		isoCrew(e, args[1:], r)
	default:
		r.bad("unknown iso command "+args[0], "use up, down or seed")
	}
}

var blanked = []string{
	"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN", "SLACK_WEBHOOK_URL", "NTFY_TOPIC", "NTFY_URL",
	"GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "ANTHROPIC_API_KEY", "OPENAI_API_KEY",
}

func isoEnv(e *env, home, socket string) []string {
	h := filepath.Join(home, "home")
	fakes := filepath.Join(e.repo, "test", "e2e", "testdata", "bin")
	vars := []string{
		"CREW_HOME=" + home,
		"CREW_TMUX_SOCKET=" + socket,
		"HOME=" + h,
		"XDG_CONFIG_HOME=" + filepath.Join(h, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(h, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(h, ".local", "state"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(h, ".claude"),
		"CODEX_HOME=" + filepath.Join(h, ".codex"),
		"CREW_E2E=" + filepath.Join(home, "e2e"),
		"PATH=" + fakes + string(os.PathListSeparator) + e.sys.getenv("PATH"),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}
	for _, k := range blanked {
		vars = append(vars, k+"=")
	}
	return vars
}

var pairCode = regexp.MustCompile(`Code ([A-Z0-9]+),`)

func isoUp(e *env, args []string, r *result) {
	if _, ok := parse(args, nil, nil, r); !ok {
		return
	}
	if st, err := loadIso(e); err == nil {
		r.set("home", st.Home)
		r.fail("the isolated crew is already up at "+st.Home, "run go run ./scripts/verify iso down first")
		return
	}
	home, err := os.MkdirTemp("/tmp", "crew-iso-")
	if err != nil {
		r.fail("cannot make the isolated home: "+err.Error(), "free space in /tmp")
		return
	}
	st := isoState{Home: home, Socket: "crew-iso-" + filepath.Base(home)[len("crew-iso-"):], Bin: filepath.Join(home, "bin", "crew"), Started: e.now()}
	st.Env = isoEnv(e, home, st.Socket)
	for _, d := range []string{"bin", "e2e", "home/.config", "home/.local/share", "home/.local/state", "home/.claude", "home/.codex"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	for _, f := range []string{"onboarded", "e2e/claude.steps", "e2e/codex.steps"} {
		_ = os.WriteFile(filepath.Join(home, f), nil, 0o644)
	}
	ok := false
	defer func() {
		if !ok {
			stopIso(e, st)
		}
	}()
	if out, errs, code := e.sys.exec(cmd{dir: e.repo, name: "go", args: []string{"build", "-o", st.Bin, "./cmd/crew"}}); code != 0 {
		r.fail("crew does not build: "+firstLine(out+errs, "error"), "run make build and fix the error")
		return
	}
	if out, errs, code := e.sys.exec(cmd{dir: home, env: st.Env, name: st.Bin, args: []string{"daemon", "start"}}); code != 0 {
		r.fail("the isolated daemon did not start: "+strings.TrimSpace(out+errs), "read "+filepath.Join(home, "daemon.log"))
		return
	}
	st.ServePort, st.WebPort = e.sys.freePort(), e.sys.freePort()
	st.ServeURL = "http://127.0.0.1:" + strconv.Itoa(st.ServePort)
	st.WebURL = "http://127.0.0.1:" + strconv.Itoa(st.WebPort)
	pid, err := e.sys.start(cmd{dir: home, env: st.Env, name: st.Bin, args: []string{"serve", "--addr", "127.0.0.1:" + strconv.Itoa(st.ServePort), "--url", st.WebURL}}, filepath.Join(home, "serve.log"))
	if err != nil {
		r.fail("crew serve did not start: "+err.Error(), "read "+filepath.Join(home, "serve.log"))
		return
	}
	st.Pids = append(st.Pids, pid)
	webEnv := append(append([]string{}, st.Env...), "CREW_API="+st.ServeURL)
	pid, err = e.sys.start(viteCmd(e, webEnv, st.WebPort), filepath.Join(home, "vite.log"))
	if err != nil {
		r.fail("the web dev server did not start: "+err.Error(), "cd web && npm ci")
		return
	}
	st.Pids = append(st.Pids, pid)
	if err := saveIso(e, st); err != nil {
		r.fail("cannot save the state: "+err.Error(), "check "+e.state)
		return
	}
	if !waitHTTP(e, st.ServeURL+"/api/v1/hello") {
		r.fail("crew serve does not answer on "+st.ServeURL, "read "+filepath.Join(home, "serve.log"))
		return
	}
	if !waitHTTP(e, st.WebURL+"/api/v1/hello") {
		r.fail("the web dev server does not answer on "+st.WebURL, "read "+filepath.Join(home, "vite.log")+"; run cd web && npm ci")
		return
	}
	if err := pairDevice(e, &st); err != nil {
		r.fail("cannot pair a device: "+err.Error(), "read "+filepath.Join(home, "daemon.log"))
		return
	}
	if err := saveIso(e, st); err != nil {
		r.fail("cannot save the state: "+err.Error(), "check "+e.state)
		return
	}
	ok = true
	isoReport(st, r)
}

func viteCmd(e *env, vars []string, port int) cmd {
	web := filepath.Join(e.repo, "web")
	return cmd{dir: web, env: vars, name: "node", args: []string{filepath.Join(web, "node_modules", "vite", "bin", "vite.js"), "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--strictPort"}}
}

func isoReport(st isoState, r *result) {
	r.set("home", st.Home)
	r.set("socket", st.Socket)
	r.set("tui_socket", st.Socket+"-tui")
	r.set("bin", st.Bin)
	r.set("serve_url", st.ServeURL)
	r.set("web_url", st.WebURL)
	r.set("env", st.Env)
}

func waitHTTP(e *env, url string) bool {
	for range 150 {
		if code, _, err := e.sys.request("GET", url, ""); err == nil && code == 200 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func pairDevice(e *env, st *isoState) error {
	out, errs, code := e.sys.exec(cmd{dir: st.Home, env: append(append([]string{}, st.Env...), "COLUMNS=10"), name: st.Bin, args: []string{"remote", "pair", "--name", "TEST-verify", "--url", "https://iso.invalid"}})
	if code != 0 {
		return fmt.Errorf("crew remote pair: %s", strings.TrimSpace(out+errs))
	}
	m := pairCode.FindStringSubmatch(out)
	if m == nil {
		return errors.New("no pairing code in the crew remote pair output")
	}
	body, _ := json.Marshal(map[string]string{"code": m[1], "name": "TEST-verify"})
	status, resp, err := e.sys.request("POST", st.ServeURL+"/api/v1/pair", string(body))
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("POST /api/v1/pair answered %d", status)
	}
	var paired struct {
		Token  string          `json:"token"`
		Device json.RawMessage `json:"device"`
	}
	if err := json.Unmarshal([]byte(resp), &paired); err != nil || paired.Token == "" {
		return errors.New("POST /api/v1/pair gave no token")
	}
	st.Token, st.Device = paired.Token, paired.Device
	return nil
}

func stopIso(e *env, st isoState) {
	for _, pid := range st.Pids {
		e.sys.kill(pid)
	}
	e.sys.exec(cmd{dir: st.Home, env: st.Env, name: st.Bin, args: []string{"daemon", "stop"}})
	for _, s := range []string{st.Socket + "-tui", st.Socket} {
		e.sys.exec(cmd{name: "tmux", args: []string{"-L", s, "kill-server"}})
	}
	if strings.HasPrefix(st.Home, "/tmp/crew-iso-") {
		_ = os.RemoveAll(st.Home)
	}
	_ = os.Remove(isoPath(e))
}

func isoDown(e *env, r *result) {
	st, err := loadIso(e)
	if err != nil {
		r.set("stopped", false)
		return
	}
	stopIso(e, st)
	r.set("stopped", true)
	r.set("home", st.Home)
}

func isoCrew(e *env, args []string, r *result) {
	var rest []string
	for i, a := range args {
		if a == "--" {
			args, rest = args[:i], args[i+1:]
			break
		}
	}
	o, ok := parse(args, []string{"expect", "absent"}, nil, r)
	if !ok {
		return
	}
	rest = append(o.pos, rest...)
	if len(rest) == 0 {
		r.bad("iso crew needs crew arguments", "go run ./scripts/verify iso crew -- pr seed-session-1")
		return
	}
	st, ok := needIso(e, r)
	if !ok {
		return
	}
	out, errs, code := e.sys.exec(cmd{dir: st.Home, env: st.Env, name: st.Bin, args: rest})
	r.set("stdout", tail(out, 20000))
	r.set("stderr", tail(errs, 4000))
	r.set("exit", code)
	if code != 0 {
		r.fail("crew "+strings.Join(rest, " ")+" exited "+strconv.Itoa(code)+": "+strings.TrimSpace(tail(errs, 300)), "read "+filepath.Join(st.Home, "daemon.log"))
	}
	for _, want := range o.vals["expect"] {
		if !strings.Contains(out, want) {
			r.fail("the output has no "+strconv.Quote(want), "read stdout in the result")
		}
	}
	for _, not := range o.vals["absent"] {
		if strings.Contains(out, not) {
			r.fail("the output has "+strconv.Quote(not), "read stdout in the result")
		}
	}
}

func isoSeed(e *env, args []string, r *result) {
	o, ok := parse(args, []string{"sessions"}, []string{"codex"}, r)
	if !ok {
		return
	}
	st, ok := needIso(e, r)
	if !ok {
		return
	}
	n := o.get("sessions", "3")
	if v, err := strconv.Atoi(n); err != nil || v < 1 {
		r.bad("--sessions must be a positive number", "--sessions 3")
		return
	}
	a := []string{"debug", "seed", n}
	if o.bools["codex"] {
		a = append(a, "--codex")
	}
	out, errs, code := e.sys.exec(cmd{dir: st.Home, env: st.Env, name: st.Bin, args: a})
	if code != 0 {
		r.fail("crew debug seed failed: "+strings.TrimSpace(out+errs), "read "+filepath.Join(st.Home, "daemon.log"))
	}
	isoReport(st, r)
	r.set("seeded", n)
}
