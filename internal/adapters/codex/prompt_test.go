package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/app"
)

func promptScreen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "prompt", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPermissionPromptParsesTheCapturedDialogs(t *testing.T) {
	cases := []struct {
		fixture string
		text    string
		choices []app.PermissionChoice
	}{
		{
			"command.txt",
			"Would you like to run the following command?\n\nReason: the file does not exist yet\n\n$ touch notes.txt",
			[]app.PermissionChoice{
				{ID: "1", Label: "Yes, proceed", Keys: []string{"y"}},
				{ID: "2", Label: "Yes, and don't ask again for commands that start with `touch`", Keys: []string{"p"}},
				{ID: "3", Label: "No, and tell Codex what to do differently", Keys: []string{"Escape"}},
			},
		},
		{
			"edit.txt",
			"Would you like to make the following edits?\n\nReason: this change is outside the workspace\n\nsrc/notes.txt (+1 -1)\n\n1 -old line\n1 +new line",
			[]app.PermissionChoice{
				{ID: "1", Label: "Yes, proceed", Keys: []string{"y"}},
				{ID: "2", Label: "Yes, and don't ask again for these files", Keys: []string{"a"}},
				{ID: "3", Label: "No, and tell Codex what to do differently", Keys: []string{"Escape"}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			got, ok := Adapter{}.PermissionPrompt(promptScreen(t, c.fixture))
			if !ok {
				t.Fatal("dialog not recognised")
			}
			if got.Text != c.text {
				t.Fatalf("text %q, want %q", got.Text, c.text)
			}
			if !reflect.DeepEqual(got.Choices, c.choices) {
				t.Fatalf("choices %+v, want %+v", got.Choices, c.choices)
			}
		})
	}
}

func TestPermissionPromptIgnoresScreensWithoutAnActiveDialog(t *testing.T) {
	for _, fixture := range []string{"idle.txt", "answered.txt", "unknown.txt"} {
		t.Run(fixture, func(t *testing.T) {
			if got, ok := (Adapter{}).PermissionPrompt(promptScreen(t, fixture)); ok {
				t.Fatalf("guessed %+v", got)
			}
		})
	}
}

func TestPermissionPromptFallsBackToTheNumberWhenNoShortcutIsShown(t *testing.T) {
	screen := "  Would you like to run the following command?\n\n  $ ls\n\n› 1. Yes, proceed\n  2. No, and tell Codex what to do differently\n"
	got, ok := Adapter{}.PermissionPrompt(screen)
	if !ok {
		t.Fatal("dialog not recognised")
	}
	want := []app.PermissionChoice{
		{ID: "1", Label: "Yes, proceed", Keys: []string{"1"}},
		{ID: "2", Label: "No, and tell Codex what to do differently", Keys: []string{"2"}},
	}
	if !reflect.DeepEqual(got.Choices, want) {
		t.Fatalf("choices %+v, want %+v", got.Choices, want)
	}
}
