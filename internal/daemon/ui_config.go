package daemon

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/BurntSushi/toml"
)

func LoadNoMouse(path string) (bool, error) {
	var cfg struct {
		UI struct {
			Mouse *bool `toml:"mouse"`
		} `toml:"ui"`
	}
	_, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ui: %s: %w", path, err)
	}
	return cfg.UI.Mouse != nil && !*cfg.UI.Mouse, nil
}
