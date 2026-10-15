package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/giovaniif/crew/internal/domain"
)

const (
	botSuffix = ".bot"
	appSuffix = ".app"
)

type Files struct {
	Dir string
}

func (f Files) SetMateTokens(name, bot, app string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := domain.ValidateMateTokens(bot, app); err != nil {
		return err
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return err
	}
	if err := f.write(name+botSuffix, bot); err != nil {
		return err
	}
	return f.write(name+appSuffix, app)
}

func (f Files) ClearMateTokens(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	for _, suffix := range []string{botSuffix, appSuffix} {
		if err := os.Remove(filepath.Join(f.Dir, name+suffix)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (f Files) HasMateTokens(name string) (bot, app bool) {
	if checkName(name) != nil {
		return false, false
	}
	return f.exists(name + botSuffix), f.exists(name + appSuffix)
}

func (f Files) MateTokens(name string) (bot, app string, err error) {
	if err := checkName(name); err != nil {
		return "", "", err
	}
	b, err := os.ReadFile(filepath.Join(f.Dir, name+botSuffix))
	if err != nil {
		return "", "", fmt.Errorf("mate %s has no bot token", name)
	}
	a, err := os.ReadFile(filepath.Join(f.Dir, name+appSuffix))
	if err != nil {
		return "", "", fmt.Errorf("mate %s has no app token", name)
	}
	return string(b), string(a), nil
}

func (f Files) exists(file string) bool {
	info, err := os.Stat(filepath.Join(f.Dir, file))
	return err == nil && info.Mode().IsRegular()
}

func (f Files) write(file, value string) error {
	tmp, err := os.CreateTemp(f.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(f.Dir, file))
}

func checkName(name string) error {
	if !domain.ValidMateName(name) {
		return fmt.Errorf("%q is not a mate name", name)
	}
	return nil
}
