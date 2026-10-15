package domain

import (
	"testing"
	"time"
)

func TestMateClaimAccepts(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	claim := NewMateClaim("reviewer", "ABCD2345", at)
	if !claim.Expires.Equal(at.Add(10*time.Minute)) || claim.Mate != "reviewer" || claim.Code != "ABCD2345" {
		t.Fatalf("claim %+v", claim)
	}
	cases := []struct {
		name  string
		claim MateClaim
		user  string
		text  string
		now   time.Time
		want  bool
	}{
		{"the code from a user", claim, "U012ABCDEF", "ABCD2345", at, true},
		{"lower case and spaces", claim, "U012ABCDEF", "  abcd-2345\n", at.Add(time.Minute), true},
		{"just before the end", claim, "W012ABCDEF", "ABCD2345", at.Add(10*time.Minute - time.Second), true},
		{"at the end", claim, "U012ABCDEF", "ABCD2345", at.Add(10 * time.Minute), false},
		{"a wrong code", claim, "U012ABCDEF", "ABCD2346", at, false},
		{"the code inside other text", claim, "U012ABCDEF", "my code is ABCD2345", at, false},
		{"not a user ID", claim, "B012ABCDEF", "ABCD2345", at, false},
		{"no user", claim, "", "ABCD2345", at, false},
		{"no claim", MateClaim{}, "U012ABCDEF", "", at, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.claim.Accepts(c.user, c.text, c.now); got != c.want {
				t.Errorf("Accepts = %v, want %v", got, c.want)
			}
		})
	}
}
