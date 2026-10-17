package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type mateDoc struct {
	Name    string   `toml:"name"`
	Prompt  string   `toml:"prompt"`
	Agent   string   `toml:"agent"`
	Model   string   `toml:"model"`
	Effort  string   `toml:"effort"`
	Repos   []string `toml:"repos"`
	Owner   string   `toml:"owner"`
	Enabled *bool    `toml:"enabled"`
}

func parseMates(text, path string) ([]domain.Mate, []string) {
	var doc struct {
		Mate []mateDoc `toml:"mate"`
	}
	meta, err := toml.Decode(text, &doc)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", path, err)}
	}
	var problems []string
	for _, key := range meta.Undecoded() {
		if len(key) > 1 && key[0] == "mate" {
			problems = append(problems, fmt.Sprintf("%s: unknown mate key %s", path, key))
		}
	}
	mates := make([]domain.Mate, len(doc.Mate))
	for i, d := range doc.Mate {
		mates[i] = domain.Mate{
			Name: d.Name, Prompt: d.Prompt, Agent: d.Agent, Model: d.Model, Effort: d.Effort,
			Repos: d.Repos, Owner: d.Owner, Enabled: d.Enabled == nil || *d.Enabled,
		}
	}
	return mates, problems
}

type mateLimitsDoc struct {
	HopLimit     *int   `toml:"hop_limit"`
	TurnsPerHour *int   `toml:"turns_per_hour"`
	DailyTurns   *int   `toml:"daily_turns"`
	DailyTokens  *int64 `toml:"daily_tokens"`
	Stop         bool   `toml:"stop"`
}

func parseMateLimits(text, path string) (domain.MateLimits, []string) {
	limits := domain.DefaultMateLimits()
	var doc struct {
		Mates mateLimitsDoc `toml:"mates"`
	}
	meta, err := toml.Decode(text, &doc)
	if err != nil {
		return limits, nil
	}
	var problems []string
	for _, key := range meta.Undecoded() {
		if len(key) > 1 && key[0] == "mates" {
			problems = append(problems, fmt.Sprintf("%s: unknown key %s", path, key))
		}
	}
	set := func(key string, dst *int64, v *int64) {
		switch {
		case v == nil:
		case *v < 0:
			problems = append(problems, fmt.Sprintf("%s: mates.%s must be 0 or more", path, key))
		default:
			*dst = *v
		}
	}
	setInt := func(key string, dst *int, v *int) {
		if v == nil {
			return
		}
		n, w := int64(*dst), int64(*v)
		set(key, &n, &w)
		*dst = int(n)
	}
	setInt("hop_limit", &limits.HopLimit, doc.Mates.HopLimit)
	setInt("turns_per_hour", &limits.TurnsPerHour, doc.Mates.TurnsPerHour)
	setInt("daily_turns", &limits.DailyTurns, doc.Mates.DailyTurns)
	set("daily_tokens", &limits.DailyTokens, doc.Mates.DailyTokens)
	limits.Stop = doc.Mates.Stop
	return limits, problems
}

func (d *Daemon) registeredWorkspaces() []domain.Workspace {
	var out []domain.Workspace
	d.query(func(s *state) {
		for _, ws := range s.workspaces {
			out = append(out, ws)
		}
	})
	return out
}

func (d *Daemon) loadMates(src string) rpc.MateList {
	list := rpc.MateList{Mates: []domain.Mate{}}
	parsed, problems := parseMates(src, d.cfg.path)
	valid, invalid := domain.CheckMates(parsed, d.registeredWorkspaces())
	limits, limitProblems := parseMateLimits(src, d.cfg.path)
	list.Limits = limits
	list.Problems = append(problems, limitProblems...)
	for _, err := range invalid {
		list.Problems = append(list.Problems, fmt.Sprintf("%s: %v", d.cfg.path, err))
	}
	list.Mates = append(list.Mates, valid...)
	return list
}

