package rpc

import (
	"os"
	"path/filepath"
)

func LegacySocketPath(home string) string { return filepath.Join(home, "agentws.sock") }

func SocketPath(home string) string {
	current := ListenPath(home)
	if _, err := os.Lstat(current); err == nil {
		return current
	}
	if _, err := os.Lstat(LegacySocketPath(home)); err == nil {
		return LegacySocketPath(home)
	}
	return current
}
