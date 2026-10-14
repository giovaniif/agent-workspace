package codex

import (
	"strconv"

	"github.com/giovaniif/crew/internal/adapters/dialog"
	"github.com/giovaniif/crew/internal/app"
)

var permissionSpec = dialog.Spec{
	Questions: []string{"Would you like to run the following command?", "Would you like to make the following edits?"},
	Start:     dialog.FromQuestion,
	Keys: func(number int, shortcut string) []string {
		switch shortcut {
		case "":
			return []string{strconv.Itoa(number)}
		case "esc":
			return []string{"Escape"}
		}
		return []string{shortcut}
	},
}

func (Adapter) PermissionPrompt(screen string) (app.PermissionPrompt, bool) {
	return dialog.Parse(screen, permissionSpec)
}
