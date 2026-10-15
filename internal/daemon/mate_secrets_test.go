package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/adapters/secrets"
	"github.com/giovaniif/crew/internal/daemon"
	"github.com/giovaniif/crew/internal/rpc"
)

const (
	knownBot = "xoxb-111-222-zzbotsecretzz"
	knownApp = "xapp-1-A0-333-zzappsecretzz"
)

func secretsDaemon(t *testing.T, body string) (*rpc.Client, string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "secrets")
	_, sock := start(t, &memStore{}, daemon.WithWorkspaces(shop, shopGit), daemon.WithConfig(path), daemon.WithMateSecrets(secrets.Files{Dir: dir}))
	c := dial(t, sock)
	if _, err := c.WorkspaceAdd(context.Background(), "/shop"); err != nil {
		t.Fatal(err)
	}
	return c, path, dir
}

func setTokens(c *rpc.Client, name, bot, app string) (rpc.MateTokenStatus, error) {
	var got rpc.MateTokenStatus
	err := c.Call(context.Background(), rpc.MethodMateSetTokens, rpc.MateSetTokensParams{Name: name, BotToken: bot, AppToken: app}, &got)
	return got, err
}

func TestMateSetTokensStoresThemInFilesAndAnswersOnlyWhichOnesExist(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	c, cfg, dir := secretsDaemon(t, globalMates)
	got, err := setTokens(c, "reviewer", knownBot, knownApp)
	if err != nil {
		t.Fatal(err)
	}
	if got != (rpc.MateTokenStatus{Name: "reviewer", HasBotToken: true, HasAppToken: true}) {
		t.Errorf("status %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "reviewer.bot"))
	if err != nil || string(data) != knownBot {
		t.Errorf("bot file %q, %v", data, err)
	}
	body, _ := os.ReadFile(cfg)
	if strings.Contains(string(body), "xoxb") || strings.Contains(string(body), "xapp") || strings.Contains(string(body), "zz") {
		t.Errorf("config.toml holds a token:\n%s", body)
	}
	if strings.Contains(logs.String(), "zzbotsecretzz") || strings.Contains(logs.String(), "zzappsecretzz") {
		t.Errorf("the log holds a token:\n%s", logs.String())
	}
}

func TestNoMateMethodReturnsAToken(t *testing.T) {
	c, _, _ := secretsDaemon(t, globalMates)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, call := range []struct {
		method string
		params any
	}{
		{rpc.MethodMateList, struct{}{}},
		{rpc.MethodMateShow, rpc.MateShowParams{Name: "reviewer"}},
		{rpc.MethodMateSetTokens, rpc.MateSetTokensParams{Name: "reviewer", BotToken: knownBot, AppToken: knownApp}},
	} {
		var raw json.RawMessage
		if err := c.Call(ctx, call.method, call.params, &raw); err != nil {
			t.Fatal(err)
		}
		if text := string(raw); strings.Contains(text, "zz") || strings.Contains(text, "xox") || strings.Contains(text, "xapp") {
			t.Errorf("%s returned a token: %s", call.method, text)
		}
	}
}

func TestMateSetTokensRefusesAWrongPrefixWithoutEchoingTheValue(t *testing.T) {
	c, _, dir := secretsDaemon(t, globalMates)
	_, err := setTokens(c, "reviewer", knownApp, knownBot)
	if callCode(err) != rpc.CodeBadRequest {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "zz") {
		t.Errorf("the error shows a token: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "reviewer.bot")); statErr == nil {
		t.Error("a file was written")
	}
}

func TestMateSetTokensNeedsAnExistingMate(t *testing.T) {
	c, _, _ := secretsDaemon(t, globalMates)
	if _, err := setTokens(c, "ghost", knownBot, knownApp); callCode(err) != rpc.CodeNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestMateClearTokensRemovesTheFiles(t *testing.T) {
	c, _, dir := secretsDaemon(t, globalMates)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	var got rpc.MateTokenStatus
	if err := c.Call(context.Background(), rpc.MethodMateClearTokens, rpc.MateClearTokensParams{Name: "reviewer"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.HasBotToken || got.HasAppToken {
		t.Errorf("status %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
}

func TestMateRemoveAlsoClearsItsTokens(t *testing.T) {
	c, _, dir := secretsDaemon(t, globalMates)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), rpc.MethodMateRemove, rpc.MateRemoveParams{Name: "reviewer"}, nil); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
}

func TestMateTokensAreUnavailableWithoutASecretStore(t *testing.T) {
	c, _ := mateDaemon(t, globalMates)
	if _, err := setTokens(c, "reviewer", knownBot, knownApp); callCode(err) != rpc.CodeUnavailable {
		t.Errorf("err = %v", err)
	}
}
