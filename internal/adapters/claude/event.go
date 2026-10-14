package claude

import (
	"encoding/json"

	"github.com/giovaniif/crew/internal/domain"
)

func Event(name string, payload []byte) (domain.HarnessEventKind, bool) {
	if name != "Notification" {
		return domain.HookEvent(domain.HarnessClaude, name)
	}
	var n struct {
		NotificationType string `json:"notification_type"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &n)
	}
	return domain.ClaudeNotification(n.NotificationType)
}
