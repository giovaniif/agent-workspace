package domain

import "testing"

func TestProofNewBehavior(t *testing.T) {
	if Double(2) != 4 {
		t.Fatal("Double(2) != 4")
	}
}
