package claude_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/claude"
	"github.com/giovaniif/crew/internal/app"
)

func screen(t *testing.T, name string) string {
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
			"Bash command\n\ntouch notes.txt\nCreate an empty notes.txt file\n\nDo you want to proceed?",
			[]app.PermissionChoice{
				{ID: "1", Label: "Yes", Keys: []string{"1"}},
				{ID: "2", Label: "Yes, and don't ask again for touch commands in /tmp/project", Keys: []string{"2"}},
				{ID: "3", Label: "No, and tell Claude what to do differently", Keys: []string{"3"}},
			},
		},
		{
			"edit.txt",
			"Edit file\nsrc/notes.txt\n\n1 -old line\n1 +new line\n\nDo you want to make this edit to notes.txt?",
			[]app.PermissionChoice{
				{ID: "1", Label: "Yes", Keys: []string{"1"}},
				{ID: "2", Label: "Yes, allow all edits during this session", Keys: []string{"2"}},
				{ID: "3", Label: "No, and tell Claude what to do differently", Keys: []string{"3"}},
			},
		},
		{
			"webfetch.txt",
			"Fetch\n\nhttps://example.com/docs\nClaude wants to fetch content from example.com\n\nDo you want to allow Claude to fetch this content?",
			[]app.PermissionChoice{
				{ID: "1", Label: "Yes", Keys: []string{"1"}},
				{ID: "2", Label: "Yes, and don't ask again for example.com", Keys: []string{"2"}},
				{ID: "3", Label: "No, and tell Claude what to do differently", Keys: []string{"3"}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			got, ok := claude.Adapter{}.PermissionPrompt(screen(t, c.fixture))
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
			if got, ok := (claude.Adapter{}).PermissionPrompt(screen(t, fixture)); ok {
				t.Fatalf("guessed %+v", got)
			}
		})
	}
}

func TestPermissionPromptIgnoresAnEmptyScreen(t *testing.T) {
	if _, ok := (claude.Adapter{}).PermissionPrompt(""); ok {
		t.Fatal("recognised an empty screen")
	}
}
