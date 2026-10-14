package domain

import "testing"

func TestEnvPrefersCrewThenFallsBackToAgentws(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"crew name wins", map[string]string{"CREW_HOME": "/c", "AGENTWS_HOME": "/a"}, "/c"},
		{"agentws name is the fallback", map[string]string{"AGENTWS_HOME": "/a"}, "/a"},
		{"empty crew value falls back", map[string]string{"CREW_HOME": "", "AGENTWS_HOME": "/a"}, "/a"},
		{"neither set gives empty", map[string]string{"OTHER": "x"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			get := func(k string) string { return c.env[k] }
			if got := Env(get, "HOME"); got != c.want {
				t.Fatalf("Env(HOME) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEnvNameUsesTheCrewPrefix(t *testing.T) {
	if got := EnvName("HOME"); got != "CREW_HOME" {
		t.Fatalf("EnvName(HOME) = %q, want CREW_HOME", got)
	}
}
