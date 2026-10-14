package claude

import (
	"strconv"

	"github.com/giovaniif/crew/internal/adapters/dialog"
	"github.com/giovaniif/crew/internal/app"
)

var permissionSpec = dialog.Spec{
	Questions: []string{"Do you want to"},
	Start:     dialog.FromRule,
	Keys:      func(number int, _ string) []string { return []string{strconv.Itoa(number)} },
}

func (Adapter) PermissionPrompt(screen string) (app.PermissionPrompt, bool) {
	return dialog.Parse(screen, permissionSpec)
}
