package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func WithMateSecrets(s app.MateSecrets) Option {
	return func(d *Daemon) { d.mateSecrets = s }
}

func (d *Daemon) mateTokens(req rpc.Request) *rpc.Response {
	if d.mateSecrets == nil {
		return errorResponse(req.ID, rpc.CodeUnavailable, "this daemon has no secret store")
	}
	var p struct {
		Name     string `json:"name"`
		BotToken string `json:"bot_token"`
		AppToken string `json:"app_token"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return errorResponse(req.ID, rpc.CodeBadRequest, req.Method+" needs a name and valid params")
	}
	if !d.hasMate(p.Name) {
		return errorResponse(req.ID, rpc.CodeNotFound, fmt.Sprintf("no mate %q", p.Name))
	}
	if req.Method == rpc.MethodMateClearTokens {
		if err := d.mateSecrets.ClearMateTokens(p.Name); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
	} else {
		if err := domain.ValidateMateTokens(p.BotToken, p.AppToken); err != nil {
			return errorResponse(req.ID, rpc.CodeBadRequest, err.Error())
		}
		if err := d.mateSecrets.SetMateTokens(p.Name, p.BotToken, p.AppToken); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error())
		}
	}
	bot, app := d.mateSecrets.HasMateTokens(p.Name)
	return result(req.ID, rpc.MateTokenStatus{Name: p.Name, HasBotToken: bot, HasAppToken: app})
}

func (d *Daemon) hasMate(name string) bool {
	src, err := readConfig(d.cfg.path)
	if err != nil {
		return false
	}
	for _, m := range d.loadMates(src).Mates {
		if m.Name == name {
			return true
		}
	}
	return false
}
