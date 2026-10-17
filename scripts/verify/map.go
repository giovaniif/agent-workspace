package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type recipe struct {
	Label    string   `json:"label"`
	Manual   string   `json:"manual,omitempty"`
	Needs    []string `json:"needs,omitempty"`
	Commands []string `json:"commands"`
}

type area struct {
	Name    string
	Setup   []string
	Cleanup []string
	Recipes []recipe
}

var manualLabel = regexp.MustCompile(`^(.*?)\s*\(manual:\s*(.*)\)\s*$`)

func parseArea(name, text string) area {
	a := area{Name: name}
	section := ""
	var cur *recipe
	inCode := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			if trimmed == "" {
				continue
			}
			switch {
			case section == "setup":
				a.Setup = append(a.Setup, trimmed)
			case section == "cleanup":
				a.Cleanup = append(a.Cleanup, trimmed)
			case section == "recipes" && cur != nil:
				cur.Commands = append(cur.Commands, trimmed)
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "## "):
			section = strings.ToLower(strings.TrimSpace(line[3:]))
			cur = nil
		case strings.HasPrefix(line, "### ") && section == "recipes":
			label := strings.TrimSpace(line[4:])
			rc := recipe{Label: label}
			if m := manualLabel.FindStringSubmatch(label); m != nil {
				rc.Label, rc.Manual = m[1], m[2]
			}
			a.Recipes = append(a.Recipes, rc)
			cur = &a.Recipes[len(a.Recipes)-1]
		case strings.HasPrefix(trimmed, "Needs:") && cur != nil:
			cur.Needs = append(cur.Needs, strings.FieldsFunc(trimmed[len("Needs:"):], func(r rune) bool { return r == ',' || r == ' ' || r == '`' })...)
		}
	}
	return a
}

func featuresDir(e *env, o opts) string {
	return o.get("features", filepath.Join(e.repo, "features"))
}

func loadAreas(dir string) (map[string]area, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	areas := map[string]area{}
	for _, en := range entries {
		n := en.Name()
		if en.IsDir() || !strings.HasSuffix(n, ".md") || n == "README.md" || n == "AGENTS.md" || n == "CLAUDE.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(n, ".md")
		areas[name] = parseArea(name, string(b))
	}
	return areas, nil
}

type mapRecord struct {
	Label  string    `json:"label"`
	Result string    `json:"result"`
	Why    string    `json:"why,omitempty"`
	Output string    `json:"output,omitempty"`
	At     time.Time `json:"at"`
}

func mapResultsPath(e *env) string { return filepath.Join(e.state, "map-results.json") }

func loadMapResults(e *env) map[string]map[string]mapRecord {
	all := map[string]map[string]mapRecord{}
	if b, err := os.ReadFile(mapResultsPath(e)); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	return all
}

func mapCmd(e *env, args []string, r *result) {
	if len(args) == 0 {
		r.bad("map needs status or run", "go run ./scripts/verify map status")
		return
	}
	o, ok := parse(args[1:], []string{"recipe", "features"}, nil, r)
	if !ok {
		return
	}
	areas, err := loadAreas(featuresDir(e, o))
	if err != nil {
		r.fail("cannot read the feature map: "+err.Error(), "run from the repo root; the map is in features/")
		return
	}
	switch args[0] {
	case "status":
		mapStatus(e, areas, r)
	case "run":
		if len(o.pos) != 1 {
			r.bad("map run needs one area", "go run ./scripts/verify map status lists the areas")
			return
		}
		a, ok := areas[o.pos[0]]
		if !ok {
			r.bad("unknown area "+o.pos[0], "go run ./scripts/verify map status lists the areas")
			return
		}
		mapRun(e, a, o.get("recipe", ""), r)
	default:
		r.bad("unknown map command "+args[0], "use status or run")
	}
}

func mapStatus(e *env, areas map[string]area, r *result) {
	results := loadMapResults(e)
	var names []string
	for n := range areas {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []map[string]any
	for _, n := range names {
		counts := map[string]int{}
		for _, rc := range areas[n].Recipes {
			if rec, ok := results[n][rc.Label]; ok {
				counts[rec.Result]++
			} else {
				counts["not-run"]++
			}
		}
		out = append(out, map[string]any{"area": n, "recipes": len(areas[n].Recipes), "pass": counts["pass"], "fail": counts["fail"], "needs-var": counts["needs-var"], "manual": counts["manual"], "not-run": counts["not-run"]})
	}
	if out == nil {
		out = []map[string]any{}
	}
	r.set("areas", out)
}

func (e *env) shell(line string) (string, int) {
	out, errs, code := e.sys.exec(cmd{dir: e.repo, name: "sh", args: []string{"-c", line}})
	return out + errs, code
}

func mapRun(e *env, a area, only string, r *result) {
	var recipes []recipe
	for _, rc := range a.Recipes {
		if only == "" || rc.Label == only {
			recipes = append(recipes, rc)
		}
	}
	if len(recipes) == 0 {
		r.bad("no recipe "+only+" in "+a.Name, "go run ./scripts/verify map status")
		return
	}
	defer func() {
		for _, c := range a.Cleanup {
			if out, code := e.shell(c); code != 0 {
				r.fail("cleanup failed: "+c, tail(out, 300))
			}
		}
	}()
	for _, c := range a.Setup {
		if out, code := e.shell(c); code != 0 {
			r.fail("setup failed: "+c, tail(out, 300))
			return
		}
	}
	results := loadMapResults(e)
	if results[a.Name] == nil {
		results[a.Name] = map[string]mapRecord{}
	}
	var recs []mapRecord
	for _, rc := range recipes {
		rec := mapRecord{Label: rc.Label, Result: "pass", At: e.now()}
		var missing []string
		for _, v := range rc.Needs {
			if e.sys.getenv(v) == "" {
				missing = append(missing, v)
			}
		}
		switch {
		case rc.Manual != "":
			rec.Result, rec.Why = "manual", rc.Manual
		case len(missing) > 0:
			rec.Result, rec.Why = "needs-var", strings.Join(missing, ", ")
		case len(rc.Commands) == 0:
			rec.Result, rec.Why = "fail", "the recipe has no commands"
		default:
			for _, c := range rc.Commands {
				out, code := e.shell(c)
				if code != 0 {
					rec.Result, rec.Why, rec.Output = "fail", c, tail(out, 2000)
					break
				}
			}
		}
		if rec.Result == "fail" {
			r.fail(a.Name+": "+rc.Label+" failed at "+rec.Why, "a map drift: fix the recipe; a product bug: keep it failing, add a Gotcha and open an issue")
		}
		results[a.Name][rc.Label] = rec
		recs = append(recs, rec)
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	_ = os.WriteFile(mapResultsPath(e), b, 0o644)
	r.set("area", a.Name)
	r.set("recipes", recs)
}
