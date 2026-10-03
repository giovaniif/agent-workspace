package domain

func ClaudeNotification(notificationType string) (HarnessEventKind, bool) {
	switch notificationType {
	case "permission_prompt":
		return EventPermissionRequest, true
	case "auth_success":
		return "", false
	}
	return EventWaitingForInput, true
}

func HookEvent(h Harness, name string) (HarnessEventKind, bool) {
	kind, ok := Spec(h).Hooks[name]
	return kind, ok
}

func HookWantsReply(name string) bool {
	return name == "UserPromptSubmit"
}

func SessionOnPane(sessions []Session, pane string) (Session, bool) {
	if pane == "" {
		return Session{}, false
	}
	for _, s := range sessions {
		if s.Pane == pane {
			return s, true
		}
	}
	return Session{}, false
}
