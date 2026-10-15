package domain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	botTokenPrefix = "xoxb-"
	appTokenPrefix = "xapp-"
)

var mateBotScopes = []string{
	"app_mentions:read",
	"chat:write",
	"channels:history",
	"groups:history",
	"im:history",
	"im:read",
	"im:write",
	"users:read",
}

var mateBotEvents = []string{"app_mention", "message.channels", "message.groups", "message.im"}

type manifestDoc struct {
	Display struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"display_information"`
	Features struct {
		BotUser struct {
			DisplayName  string `json:"display_name"`
			AlwaysOnline bool   `json:"always_online"`
		} `json:"bot_user"`
		AppHome struct {
			MessagesTab         bool `json:"messages_tab_enabled"`
			MessagesTabReadOnly bool `json:"messages_tab_read_only_enabled"`
		} `json:"app_home"`
	} `json:"features"`
	OAuth struct {
		Scopes struct {
			Bot []string `json:"bot"`
		} `json:"scopes"`
	} `json:"oauth_config"`
	Settings struct {
		Events struct {
			BotEvents []string `json:"bot_events"`
		} `json:"event_subscriptions"`
		Interactivity struct {
			Enabled bool `json:"is_enabled"`
		} `json:"interactivity"`
		OrgDeploy     bool `json:"org_deploy_enabled"`
		SocketMode    bool `json:"socket_mode_enabled"`
		TokenRotation bool `json:"token_rotation_enabled"`
	} `json:"settings"`
}

func MateManifest(m Mate) string {
	var doc manifestDoc
	doc.Display.Name = m.Name
	doc.Display.Description = "A crew mate: " + m.Name
	doc.Features.BotUser.DisplayName = m.Name
	doc.Features.BotUser.AlwaysOnline = true
	doc.Features.AppHome.MessagesTab = true
	doc.OAuth.Scopes.Bot = mateBotScopes
	doc.Settings.Events.BotEvents = mateBotEvents
	doc.Settings.Interactivity.Enabled = true
	doc.Settings.SocketMode = true
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(out) + "\n"
}

func MateManifestURL(manifest string) string {
	q := url.Values{"new_app": {"1"}, "manifest_json": {manifest}}
	return "https://api.slack.com/apps?" + q.Encode()
}

func ValidMateName(name string) bool {
	return len(name) <= mateNameMax && mateName.MatchString(name)
}

func ValidateMateTokens(bot, app string) error {
	if err := checkToken("bot token", bot, botTokenPrefix); err != nil {
		return err
	}
	return checkToken("app token", app, appTokenPrefix)
}

func checkToken(what, value, prefix string) error {
	switch {
	case !strings.HasPrefix(value, prefix):
		return fmt.Errorf("the %s must start with %s", what, prefix)
	case len(value) == len(prefix):
		return fmt.Errorf("the %s has nothing after its prefix", what)
	case strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0:
		return fmt.Errorf("the %s has white space or a control character", what)
	}
	return nil
}
