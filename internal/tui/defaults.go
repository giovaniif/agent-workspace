package tui

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/BurntSushi/toml"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

type Defaults struct {
	Model  string `toml:"model"`
	Effort string `toml:"effort"`
	// why: keyed by short model name ("opus-5.5"), for when a harness sets effort
	// per model rather than once.
	EffortByModel map[string]string `toml:"-"`
}

func LoadDefaults(path string) (map[domain.Harness]Defaults, error) {
	var cfg struct {
		Defaults map[string]Defaults `toml:"defaults"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tui: %s: %w", path, err)
	}
	out := map[domain.Harness]Defaults{}
	for _, h := range domain.Harnesses() {
		if d, ok := cfg.Defaults[string(h)]; ok {
			out[h] = d
		}
	}
	return out, nil
}

func LoadFallback(path string) (domain.FallbackConfig, error) {
	var cfg struct {
		Fallback struct {
			Threshold int               `toml:"threshold"`
			Models    map[string]string `toml:"models"`
			Efforts   map[string]string `toml:"efforts"`
		} `toml:"fallback"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.FallbackConfig{}, nil
	}
	if err != nil {
		return domain.FallbackConfig{}, fmt.Errorf("tui: %s: %w", path, err)
	}
	f := cfg.Fallback
	return domain.FallbackConfig{Threshold: f.Threshold, Models: f.Models, Efforts: f.Efforts}, nil
}
