package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type tuiStep struct {
	Keys    *string `json:"keys,omitempty"`
	Type    *string `json:"type,omitempty"`
	Wait    *int    `json:"wait,omitempty"`
	Expect  *string `json:"expect,omitempty"`
	Absent  *string `json:"absent,omitempty"`
	Timeout int     `json:"timeout,omitempty"`
	Shot    *string `json:"shot,omitempty"`
}

var tuiVerbs = map[string]bool{"keys": true, "type": true, "wait": true, "expect": true, "absent": true, "shot": true, "timeout": true}

func parseTUISteps(raw string) ([]tuiStep, error) {
	var generic []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return nil, fmt.Errorf("steps must be a JSON list of objects: %v", err)
	}
	steps := make([]tuiStep, len(generic))
	for i, g := range generic {
		verbs := 0
		for k := range g {
			if !tuiVerbs[k] {
				return nil, fmt.Errorf("step %d: unknown key %q", i, k)
			}
			if k != "timeout" {
				verbs++
			}
		}
		if verbs != 1 {
			return nil, fmt.Errorf("step %d: give exactly one of keys, type, wait, expect, absent, shot", i)
		}
		b, _ := json.Marshal(g)
		if err := json.Unmarshal(b, &steps[i]); err != nil {
			return nil, fmt.Errorf("step %d: %v", i, err)
		}
	}
	return steps, nil
}

const tuiSession = "verify"

func tui(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("tui needs shot or drive", "go run ./scripts/verify tui shot --out a.png")
		return
	}
	o, ok := parse(args[1:], []string{"out", "out-dir", "size", "steps"}, []string{"restart"}, r)
	if !ok {
		return
	}
	var steps []tuiStep
	switch args[0] {
	case "shot":
		out := o.get("out", "")
		if out == "" {
			d, err := outDir(e, o, "tui")
			if err != nil {
				r.fail(err.Error(), "check the --out-dir")
				return
			}
			out = filepath.Join(d, "tui.png")
		}
		s := out
		steps = []tuiStep{{Shot: &s}}
	case "drive":
		var err error
		if steps, err = parseTUISteps(o.get("steps", "")); err != nil {
			r.bad(err.Error(), `--steps '[{"keys":"j"},{"expect":"TEST-"},{"shot":"after.png"}]'`)
			return
		}
	default:
		r.bad("unknown tui command "+args[0], "use shot or drive")
		return
	}
	st, ok := needIso(e, r)
	if !ok {
		return
	}
	dir, err := outDir(e, o, "tui")
	if err != nil {
		r.fail(err.Error(), "check the --out-dir")
		return
	}
	t := tmuxTUI{e: e, st: st, sock: st.Socket + "-tui"}
	w, h := 120, 40
	if sz := o.get("size", ""); sz != "" {
		a, b, _ := strings.Cut(sz, "x")
		w, _ = strconv.Atoi(a)
		h, _ = strconv.Atoi(b)
		if w < 20 || h < 10 {
			r.bad("--size must be COLSxROWS, at least 20x10", "--size 120x40")
			return
		}
	}
	if o.bools["restart"] {
		t.tmux("kill-server")
	}
	if err := t.ensure(w, h); err != nil {
		r.fail("cannot start the TUI: "+err.Error(), "run go run ./scripts/verify iso down, then iso up")
		return
	}
	r.set("tui_socket", t.sock)
	var shots []map[string]any
	for i, s := range steps {
		if err := t.step(s, dir, &shots); err != nil {
			pane := t.capture(false)
			fp := filepath.Join(dir, "failure.txt")
			_ = os.WriteFile(fp, []byte(pane), 0o644)
			r.set("failed_step", i)
			r.set("failure_capture", fp)
			r.fail(fmt.Sprintf("step %d: %v", i, err), "read "+fp+" for the pane at the failure")
			break
		}
	}
	if shots == nil {
		shots = []map[string]any{}
	}
	r.set("shots", shots)
	if len(shots) > 0 {
		r.set("sha256", shots[len(shots)-1]["sha256"])
		r.set("path", shots[len(shots)-1]["path"])
	}
}

