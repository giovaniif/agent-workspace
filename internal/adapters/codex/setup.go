package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const hooksFragment = " hook --harness codex --event "

var managedEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PermissionRequest",
	"Stop",
	"Interrupt",
	"SessionEnd",
}

var toolEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true, "PermissionRequest": true}

const TrustStep = domain.CodexTrustStep

type SetupConfig struct {
	Dir     string
	Command string
	Now     func() time.Time
}

type SetupResult struct {
	Changed bool
	Backup  string
}

func Setup(cfg SetupConfig) (SetupResult, error) {
	file, changed, err := merged(cfg)
	if err != nil || !changed {
		return SetupResult{}, err
	}
	return file.save(cfg)
}

func Installed(cfg SetupConfig) (bool, error) {
	file, changed, err := merged(cfg)
	if err != nil {
		return false, err
	}
	return file.existed && !changed, nil
}

func merged(cfg SetupConfig) (*hooksFile, bool, error) {
	file, err := loadHooksFile(cfg)
	if err != nil {
		return nil, false, err
	}
	hooks := file.hooksObject(true)
	changed := false
	for _, event := range managedEvents {
		if mergeEvent(hooks, event, cfg.Command) {
			changed = true
		}
	}
	return file, changed, nil
}

func Remove(cfg SetupConfig) (SetupResult, error) {
	file, err := loadHooksFile(cfg)
	if err != nil {
		return SetupResult{}, err
	}
	hooks := file.hooksObject(false)
	if hooks == nil {
		return SetupResult{}, nil
	}
	changed := false
	for _, event := range append([]string(nil), hooks.keys...) {
		groups, ok := hooks.vals[event].([]any)
		if !ok {
			continue
		}
		kept, removed := stripOurs(groups)
		if !removed {
			continue
		}
		changed = true
		if len(kept) == 0 {
			hooks.delete(event)
		} else {
			hooks.set(event, kept)
		}
	}
	if !changed {
		return SetupResult{}, nil
	}
	if len(hooks.keys) == 0 {
		file.root.delete("hooks")
	}
	return file.save(cfg)
}

type hooksFile struct {
	path    string
	raw     []byte
	existed bool
	root    *object
}

func loadHooksFile(cfg SetupConfig) (*hooksFile, error) {
	f := &hooksFile{path: filepath.Join(cfg.Dir, "hooks.json"), root: newObject()}
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	f.raw, f.existed = raw, true
	parsed, err := decodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON, left untouched: %w", f.path, err)
	}
	root, ok := parsed.(*object)
	if !ok {
		return nil, fmt.Errorf("%s is not a JSON object, left untouched", f.path)
	}
	f.root = root
	return f, nil
}

func (f *hooksFile) hooksObject(create bool) *object {
	if v, ok := f.root.get("hooks"); ok {
		if obj, ok := v.(*object); ok {
			return obj
		}
	}
	if !create {
		return nil
	}
	obj := newObject()
	f.root.set("hooks", obj)
	return obj
}

func (f *hooksFile) save(cfg SetupConfig) (SetupResult, error) {
	var res SetupResult
	if f.existed {
		now := time.Now
		if cfg.Now != nil {
			now = cfg.Now
		}
		res.Backup = fmt.Sprintf("%s.crew-%s.bak", f.path, now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(res.Backup, f.raw, 0o600); err != nil {
			return SetupResult{}, err
		}
	}
	res.Changed = true
	if len(f.root.keys) == 0 {
		return res, os.Remove(f.path)
	}
	out, err := encodeIndented(f.root)
	if err != nil {
		return SetupResult{}, err
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return SetupResult{}, err
	}
	tmp := f.path + ".crew-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return SetupResult{}, err
	}
	return res, os.Rename(tmp, f.path)
}

func mergeEvent(hooks *object, event, command string) bool {
	want := desiredGroup(event, command)
	var groups []any
	if v, ok := hooks.get(event); ok {
		groups, _ = v.([]any)
	}
	if isCurrent(groups, want) {
		return false
	}
	kept, _ := stripOurs(groups)
	hooks.set(event, append(kept, want))
	return true
}

func isCurrent(groups []any, want *object) bool {
	wantJSON, _ := encodeCompact(want)
	found := 0
	for _, g := range groups {
		if !groupHasOurs(g) {
			continue
		}
		found++
		gotJSON, _ := encodeCompact(g)
		if string(gotJSON) != string(wantJSON) {
			return false
		}
	}
	return found == 1
}

func desiredGroup(event, command string) *object {
	hook := newObject()
	hook.set("type", "command")
	hook.set("command", shellQuote(command)+hooksFragment+event)
	if event != "UserPromptSubmit" {
		hook.set("async", true)
	}
	group := newObject()
	if toolEvents[event] {
		group.set("matcher", ".*")
	}
	group.set("hooks", []any{hook})
	return group
}

func stripOurs(groups []any) (kept []any, removed bool) {
	kept = []any{}
	for _, g := range groups {
		obj, ok := g.(*object)
		if !ok || !groupHasOurs(g) {
			kept = append(kept, g)
			continue
		}
		removed = true
		hooks, _ := obj.vals["hooks"].([]any)
		rest := []any{}
		for _, h := range hooks {
			if !isOurs(h) {
				rest = append(rest, h)
			}
		}
		if len(rest) > 0 {
			obj.set("hooks", rest)
			kept = append(kept, obj)
		}
	}
	return kept, removed
}

func groupHasOurs(g any) bool {
	obj, ok := g.(*object)
	if !ok {
		return false
	}
	hooks, _ := obj.vals["hooks"].([]any)
	for _, h := range hooks {
		if isOurs(h) {
			return true
		}
	}
	return false
}

func isOurs(h any) bool {
	obj, ok := h.(*object)
	if !ok {
		return false
	}
	command, _ := obj.vals["command"].(string)
	return strings.Contains(command, hooksFragment)
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
