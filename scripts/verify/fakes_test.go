package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCall struct {
	cmd cmd
}

type fakeSys struct {
	calls    []fakeCall
	handlers map[string]func(c cmd) (string, string, int)
	paths    map[string]bool
	vars     map[string]string
	busy     map[int]bool
	nextPort int
	loadAvg  float64
	cpus     int
	started  []cmd
	killed   []int
	http     func(method, url, body string) (int, string, error)
}

func newFakeSys() *fakeSys {
	return &fakeSys{
		handlers: map[string]func(c cmd) (string, string, int){},
		paths:    map[string]bool{},
		vars:     map[string]string{},
		busy:     map[int]bool{},
		nextPort: 41000,
		cpus:     8,
	}
}

func (f *fakeSys) exec(c cmd) (string, string, int) {
	f.calls = append(f.calls, fakeCall{c})
	key := filepath.Base(c.name)
	if len(c.args) > 0 {
		if h, ok := f.handlers[key+" "+c.args[0]]; ok {
			return h(c)
		}
	}
	if h, ok := f.handlers[key]; ok {
		return h(c)
	}
	if f.paths[c.name] || strings.Contains(c.name, "/") {
		return "", "", 0
	}
	return "", c.name + ": not found", 127
}

func (f *fakeSys) look(name string) (string, bool) {
	if f.paths[name] {
		return "/fake/bin/" + name, true
	}
	return "", false
}

func (f *fakeSys) getenv(k string) string { return f.vars[k] }

func (f *fakeSys) start(c cmd, log string) (int, error) {
	f.started = append(f.started, c)
	return 9000 + len(f.started), nil
}

func (f *fakeSys) kill(pid int) { f.killed = append(f.killed, pid) }

func (f *fakeSys) portFree(p int) bool { return !f.busy[p] }

func (f *fakeSys) freePort() int {
	f.nextPort++
	return f.nextPort
}

func (f *fakeSys) load() (float64, int) { return f.loadAvg, f.cpus }

func (f *fakeSys) request(method, url, body string) (int, string, error) {
	if f.http != nil {
		return f.http(method, url, body)
	}
	return 200, "{}", nil
}

func (f *fakeSys) ran(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(append([]string{c.cmd.name}, c.cmd.args...), " "), prefix) {
			return true
		}
	}
	return false
}

func runJSON(t *testing.T, e *env, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	e.out = &out
	code := run(args, e)
	var got map[string]any
	dec := json.NewDecoder(&out)
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("%v: output is not one JSON object: %v: %q", args, err, out.String())
	}
	if dec.More() {
		t.Fatalf("%v: output has more than one JSON value: %q", args, out.String())
	}
	if _, ok := got["ok"].(bool); !ok {
		t.Fatalf("%v: no ok field: %v", args, got)
	}
	if _, ok := got["problems"].([]any); !ok {
		t.Fatalf("%v: no problems list: %v", args, got)
	}
	return got, code
}

func problemText(got map[string]any) string {
	var b strings.Builder
	for _, p := range got["problems"].([]any) {
		m := p.(map[string]any)
		b.WriteString(m["msg"].(string))
		b.WriteString(" | ")
		if h, ok := m["hint"].(string); ok {
			b.WriteString(h)
		}
		b.WriteString("\n")
	}
	return b.String()
}
