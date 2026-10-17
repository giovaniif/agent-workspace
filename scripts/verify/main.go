package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type problem struct {
	Msg  string `json:"msg"`
	Hint string `json:"hint"`
}

type result struct {
	data     map[string]any
	problems []problem
	usage    bool
}

func newResult() *result { return &result{data: map[string]any{}} }

func (r *result) fail(msg, hint string) { r.problems = append(r.problems, problem{msg, hint}) }

func (r *result) bad(msg, hint string) {
	r.usage = true
	r.fail(msg, hint)
}

func (r *result) set(k string, v any) { r.data[k] = v }

type env struct {
	out   io.Writer
	sys   sys
	repo  string
	state string
	now   func() time.Time
}

const usageHint = "run go run ./scripts/verify help"

var commands = map[string]func(e *env, args []string, r *result){
	"doctor":       doctor,
	"iso":          iso,
	"tui":          tui,
	"web":          web,
	"mac":          mac,
	"map":          mapCmd,
	"evidence":     evidence,
	"design-check": designCheck,
	"annotate":     annotate,
	"assets":       assets,
	"selftest":     selftest,
}

func init() { commands["help"] = help }

func help(e *env, args []string, r *result) {
	var names []string
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	r.set("commands", names)
	r.set("doc", "scripts/AGENTS.md and .claude/skills/verify/SKILL.md")
}

func run(args []string, e *env) int {
	r := newResult()
	if len(args) == 0 {
		r.bad("no command", usageHint)
	} else if f, ok := commands[args[0]]; !ok {
		r.bad("unknown command "+args[0], usageHint)
	} else {
		r.set("command", args[0])
		f(e, args[1:], r)
	}
	return emit(e.out, r)
}

func emit(w io.Writer, r *result) int {
	out := map[string]any{}
	for k, v := range r.data {
		out[k] = v
	}
	if r.problems == nil {
		r.problems = []problem{}
	}
	out["ok"] = len(r.problems) == 0
	out["problems"] = r.problems
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		b = []byte(`{"ok":false,"problems":[{"msg":"cannot encode the result","hint":"report a bug in scripts/verify"}]}`)
	}
	b = append(b, '\n')
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			break
		}
		b = b[n:]
	}
	switch {
	case r.usage:
		return 2
	case len(r.problems) > 0:
		return 1
	}
	return 0
}

type opts struct {
	pos   []string
	vals  map[string][]string
	bools map[string]bool
}

func (o opts) get(k, def string) string {
	if v := o.vals[k]; len(v) > 0 {
		return v[len(v)-1]
	}
	return def
}

func parse(args []string, valueFlags, boolFlags []string, r *result) (opts, bool) {
	o := opts{vals: map[string][]string{}, bools: map[string]bool{}}
	isVal := map[string]bool{}
	for _, f := range valueFlags {
		isVal[f] = true
	}
	isBool := map[string]bool{}
	for _, f := range boolFlags {
		isBool[f] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") || a == "--" {
			o.pos = append(o.pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		switch {
		case isBool[name]:
			o.bools[name] = true
		case isVal[name]:
			if !hasVal {
				if i+1 >= len(args) {
					r.bad("--"+name+" needs a value", usageHint)
					return o, false
				}
				i++
				val = args[i]
			}
			o.vals[name] = append(o.vals[name], val)
		default:
			r.bad("unknown flag --"+name, usageHint)
			return o, false
		}
	}
	return o, true
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func outDir(e *env, o opts, name string) (string, error) {
	d := o.get("out-dir", "")
	if d == "" {
		d = filepath.Join(e.state, "out", name+"-"+e.now().UTC().Format("20060102-150405"))
	}
	d, err := filepath.Abs(d)
	if err != nil {
		return "", err
	}
	return d, os.MkdirAll(d, 0o755)
}

func selftest(e *env, args []string, r *result) {
	stdout, stderr, code := e.sys.exec(cmd{dir: e.repo, name: "go", args: []string{"test", "./scripts/verify/"}})
	r.set("output", tail(stdout+stderr, 4000))
	if code != 0 {
		r.fail("selftest failed: "+firstLine(stdout+stderr, "FAIL"), "run go test ./scripts/verify/ -v")
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func firstLine(s, want string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, want) {
			return strings.TrimSpace(l)
		}
	}
	return strings.TrimSpace(tail(s, 200))
}

func main() {
	repo, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	state := os.Getenv("VERIFY_HOME")
	if state == "" {
		state = fmt.Sprintf("/tmp/crew-verify-%d", os.Getuid())
	}
	_ = os.MkdirAll(state, 0o700)
	os.Exit(run(os.Args[1:], &env{out: os.Stdout, sys: realSys{}, repo: repo, state: state, now: time.Now}))
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return dir, nil
		}
	}
}
