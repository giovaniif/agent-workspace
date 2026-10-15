package onboard

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/giovaniif/crew/internal/domain"
)

func (p Probe) nvimSetupFile() string { return filepath.Join(p.NvimConfigDir, "plugin", "crew.lua") }

func (p Probe) InstallNvim(context.Context) (domain.NvimSetup, error) {
	n := p.nvimSetup()
	if !n.PluginFound {
		return n, errors.New("the crew nvim plugin is not at " + n.PluginDir)
	}
	want := domain.NvimSetupFile(n.PluginDir)
	current, err := os.ReadFile(n.ConfigFile)
	switch {
	case err == nil && string(current) == want:
		return n, nil
	case err == nil && !domain.IsNvimSetupFile(string(current)):
		return n, errors.New(n.ConfigFile + " exists and was not written by crew, left untouched")
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return n, err
	}
	if err := writeAtomic(n.ConfigFile, []byte(want)); err != nil {
		return n, err
	}
	return p.nvimSetup(), nil
}

func (p Probe) RemoveNvim(context.Context) (bool, error) {
	file := p.nvimSetupFile()
	current, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !domain.IsNvimSetupFile(string(current)) {
		return false, errors.New(file + " was not written by crew, left untouched")
	}
	return true, os.Remove(file)
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".crew-tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
