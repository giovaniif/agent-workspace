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

type artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type proof struct {
	Ref         string     `json:"ref"`
	Kind        string     `json:"kind"`
	Note        string     `json:"note,omitempty"`
	Viewport    string     `json:"viewport,omitempty"`
	Run         string     `json:"run,omitempty"`
	Exit        *int       `json:"exit,omitempty"`
	APIFailures []string   `json:"api_failures,omitempty"`
	Regression  string     `json:"regression,omitempty"`
	Artifacts   []artifact `json:"artifacts,omitempty"`
	Checkout    string     `json:"checkout"`
	At          time.Time  `json:"at"`
}

type bundle struct {
	Claim   string    `json:"claim"`
	Repo    string    `json:"repo"`
	Main    string    `json:"main"`
	Base    string    `json:"base"`
	Head    string    `json:"head"`
	Files   []string  `json:"files"`
	Created time.Time `json:"created"`
	Proofs  []proof   `json:"proofs"`
}

var proofKinds = map[string]bool{"repro": true, "fix": true, "test": true, "design": true, "regression": true, "note": true}

func bundlePath(dir string) string { return filepath.Join(dir, "evidence.json") }

func loadBundle(dir string) (bundle, error) {
	var b bundle
	raw, err := os.ReadFile(bundlePath(dir))
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

func saveBundle(dir string, b bundle) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(bundlePath(dir), raw, 0o644)
}

func (e *env) git(dir string, args ...string) (string, bool) {
	out, _, code := e.sys.exec(cmd{dir: dir, name: "git", args: args})
	return strings.TrimSpace(out), code == 0
}

func evidence(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("evidence needs init, add or verify", "go run ./scripts/verify evidence init <dir> --claim '...' --repo .")
		return
	}
	o, ok := parse(args[1:], []string{"claim", "repo", "head", "main", "ref", "kind", "note", "viewport", "artifact", "api-failure", "regression", "run", "exit"}, nil, r)
	if !ok {
		return
	}
	if len(o.pos) != 1 {
		r.bad("evidence "+args[0]+" needs the bundle dir", "go run ./scripts/verify evidence "+args[0]+" <dir>")
		return
	}
	dir, _ := filepath.Abs(o.pos[0])
	r.set("bundle", dir)
	switch args[0] {
	case "init":
		evInit(e, dir, o, r)
	case "add":
		evAdd(e, dir, o, r)
	case "verify":
		evVerify(e, dir, r)
	default:
		r.bad("unknown evidence command "+args[0], "use init, add or verify")
	}
}

