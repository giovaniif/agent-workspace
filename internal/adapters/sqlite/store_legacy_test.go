package sqlite

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
)

func TestOpenRenamesAutomationTrustKeysOfTheOldRepoConfig(t *testing.T) {
	s, path := openTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 9`); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"/w/api/.agentws.toml\x00deps", "/w/web/.crew.toml\x00deps"} {
		hash := "h1"
		if strings.Contains(key, "web") {
			hash = "h2"
		}
		data, _ := json.Marshal(domain.AutomationTrust{Key: key, Hash: hash})
		if _, err := db.Exec(`INSERT INTO automation_trust(key, data) VALUES (?, ?)`, key, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	snap, err := r.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.AutomationTrust{{Key: "/w/api/.crew.toml\x00deps", Hash: "h1"}, {Key: "/w/web/.crew.toml\x00deps", Hash: "h2"}}
	if !reflect.DeepEqual(snap.AutomationTrust, want) {
		t.Fatalf("trust %+v", snap.AutomationTrust)
	}
}
