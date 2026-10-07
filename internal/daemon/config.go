package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type configCfg struct {
	mu   sync.Mutex
	path string
}

func WithConfig(path string) Option {
	return func(d *Daemon) {
		d.cfg.path = path
	}
}

func (d *Daemon) configMethod(req rpc.Request) *rpc.Response {
	if d.cfg.path == "" {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no config.toml")
	}
	d.cfg.mu.Lock()
	defer d.cfg.mu.Unlock()
	if req.Method == rpc.MethodConfigSet {
		var p rpc.ConfigSetParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, "config.set params: "+err.Error())
		}
		literal, err := domain.ConfigLiteral(p.Key, p.Value)
		if err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, err.Error())
		}
		if err := d.writeConfig(p.Key, p.Value, literal); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
		d.applyConfig()
	}
	src, err := readConfig(d.cfg.path)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error())
	}
	values, err := configValues(src)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, fmt.Sprintf("%s: %v", d.cfg.path, err))
	}
	return result(req.ID, rpc.ConfigValues{Path: d.cfg.path, Values: values})
}

func (d *Daemon) writeConfig(key, value, literal string) error {
	src, err := readConfig(d.cfg.path)
	if err != nil {
		return err
	}
	if _, err := configValues(src); err != nil {
		return fmt.Errorf("%s does not parse, so agentws leaves it alone: %w", d.cfg.path, err)
	}
	next := domain.SetTOMLValue(src, key, literal)
	if next == src {
		return nil
	}
	values, err := configValues(next)
	if err != nil || values[key] != value {
		return fmt.Errorf("%s sets %s in a form agentws cannot edit; change it there by hand", d.cfg.path, key)
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(d.cfg.path); err == nil {
		mode = info.Mode().Perm()
		backup := d.cfg.path + ".agentws-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
		if err := os.WriteFile(backup, []byte(src), 0o600); err != nil {
			return fmt.Errorf("back up %s: %w", d.cfg.path, err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.cfg.path), ".config.toml-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(next); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), d.cfg.path)
}

func (d *Daemon) applyConfig() {
	defaults, errDefaults := LoadStartDefaults(d.cfg.path)
	maxParallel, errParallel := LoadMaxParallel(d.cfg.path)
	awayAfter, errAway := LoadAwayAfter(d.cfg.path)
	d.query(func(s *state) {
		if errDefaults == nil {
			d.hs.defaults = defaults
		}
		if errParallel == nil {
			d.lc.maxParallel = maxParallel
		}
		if errAway == nil && d.presence.activity != nil {
			s.presence.AwayAfter = awayAfter
		}
	})
}

func readConfig(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func configValues(src string) (map[string]string, error) {
	var tree map[string]any
	if _, err := toml.Decode(src, &tree); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, key := range domain.ConfigKeys() {
		var node any = tree
		for _, part := range strings.Split(key, ".") {
			table, ok := node.(map[string]any)
			if !ok {
				node = nil
				break
			}
			node = table[part]
		}
		switch v := node.(type) {
		case string:
			out[key] = v
		case int64:
			out[key] = strconv.FormatInt(v, 10)
		}
	}
	return out, nil
}
