package codex

import (
	"os"
	"testing"
)

func TestInstalledIsTrueOnlyAfterSetup(t *testing.T) {
	cfg, path := codexHome(t, []byte(`{"hooks":{}}`))
	if ok, err := Installed(cfg); err != nil || ok {
		t.Fatalf("before setup: installed = %v, %v", ok, err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"hooks":{}}` {
		t.Fatalf("Installed changed hooks.json: %s", b)
	}
	if _, err := Setup(cfg); err != nil {
		t.Fatal(err)
	}
	if ok, err := Installed(cfg); err != nil || !ok {
		t.Errorf("after setup: installed = %v, %v", ok, err)
	}
	moved := cfg
	moved.Command = "/elsewhere/crew"
	if ok, _ := Installed(moved); ok {
		t.Error("hooks pointing at another binary count as installed")
	}
}

func TestInstalledWithoutHooksJSONIsFalseAndBrokenJSONIsAnError(t *testing.T) {
	cfg, path := codexHome(t, nil)
	if ok, err := Installed(cfg); err != nil || ok {
		t.Errorf("no file: installed = %v, %v", ok, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Installed(cfg); err == nil {
		t.Error("broken JSON gives no error")
	}
}
