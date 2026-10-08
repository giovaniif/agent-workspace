package daemon

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

type StartDefaults struct {
	Model  string `toml:"model"`
	Effort string `toml:"effort"`
}

func WithStartDefaults(defaults map[domain.Harness]StartDefaults) Option {
	return func(d *Daemon) {
		d.hs.defaults = defaults
	}
}

func LoadStartDefaults(path string) (map[domain.Harness]StartDefaults, error) {
	var cfg struct {
		Defaults map[string]StartDefaults `toml:"defaults"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("defaults: %s: %w", path, err)
	}
	out := map[domain.Harness]StartDefaults{}
	for _, h := range domain.Harnesses() {
		if def, ok := cfg.Defaults[string(h)]; ok {
			out[h] = def
		}
	}
	return out, nil
}

func (d *Daemon) sessionOptions() rpc.SessionOptions {
	var maxParallel int
	var defaults map[domain.Harness]StartDefaults
	d.query(func(*state) { maxParallel, defaults = d.lc.maxParallel, d.hs.defaults })
	home, _ := os.UserHomeDir()
	out := rpc.SessionOptions{MaxParallel: cmp.Or(maxParallel, domain.DefaultMaxParallel), Home: home}
	for _, h := range domain.Harnesses() {
		if _, ok := d.hs.adapters[h]; !ok {
			continue
		}
		spec := domain.Spec(h)
		def := defaults[h]
		out.Harnesses = append(out.Harnesses, rpc.HarnessOptions{
			Harness: string(h), Name: spec.Name, Tag: spec.Tag, Models: spec.Models, Efforts: spec.Efforts,
			Model: def.Model, Effort: def.Effort,
		})
	}
	return out
}
