package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

func TestNvimKeysWithoutNvimSayHowToGetItAndStartNoPane(t *testing.T) {
	r := startTerm(t, []domain.Session{termSession}, termWTs)
	r.editor.setMissing()
	ctx := context.Background()
	var out rpc.NvimResult
	calls := []struct {
		method string
		params any
	}{
		{rpc.MethodNvimToggle, rpc.NvimParams{Session: "s1"}},
		{rpc.MethodNvimOpen, rpc.NvimParams{Session: "s1", Path: "main.go", Line: 3}},
	}
	for _, c := range calls {
		err := r.c.Call(ctx, c.method, c.params, &out)
		var rerr *rpc.Error
		if !errors.As(err, &rerr) || rerr.Code != rpc.CodeUnavailable || !strings.Contains(rerr.Message, "nvim is not on PATH") || !strings.Contains(rerr.Message, "brew install neovim") {
			t.Errorf("%s: %v", c.method, err)
		}
	}
	if n := len(r.term.createdSpecs()); n != 0 {
		t.Errorf("%d panes created without nvim", n)
	}
}
