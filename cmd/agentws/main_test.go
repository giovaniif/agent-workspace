package main

import (
	"io"
	"testing"
)

func TestProofExistingBehavior(t *testing.T) {
	if run([]string{"version"}, io.Discard, io.Discard) != 0 {
		t.Fatal("version should exit 0")
	}
}
