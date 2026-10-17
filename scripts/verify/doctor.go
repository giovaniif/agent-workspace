package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var tools = []struct{ name, hint string }{
	{"go", "install Go from go.dev/dl, the version in go.mod"},
	{"git", "install git"},
	{"tmux", "install tmux (apt install tmux or brew install tmux)"},
	{"node", "install Node 22.18 or newer"},
	{"freeze", "go install github.com/charmbracelet/freeze@latest"},
}

const chromiumProbe = `const { chromium } = require("playwright"); const fs = require("fs"); const p = chromium.executablePath(); if (!fs.existsSync(p)) { process.exit(1); } console.log(p);`

func nodeOK(v string) bool {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 22 || major == 22 && minor >= 18
}

func doctor(e *env, args []string, r *result) {
	if _, ok := parse(args, nil, nil, r); !ok {
		return
	}
	found := map[string]bool{}
	for _, t := range tools {
		if _, ok := e.sys.look(t.name); ok {
			found[t.name] = true
			continue
		}
		if t.name == "freeze" {
			if gp, _, code := e.sys.exec(cmd{name: "go", args: []string{"env", "GOPATH"}}); code == 0 {
				if _, err := os.Stat(filepath.Join(strings.TrimSpace(gp), "bin", "freeze")); err == nil {
					found[t.name] = true
					continue
				}
			}
		}
		r.fail(t.name+" is not on PATH", t.hint)
	}
	r.set("tools", found)
	if found["node"] {
		v, _, _ := e.sys.exec(cmd{name: "node", args: []string{"--version"}})
		v = strings.TrimSpace(v)
		r.set("node", v)
		if !nodeOK(v) {
			r.fail("node "+v+" is older than 22.18", "install Node 22.18 or newer (nvm install 22)")
		}
		web := filepath.Join(e.repo, "web")
		if p, _, code := e.sys.exec(cmd{dir: web, name: "node", args: []string{"-e", chromiumProbe}}); code == 0 {
			r.set("chromium", strings.TrimSpace(p))
		} else {
			r.fail("the Playwright Chromium is not installed", "cd web && npm ci && npx playwright install chromium")
		}
	}
	branch, _, code := e.sys.exec(cmd{dir: e.repo, name: "git", args: []string{"rev-parse", "--abbrev-ref", "HEAD"}})
	if code != 0 {
		r.fail("the repo "+e.repo+" is not a git checkout", "run the CLI from the crew checkout")
	} else {
		branch = strings.TrimSpace(branch)
		r.set("branch", branch)
		if branch == "main" {
			behind, _, _ := e.sys.exec(cmd{dir: e.repo, name: "git", args: []string{"rev-list", "--count", "HEAD..origin/main"}})
			if n, _ := strconv.Atoi(strings.TrimSpace(behind)); n > 0 {
				r.fail(fmt.Sprintf("main is %d commits behind origin/main", n), "git pull --ff-only")
			}
		}
	}
	l, cpus := e.sys.load()
	r.set("load", l)
	r.set("cpus", cpus)
	if cpus > 0 && l > float64(cpus) {
		r.fail(fmt.Sprintf("machine load %.1f is over %d CPUs", l, cpus), "wait for other builds or tests to finish")
	}
	for _, p := range []int{5199} {
		if !e.sys.portFree(p) {
			r.fail(fmt.Sprintf("TCP port %d is in use", p), "stop the process on it (scripts/web-screenshot uses it)")
		}
	}
	probe, err := os.MkdirTemp("/tmp", "crew-verify-probe-")
	if err != nil {
		r.fail("cannot make an isolated home under /tmp: "+err.Error(), "free space in /tmp")
	} else {
		_ = os.RemoveAll(probe)
	}
	if len(filepath.Join(e.state, "x", "crew.sock")) > 100 {
		r.fail("the state dir "+e.state+" is too long for a Unix socket", "set VERIFY_HOME to a short path under /tmp")
	}
	if st, err := loadIso(e); err == nil {
		r.set("iso", st.Home)
	}
}
