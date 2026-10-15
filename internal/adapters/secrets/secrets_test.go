package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/secrets"
)

const (
	bot = "xoxb-111-222-botsecret"
	app = "xapp-1-A0-333-appsecret"
)

func newFiles(t *testing.T) (secrets.Files, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secrets")
	return secrets.Files{Dir: dir}, dir
}

func TestSetWritesBothTokensWithMode600InADirectoryOnlyTheOwnerCanOpen(t *testing.T) {
	f, dir := newFiles(t)
	if err := f.SetMateTokens("ada", bot, app); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"ada.bot": bot, "ada.app": app} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v", name, info.Mode().Perm())
		}
		got, _ := os.ReadFile(path)
		if string(got) != want {
			t.Errorf("%s holds %q", name, got)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir has mode %v", info.Mode().Perm())
	}
}

func TestSetReplacesOldTokensAndFixesALooseMode(t *testing.T) {
	f, dir := newFiles(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ada.bot"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.SetMateTokens("ada", bot, app); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "ada.bot"))
	got, _ := os.ReadFile(filepath.Join(dir, "ada.bot"))
	if info.Mode().Perm() != 0o600 || string(got) != bot {
		t.Errorf("mode %v, content %q", info.Mode().Perm(), got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestHasReportsEachTokenWithoutReadingItBack(t *testing.T) {
	f, dir := newFiles(t)
	if b, a := f.HasMateTokens("ada"); b || a {
		t.Error("tokens exist before they are set")
	}
	if err := f.SetMateTokens("ada", bot, app); err != nil {
		t.Fatal(err)
	}
	if b, a := f.HasMateTokens("ada"); !b || !a {
		t.Errorf("bot %v app %v", b, a)
	}
	if err := os.Remove(filepath.Join(dir, "ada.app")); err != nil {
		t.Fatal(err)
	}
	if b, a := f.HasMateTokens("ada"); !b || a {
		t.Errorf("bot %v app %v", b, a)
	}
}

func TestClearRemovesBothFilesAndIsQuietWhenThereAreNone(t *testing.T) {
	f, dir := newFiles(t)
	if err := f.ClearMateTokens("ada"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetMateTokens("ada", bot, app); err != nil {
		t.Fatal(err)
	}
	if err := f.SetMateTokens("bob", bot, app); err != nil {
		t.Fatal(err)
	}
	if err := f.ClearMateTokens("ada"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 || entries[0].Name() != "bob.app" {
		t.Errorf("left %v", entries)
	}
}

func TestANameThatIsNotAMateNameNeverReachesTheFileSystem(t *testing.T) {
	f, dir := newFiles(t)
	for _, name := range []string{"../evil", "a/b", "", ".", "A"} {
		if err := f.SetMateTokens(name, bot, app); err == nil {
			t.Errorf("%q: set accepted it", name)
		}
		if err := f.ClearMateTokens(name); err == nil {
			t.Errorf("%q: clear accepted it", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.bot")); err == nil {
		t.Error("a file escaped the directory")
	}
}

func TestSetRefusesTokensWithTheWrongPrefix(t *testing.T) {
	f, dir := newFiles(t)
	if err := f.SetMateTokens("ada", app, bot); err == nil {
		t.Fatal("swapped tokens were accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("wrote %v", entries)
	}
}

func TestMateTokensReadsBothTokensForTheDaemon(t *testing.T) {
	f, _ := newFiles(t)
	if _, _, err := f.MateTokens("reviewer"); err == nil {
		t.Error("MateTokens of a mate with no tokens gave no error")
	}
	if err := f.SetMateTokens("reviewer", bot, app); err != nil {
		t.Fatal(err)
	}
	gotBot, gotApp, err := f.MateTokens("reviewer")
	if err != nil || gotBot != bot || gotApp != app {
		t.Errorf("MateTokens = %q, %q, %v", gotBot, gotApp, err)
	}
	if _, _, err := f.MateTokens("../x"); err == nil {
		t.Error("a bad name gave no error")
	}
}
