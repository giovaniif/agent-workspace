package skill

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed SKILL.md
var body []byte

const Marker = "<!-- agentws:browser-skill -->"

func Path(dir string) string {
	return filepath.Join(dir, "skills", "agentws-browser", "SKILL.md")
}

func Installed(dir string) bool {
	got, err := os.ReadFile(Path(dir))
	return err == nil && bytes.Equal(got, body)
}

func Install(dir string) (bool, error) {
	file := Path(dir)
	got, err := os.ReadFile(file)
	switch {
	case err == nil && bytes.Equal(got, body):
		return false, nil
	case err == nil && !strings.Contains(string(got), Marker):
		return false, fmt.Errorf("%s exists and agentws did not write it; move it away to install the browser skill", file)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(file, body, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func Remove(dir string) (bool, error) {
	file := Path(dir)
	got, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.Contains(string(got), Marker) {
		return false, fmt.Errorf("%s exists and agentws did not write it; it stays", file)
	}
	if err := os.Remove(file); err != nil {
		return false, err
	}
	_ = os.Remove(filepath.Dir(file))
	return true, nil
}
