package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var HookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification",
	"PermissionRequest", "Stop", "SubagentStart", "SubagentStop", "SessionEnd",
}

const hookMarker = " hook --harness claude --event "

func BackupPath(settings string) string { return settings + ".crew-backup" }

func HookCommand(bin, event string) string { return shellQuote(bin) + hookMarker + event }

func Setup(path, bin string) error {
	p, err := plan(path, bin)
	if err != nil {
		return err
	}
	if p.exists && bytes.Equal(p.before, p.stripped) {
		if _, err := os.Stat(BackupPath(path)); errors.Is(err, os.ErrNotExist) {
			if err := writeFile(BackupPath(path), p.original); err != nil {
				return err
			}
		}
	}
	if bytes.Equal(p.out, p.original) {
		return nil
	}
	return writeFile(path, p.out)
}

func Installed(path, bin string) (bool, error) {
	p, err := plan(path, bin)
	if err != nil {
		return false, err
	}
	return p.exists && bytes.Equal(p.out, p.original), nil
}

type setupPlan struct {
	exists                          bool
	original, before, stripped, out []byte
}

func plan(path, bin string) (setupPlan, error) {
	original, err := os.ReadFile(path)
	p := setupPlan{exists: err == nil, original: original}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	doc, err := parse(original)
	if err != nil {
		return p, fmt.Errorf("%s: %w", path, err)
	}
	if p.before, err = doc.encode(); err != nil {
		return p, err
	}
	chain, err := strip(doc)
	if err != nil {
		return p, fmt.Errorf("%s: %w", path, err)
	}
	if p.stripped, err = doc.encode(); err != nil {
		return p, err
	}
	if err := install(doc, bin, chain); err != nil {
		return p, err
	}
	p.out, err = doc.encode()
	return p, err
}

func Remove(path string) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	doc, err := parse(current)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if _, err := strip(doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	stripped, err := doc.encode()
	if err != nil {
		return err
	}
	backup, err := os.ReadFile(BackupPath(path))
	switch {
	case err == nil:
		if same, _ := equalJSON(stripped, backup); same {
			stripped = backup
		}
	case errors.Is(err, os.ErrNotExist):
		if same, _ := equalJSON(stripped, current); same {
			return nil
		}
		if len(doc.members) == 0 {
			return os.Remove(path)
		}
	default:
		return err
	}
	if !bytes.Equal(stripped, current) {
		if err := writeFile(path, stripped); err != nil {
			return err
		}
	}
	if err := os.Remove(BackupPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func install(doc *object, bin, chain string) error {
	hooks, err := doc.object("hooks")
	if err != nil {
		return err
	}
	for _, event := range HookEvents {
		groups, err := hooks.array(event)
		if err != nil {
			return err
		}
		group, err := marshal(hookGroup{Hooks: []hookEntry{{Type: "command", Command: HookCommand(bin, event)}}})
		if err != nil {
			return err
		}
		if err := hooks.set(event, append(groups, group)); err != nil {
			return err
		}
	}
	if err := doc.set("hooks", hooks); err != nil {
		return err
	}
	line, err := doc.object("statusLine")
	if err != nil {
		return err
	}
	if _, ok := line.get("type"); !ok {
		if err := line.set("type", "command"); err != nil {
			return err
		}
	}
	if err := line.set("command", StatusLineCommand(bin, chain)); err != nil {
		return err
	}
	return doc.set("statusLine", line)
}

func strip(doc *object) (chain string, err error) {
	if hooks, ok, err := doc.existingObject("hooks"); err != nil {
		return "", err
	} else if ok {
		if err := stripHooks(hooks); err != nil {
			return "", err
		}
		if len(hooks.members) == 0 {
			doc.del("hooks")
		} else if err := doc.set("hooks", hooks); err != nil {
			return "", err
		}
	}
	line, ok, err := doc.existingObject("statusLine")
	if err != nil || !ok {
		return "", err
	}
	raw, _ := line.get("command")
	var cmd string
	_ = json.Unmarshal(raw, &cmd)
	chain, ours := ChainedStatusLine(cmd)
	if !ours {
		return cmd, nil
	}
	if chain == "" {
		doc.del("statusLine")
		return "", nil
	}
	if err := line.set("command", chain); err != nil {
		return "", err
	}
	return chain, doc.set("statusLine", line)
}

func stripHooks(hooks *object) error {
	for _, m := range append([]member(nil), hooks.members...) {
		var groups []json.RawMessage
		if err := json.Unmarshal(m.val, &groups); err != nil {
			continue
		}
		kept := groups[:0]
		for _, g := range groups {
			if !isOurs(g) {
				kept = append(kept, g)
			}
		}
		if len(kept) == len(groups) {
			continue
		}
		if len(kept) == 0 {
			hooks.del(m.key)
			continue
		}
		if err := hooks.set(m.key, kept); err != nil {
			return err
		}
	}
	return nil
}

type hookGroup struct {
	Hooks []hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

func isOurs(group json.RawMessage) bool {
	var g hookGroup
	if err := json.Unmarshal(group, &g); err != nil || len(g.Hooks) == 0 {
		return false
	}
	for _, h := range g.Hooks {
		if !strings.Contains(h.Command, hookMarker) {
			return false
		}
	}
	return true
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := path + ".crew-tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func equalJSON(a, b []byte) (bool, error) {
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false, err
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya), nil
}