func evInit(e *env, dir string, o opts, r *result) {
	claim := o.get("claim", "")
	if claim == "" {
		r.bad("init needs --claim", "restate the problem: what the user sees and why it is wrong")
		return
	}
	repo, _ := filepath.Abs(o.get("repo", e.repo))
	main := o.get("main", "origin/main")
	head, ok := e.git(repo, "rev-parse", o.get("head", "HEAD")+"^{commit}")
	if !ok {
		r.fail("unknown head "+o.get("head", "HEAD"), "give --head <sha> of the fix")
		return
	}
	base, ok := e.git(repo, "merge-base", main, head)
	if !ok {
		r.fail("no merge-base of "+main+" and "+head, "git fetch origin main")
		return
	}
	files, _ := e.git(repo, "diff", "--name-only", base, head)
	b := bundle{Claim: claim, Repo: repo, Main: main, Base: base, Head: head, Files: strings.Fields(files), Created: e.now(), Proofs: []proof{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.fail(err.Error(), "check the bundle dir")
		return
	}
	if err := saveBundle(dir, b); err != nil {
		r.fail(err.Error(), "check the bundle dir")
		return
	}
	r.set("base", base)
	r.set("head", head)
	r.set("files", b.Files)
}

func evAdd(e *env, dir string, o opts, r *result) {
	b, err := loadBundle(dir)
	if err != nil {
		r.fail("no bundle in "+dir, "run evidence init first")
		return
	}
	p := proof{Ref: o.get("ref", ""), Kind: o.get("kind", ""), Note: o.get("note", ""), Viewport: o.get("viewport", ""), APIFailures: o.vals["api-failure"], Regression: o.get("regression", ""), At: e.now()}
	if p.Ref != "base" && p.Ref != "head" {
		r.bad("--ref must be base or head", "--ref head")
		return
	}
	if !proofKinds[p.Kind] {
		r.bad("--kind must be repro, fix, test, design, regression or note", "--kind fix")
		return
	}
	if p.Viewport != "" && p.Viewport != "mobile" && p.Viewport != "desktop" && p.Viewport != "tui" && p.Viewport != "mac" {
		r.bad("--viewport must be mobile, desktop, tui or mac", "--viewport mobile")
		return
	}
	if p.Kind == "regression" && p.Regression == "" {
		r.bad("a regression proof needs --regression <name>", "--regression 'sidebar order'")
		return
	}
	want := b.Base
	if p.Ref == "head" {
		want = b.Head
	}
	at, _ := e.git(b.Repo, "rev-parse", "HEAD")
	if at != want {
		r.fail(fmt.Sprintf("the checkout is at %s, not the %s %s", short(at), p.Ref, short(want)), "check out "+want+", build crew again and restart the isolated crew")
		return
	}
	p.Checkout = at
	if v := o.get("exit", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			r.bad("--exit must be a number", "--exit 1")
			return
		}
		p.Exit = &n
	}
	if run := o.get("run", ""); run != "" {
		out, errs, code := e.sys.exec(cmd{dir: b.Repo, name: "sh", args: []string{"-c", run}})
		p.Run, p.Exit = run, &code
		log := filepath.Join(dir, fmt.Sprintf("%02d-%s-%s.log", len(b.Proofs), p.Ref, p.Kind))
		_ = os.WriteFile(log, []byte(out+errs), 0o644)
		sum, _ := fileHash(log)
		p.Artifacts = append(p.Artifacts, artifact{log, sum})
		r.set("exit", code)
	}
	for _, a := range o.vals["artifact"] {
		abs, _ := filepath.Abs(a)
		sum, err := fileHash(abs)
		if err != nil {
			r.fail("cannot hash "+a+": "+err.Error(), "give a file that exists")
			return
		}
		p.Artifacts = append(p.Artifacts, artifact{abs, sum})
	}
	b.Proofs = append(b.Proofs, p)
	if err := saveBundle(dir, b); err != nil {
		r.fail(err.Error(), "check the bundle dir")
		return
	}
	r.set("proofs", len(b.Proofs))
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func isUIFile(f string) bool {
	if strings.HasSuffix(f, "_test.go") || strings.Contains(f, ".test.") || strings.Contains(f, "/testdata/") {
		return false
	}
	return strings.HasPrefix(f, "internal/tui/") || strings.HasPrefix(f, "web/src/") || strings.HasPrefix(f, "macos/") && strings.HasSuffix(f, ".swift")
}

func isWebFile(f string) bool { return strings.HasPrefix(f, "web/src/") && isUIFile(f) }

func evVerify(e *env, dir string, r *result) {
	b, err := loadBundle(dir)
	if err != nil {
		r.fail("no bundle in "+dir, "run evidence init first")
		return
	}
	r.set("claim", b.Claim)
	r.set("base", b.Base)
	r.set("head", b.Head)
	ui, webUI := false, false
	for _, f := range b.Files {
		ui = ui || isUIFile(f)
		webUI = webUI || isWebFile(f)
	}
	r.set("ui_change", ui)
	has := func(ref, kind string, ok func(p proof) bool) bool {
		for _, p := range b.Proofs {
			if p.Ref == ref && p.Kind == kind && (ok == nil || ok(p)) {
				return true
			}
		}
		return false
	}
	note := func(prefix string) bool {
		for _, p := range b.Proofs {
			if p.Kind == "note" && strings.HasPrefix(p.Note, prefix) {
				return true
			}
		}
		return false
	}
	if !has("base", "repro", nil) && !note("no-repro:") {
		r.fail("no base repro", "record the bug on base: evidence add <dir> --ref base --kind repro --artifact <shot>, or a --kind note --note 'no-repro: <why>'")
	}
	if !has("head", "fix", nil) {
		r.fail("no head fix", "record the fix on head: evidence add <dir> --ref head --kind fix --viewport <v> --artifact <shot>")
	} else if webUI {
		for _, v := range []string{"mobile", "desktop"} {
			if !has("head", "fix", func(p proof) bool { return p.Viewport == v }) {
				r.fail("no head fix at the "+v+" viewport", "run web shot --viewport both on head and add each shot")
			}
		}
	}
	baseFails := map[string]bool{}
	for _, p := range b.Proofs {
		if p.Ref == "base" {
			for _, f := range p.APIFailures {
				baseFails[f] = true
			}
		}
	}
	for _, p := range b.Proofs {
		if p.Ref != "head" {
			continue
		}
		for _, f := range p.APIFailures {
			if !baseFails[f] {
				r.fail("failed API call on head only: "+f, "fix the call, or show that base fails the same way")
				baseFails[f] = true
			}
		}
	}
	if !has("head", "test", func(p proof) bool { return p.Exit != nil && *p.Exit == 0 }) {
		r.fail("no passing head test run", "evidence add <dir> --ref head --kind test --run 'go test ./...'")
	}
	if !has("base", "test", func(p proof) bool { return p.Exit != nil && *p.Exit != 0 }) && !note("no-failing-test:") {
		r.fail("no failing test first: the regression test does not fail on base", "check out base with the test commit and run it: evidence add <dir> --ref base --kind test --run '<test>', or note 'no-failing-test: <why>'")
	}
	if ui && !has("head", "design", func(p proof) bool { return p.Exit == nil || *p.Exit == 0 }) {
		r.fail("a UI change has no clean design-check", "evidence add <dir> --ref head --kind design --run 'go run ./scripts/verify design-check'")
	}
	baseRegs := map[string]bool{}
	for _, p := range b.Proofs {
		if p.Ref == "base" && p.Kind == "regression" {
			baseRegs[p.Regression] = true
		}
	}
	for _, p := range b.Proofs {
		if p.Ref == "head" && p.Kind == "regression" && !baseRegs[p.Regression] {
			r.fail("regression on head only: "+p.Regression, "fix the regression before the PR")
		}
	}
	for _, p := range b.Proofs {
		for _, a := range p.Artifacts {
			sum, err := fileHash(a.Path)
			if err != nil {
				r.fail("artifact "+a.Path+" is missing", "keep the artifacts until the gate reads the bundle")
			} else if sum != a.SHA256 {
				r.fail("artifact "+a.Path+" hash changed after it was recorded", "record the proof again")
			}
		}
	}
	if at, _ := e.git(b.Repo, "rev-parse", "HEAD"); at != b.Head {
		r.fail(fmt.Sprintf("the checkout is at %s, not the head %s", short(at), short(b.Head)), "check out the head again, or init a new bundle for the new head")
	}
	r.set("proofs", len(b.Proofs))
}
