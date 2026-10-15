package domain

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestMateManifestMatchesTheGolden(t *testing.T) {
	got := MateManifest(Mate{Name: "reviewer", Prompt: "Review code.", Agent: "claude", Enabled: true})
	want, err := os.ReadFile("testdata/mate_manifest.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("manifest differs from testdata/mate_manifest.golden; got:\n%s", got)
	}
}

func TestMateManifestIsJSONWithSocketModeAndTheNeededScopes(t *testing.T) {
	var m struct {
		Settings struct {
			SocketMode bool `json:"socket_mode_enabled"`
		} `json:"settings"`
		OAuth struct {
			Scopes struct {
				Bot []string `json:"bot"`
			} `json:"scopes"`
		} `json:"oauth_config"`
	}
	if err := json.Unmarshal([]byte(MateManifest(Mate{Name: "ada"})), &m); err != nil {
		t.Fatal(err)
	}
	if !m.Settings.SocketMode {
		t.Error("socket mode is off")
	}
	have := strings.Join(m.OAuth.Scopes.Bot, " ")
	for _, scope := range []string{"app_mentions:read", "chat:write", "channels:history", "groups:history", "users:read", "im:history"} {
		if !strings.Contains(have, scope) {
			t.Errorf("scope %s is missing from %s", scope, have)
		}
	}
}

func TestMateManifestURLCarriesTheManifest(t *testing.T) {
	manifest := MateManifest(Mate{Name: "ada"})
	link := MateManifestURL(manifest)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "api.slack.com" || u.Path != "/apps" || u.Query().Get("new_app") != "1" {
		t.Errorf("link %s", link)
	}
	if u.Query().Get("manifest_json") != manifest {
		t.Errorf("the manifest in the link differs")
	}
}

func TestValidateMateTokens(t *testing.T) {
	cases := []struct {
		name     string
		bot, app string
		ok       bool
	}{
		{"both right", "xoxb-1234-abcd", "xapp-1-A0-1234-abcd", true},
		{"bot with the app prefix", "xapp-1-A0-1234", "xapp-1-A0-1234", false},
		{"app with the bot prefix", "xoxb-1234-abcd", "xoxb-1234-abcd", false},
		{"user token", "xoxp-1234-abcd", "xapp-1-A0-1234", false},
		{"empty bot", "", "xapp-1-A0-1234", false},
		{"empty app", "xoxb-1234-abcd", "", false},
		{"prefix only", "xoxb-", "xapp-", false},
		{"white space inside", "xoxb-12 34", "xapp-1-A0-1234", false},
		{"new line inside", "xoxb-1234\nabc", "xapp-1-A0-1234", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMateTokens(tc.bot, tc.app)
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok = %v", err, tc.ok)
			}
			if err != nil && (strings.Contains(err.Error(), tc.bot) && tc.bot != "" || strings.Contains(err.Error(), tc.app) && tc.app != "") {
				t.Errorf("the error shows a token: %v", err)
			}
		})
	}
}

func TestValidMateName(t *testing.T) {
	for name, want := range map[string]bool{"ada": true, "a-b_1": true, "": false, "../x": false, "A": false, "-a": false, "a/b": false, strings.Repeat("a", 33): false} {
		if ValidMateName(name) != want {
			t.Errorf("%q: want %v", name, want)
		}
	}
}
