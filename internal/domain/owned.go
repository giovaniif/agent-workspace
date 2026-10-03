package domain

import (
	"bytes"
	"strings"
)

type OwnedPlan int

const (
	OwnedUnchanged OwnedPlan = iota
	OwnedWrite
	OwnedDelete
	// why: a file at our path without our marker is the user's, so it is never written or deleted.
	OwnedConflict
)

func PlanOwnedInstall(marker string, current []byte, exists bool, want []byte) OwnedPlan {
	switch {
	case !exists:
		return OwnedWrite
	case bytes.Equal(current, want):
		return OwnedUnchanged
	case ownedBy(marker, current):
		return OwnedWrite
	}
	return OwnedConflict
}

func PlanOwnedRemove(marker string, current []byte, exists bool) OwnedPlan {
	switch {
	case !exists:
		return OwnedUnchanged
	case ownedBy(marker, current):
		return OwnedDelete
	}
	return OwnedConflict
}

func ownedBy(marker string, content []byte) bool {
	first, _, _ := strings.Cut(string(content), "\n")
	return first == marker
}