func (d *Daemon) mateMethod(req rpc.Request) *rpc.Response {
	if d.cfg.path == "" {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no config.toml")
	}
	d.cfg.mu.Lock()
	defer d.cfg.mu.Unlock()
	switch req.Method {
	case rpc.MethodMateSet:
		defer d.kickMates()
		return d.mateSet(req)
	case rpc.MethodMateRemove:
		defer d.kickMates()
		return d.mateRemove(req)
	case rpc.MethodMateSetTokens, rpc.MethodMateClearTokens:
		defer d.kickMates()
		return d.mateTokens(req)
	case rpc.MethodMateClaim:
		return d.mateClaim(req)
	case rpc.MethodMatePause, rpc.MethodMateResume:
		defer d.kickMates()
		return d.mateEnable(req, req.Method == rpc.MethodMateResume)
	case rpc.MethodMateStopAll:
		defer d.kickMates()
		return d.mateStopAll(req)
	}
	src, err := readConfig(d.cfg.path)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	list := d.loadMates(src)
	if req.Method == rpc.MethodMateList {
		list.States = d.mateStates(list.Mates, list.Limits)
		return result(req.ID, list)
	}
	var p rpc.MateShowParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" needs a name")
	}
	for _, m := range list.Mates {
		if m.Name != p.Name {
			continue
		}
		if req.Method == rpc.MethodMateSetup {
			manifest := domain.MateManifest(m)
			return result(req.ID, rpc.MateSetup{Manifest: manifest, URL: domain.MateManifestURL(manifest)})
		}
		return result(req.ID, m)
	}
	return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no mate %q", p.Name))
}

func (d *Daemon) editableConfig(id uint64) (string, *rpc.Response) {
	src, err := readConfig(d.cfg.path)
	if err != nil {
		return "", errorResponse(id, rpc.CodeFailed, err.Error())
	}
	if _, err := configValues(src); err != nil {
		return "", errorResponse(id, rpc.CodeFailed, fmt.Sprintf("%s does not parse, so crew leaves it alone: %v", d.cfg.path, err))
	}
	return src, nil
}

func sameMate(a, b domain.Mate) bool {
	if len(a.Repos) == 0 {
		a.Repos = nil
	}
	if len(b.Repos) == 0 {
		b.Repos = nil
	}
	return reflect.DeepEqual(a, b)
}

func (d *Daemon) mateSet(req rpc.Request) *rpc.Response {
	var p rpc.MateSetParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "mate.set params: "+err.Error())
	}
	m := p.Mate
	if err := domain.ValidateMate(m, d.registeredWorkspaces()); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, err.Error())
	}
	src, failed := d.editableConfig(req.ID)
	if failed != nil {
		return failed
	}
	next := domain.SetMateBlock(src, m)
	parsed, problems := parseMates(next, d.cfg.path)
	found := false
	for _, x := range parsed {
		found = found || sameMate(x, m)
	}
	if !found {
		return errorResponse(req.ID, rpc.CodeFailed, fmt.Sprintf("%s has a form of %q that crew cannot edit; change it there by hand %v", d.cfg.path, m.Name, problems))
	}
	if next != src {
		if err := d.replaceConfig(src, next); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
	}
	return result(req.ID, m)
}

func (d *Daemon) mateRemove(req rpc.Request) *rpc.Response {
	var p rpc.MateRemoveParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "mate.remove needs a name")
	}
	src, failed := d.editableConfig(req.ID)
	if failed != nil {
		return failed
	}
	parsed, _ := parseMates(src, d.cfg.path)
	var gone *domain.Mate
	for i := range parsed {
		if parsed[i].Name == p.Name {
			gone = &parsed[i]
			break
		}
	}
	next := domain.RemoveMateBlock(src, p.Name)
	if gone == nil || next == src {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no mate %q", p.Name))
	}
	if err := d.replaceConfig(src, next); err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	if d.mateSecrets != nil {
		if err := d.mateSecrets.ClearMateTokens(gone.Name); err != nil {
			log.Printf("mate %s: its tokens stay on disk: %v", gone.Name, err)
		}
	}
	return result(req.ID, *gone)
}

func (d *Daemon) mateEnable(req rpc.Request, enabled bool) *rpc.Response {
	var p rpc.MateNameParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" needs a name")
	}
	src, failed := d.editableConfig(req.ID)
	if failed != nil {
		return failed
	}
	parsed, _ := parseMates(src, d.cfg.path)
	for _, m := range parsed {
		if m.Name != p.Name {
			continue
		}
		m.Enabled = enabled
		if next := domain.SetMateBlock(src, m); next != src {
			if err := d.replaceConfig(src, next); err != nil {
				return errorResponse(req.ID, rpc.CodeFailed, err.Error())
			}
		}
		return result(req.ID, m)
	}
	return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no mate %q", p.Name))
}

func (d *Daemon) mateStopAll(req rpc.Request) *rpc.Response {
	var p rpc.MateStopAllParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "mate.stop_all params: "+err.Error())
	}
	src, failed := d.editableConfig(req.ID)
	if failed != nil {
		return failed
	}
	literal := ""
	if p.Stop {
		literal = "true"
	}
	next := domain.SetTOMLValue(src, "mates.stop", literal)
	if next != src {
		if err := d.replaceConfig(src, next); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
	}
	limits, _ := parseMateLimits(next, d.cfg.path)
	return result(req.ID, limits)
}
