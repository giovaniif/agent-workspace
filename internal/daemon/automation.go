package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const repoConfigFile = ".agentws.toml"

type automationDoc struct {
	Name     string   `toml:"name"`
	Agent    string   `toml:"agent"`
	Model    string   `toml:"model"`
	Effort   string   `toml:"effort"`
	Target   string   `toml:"target"`
	Prompt   string   `toml:"prompt"`
	Schedule string   `toml:"schedule"`
	Timezone string   `toml:"timezone"`
	On       []string `toml:"on"`
	Precheck string   `toml:"precheck"`
	Session  string   `toml:"session"`
	Enabled  *bool    `toml:"enabled"`
	Filter   struct {
		Repo   string `toml:"repo"`
		Branch string `toml:"branch"`
		Author string `toml:"author"`
	} `toml:"filter"`
}

func LoadAutomations(path string) ([]domain.Automation, []string) {
	var doc struct {
		Automation []automationDoc `toml:"automation"`
	}
	meta, err := toml.DecodeFile(path, &doc)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", path, err)}
	}
	var out []domain.Automation
	var problems []string
	for _, key := range meta.Undecoded() {
		if len(key) > 1 && key[0] == "automation" {
			problems = append(problems, fmt.Sprintf("%s: unknown automation key %s", path, key))
		}
	}
	seen := map[string]bool{}
	for _, d := range doc.Automation {
		a := domain.Automation{
			Name: d.Name, Agent: d.Agent, Model: d.Model, Effort: d.Effort, Target: d.Target,
			Prompt: d.Prompt, Schedule: d.Schedule, Timezone: d.Timezone, On: d.On,
			Precheck: d.Precheck, Session: d.Session, Filter: domain.AutomationFilter(d.Filter), Enabled: d.Enabled == nil || *d.Enabled, Source: path,
		}
		if err := domain.ValidateAutomation(a); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		if seen[a.Name] {
			problems = append(problems, fmt.Sprintf("%s: automation %q is defined twice", path, a.Name))
			continue
		}
		seen[a.Name] = true
		out = append(out, a)
	}
	return out, problems
}

func (d *Daemon) automations(repo string) rpc.AutomationList {
	list := d.loadAutomationList(repo)
	var trust map[string]string
	d.query(func(s *state) { trust = maps.Clone(s.automationTrust) })
	for i, a := range list.Automations {
		list.Automations[i].Trusted = !domain.AutomationNeedsTrust(a) || trust[domain.AutomationTrustKey(a)] == domain.AutomationHash(a)
	}
	return list
}

func (d *Daemon) loadAutomationList(repo string) rpc.AutomationList {
	var global []domain.Automation
	var problems []string
	if d.cfg.path != "" {
		global, problems = LoadAutomations(d.cfg.path)
	}
	if repo != "" {
		local, more := LoadAutomations(filepath.Join(repo, repoConfigFile))
		return rpc.AutomationList{Automations: domain.MergeAutomations(global, local), Problems: append(problems, more...)}
	}
	out := domain.MergeAutomations(global, nil)
	for _, r := range d.registeredRepos() {
		local, more := LoadAutomations(filepath.Join(r, repoConfigFile))
		out = append(out, local...)
		problems = append(problems, more...)
	}
	return rpc.AutomationList{Automations: out, Problems: problems}
}

func (d *Daemon) registeredRepos() []string {
	var repos []string
	d.query(func(s *state) {
		for _, ws := range s.workspaces {
			for _, r := range ws.Repos {
				repos = append(repos, r.Path)
			}
		}
	})
	slices.Sort(repos)
	return slices.Compact(repos)
}

func (d *Daemon) automationMethod(req rpc.Request) *rpc.Response {
	switch req.Method {
	case rpc.MethodAutomationList:
		var p rpc.AutomationListParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return errorResponse(req.ID, rpc.CodeBadRequest, "automation.list params: "+err.Error())
			}
		}
		return result(req.ID, d.automations(p.Repo))
	default:
		var p rpc.AutomationShowParams
		if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
			return errorResponse(req.ID, rpc.CodeBadRequest, "automation.show needs a name")
		}
		for _, a := range d.automations(p.Repo).Automations {
			if a.Name == p.Name {
				return result(req.ID, a)
			}
		}
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no automation %q", p.Name))
	}
}

func (d *Daemon) automationTrustMethod(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.AutomationShowParams
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" || p.Repo == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, "automation.trust needs a name and a repo"), true
	}
	a, found := d.findAutomation(p.Name, p.Repo)
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no automation %q", p.Name)), true
	}
	trust := domain.AutomationTrust{Key: domain.AutomationTrustKey(a), Hash: domain.AutomationHash(a)}
	a.Trusted = true
	ok := d.query(func(s *state) {
		s.automationTrust[trust.Key] = trust.Hash
		s.store.PutAutomationTrust(trust)
	})
	return result(req.ID, a), ok
}