type tmuxTUI struct {
	e    *env
	st   isoState
	sock string
}

func (t tmuxTUI) tmux(args ...string) (string, int) {
	out, errs, code := t.e.sys.exec(cmd{dir: t.st.Home, env: t.st.Env, name: "tmux", args: append([]string{"-L", t.sock}, args...)})
	if code != 0 {
		return out + errs, code
	}
	return out, 0
}

func (t tmuxTUI) ensure(w, h int) error {
	if _, code := t.tmux("has-session", "-t", tuiSession); code == 0 {
		return nil
	}
	if out, code := t.tmux("-f", "/dev/null", "new-session", "-d", "-s", tuiSession, "-x", strconv.Itoa(w), "-y", strconv.Itoa(h), "-c", t.st.Home, t.st.Bin, "tui"); code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(out))
	}
	t.tmux("set", "-g", "status", "off")
	t.tmux("set", "-g", "default-terminal", "tmux-256color")
	t.tmux("set", "-as", "terminal-features", ",*:RGB")
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func (t tmuxTUI) capture(colour bool) string {
	a := []string{"capture-pane", "-p", "-t", tuiSession}
	if colour {
		a = append(a, "-e")
	}
	out, _ := t.tmux(a...)
	return out
}

func (t tmuxTUI) step(s tuiStep, dir string, shots *[]map[string]any) error {
	switch {
	case s.Keys != nil:
		if out, code := t.tmux(append([]string{"send-keys", "-t", tuiSession}, strings.Fields(*s.Keys)...)...); code != 0 {
			return fmt.Errorf("send-keys: %s", out)
		}
		time.Sleep(300 * time.Millisecond)
	case s.Type != nil:
		if out, code := t.tmux("send-keys", "-t", tuiSession, "-l", *s.Type); code != 0 {
			return fmt.Errorf("send-keys: %s", out)
		}
		time.Sleep(300 * time.Millisecond)
	case s.Wait != nil:
		time.Sleep(time.Duration(*s.Wait) * time.Millisecond)
	case s.Expect != nil, s.Absent != nil:
		want, present := "", true
		if s.Expect != nil {
			want = *s.Expect
		} else {
			want, present = *s.Absent, false
		}
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = 5000
		}
		deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
		for {
			if strings.Contains(t.capture(false), want) == present {
				return nil
			}
			if time.Now().After(deadline) {
				if present {
					return fmt.Errorf("the pane does not show %q after %d ms", want, timeout)
				}
				return fmt.Errorf("the pane still shows %q after %d ms", want, timeout)
			}
			time.Sleep(100 * time.Millisecond)
		}
	case s.Shot != nil:
		path := *s.Shot
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if err := t.render(path); err != nil {
			return err
		}
		sum, err := fileHash(path)
		if err != nil {
			return fmt.Errorf("the shot %s was not written: %v", path, err)
		}
		*shots = append(*shots, map[string]any{"path": path, "sha256": sum})
	}
	return nil
}

func (t tmuxTUI) render(path string) error {
	freeze := "freeze"
	if _, ok := t.e.sys.look(freeze); !ok {
		gp, _, _ := t.e.sys.exec(cmd{name: "go", args: []string{"env", "GOPATH"}})
		freeze = filepath.Join(strings.TrimSpace(gp), "bin", "freeze")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	out, errs, code := t.e.sys.exec(cmd{name: freeze, stdin: t.capture(true), args: []string{"--language", "ansi", "--background", "#eff1f5", "--window=false", "--padding", "20", "--output", path}})
	if code != 0 {
		return fmt.Errorf("freeze: %s", strings.TrimSpace(out+errs))
	}
	return nil
}
