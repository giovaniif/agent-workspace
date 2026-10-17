package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/giovaniif/crew/internal/rpc"
)

func TestMateControlCallsTheDaemon(t *testing.T) {
	for _, c := range []struct {
		args   []string
		method string
		params any
		says   string
	}{
		{[]string{"pause", "helper"}, rpc.MethodMatePause, rpc.MateNameParams{Name: "helper"}, "paused helper"},
		{[]string{"resume", "helper"}, rpc.MethodMateResume, rpc.MateNameParams{Name: "helper"}, "resumed helper"},
		{[]string{"stop"}, rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: true}, "stopped all mates"},
		{[]string{"stop", "--off"}, rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: false}, "start turns again"},
	} {
		f := &fakeMateCaller{}
		var out bytes.Buffer
		if err := mateControl(context.Background(), f, &out, c.args); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 1 || f.calls[0].method != c.method || f.calls[0].params != c.params {
			t.Fatalf("%v: calls %+v", c.args, f.calls)
		}
		if !strings.Contains(out.String(), c.says) {
			t.Fatalf("%v: output %q", c.args, out.String())
		}
	}
}

func TestMateControlArguments(t *testing.T) {
	for _, c := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"pause", "a"}, true}, {[]string{"resume", "a"}, true}, {[]string{"stop"}, true}, {[]string{"stop", "--off"}, true},
		{[]string{"pause"}, false}, {[]string{"resume", "a", "b"}, false}, {[]string{"stop", "now"}, false},
	} {
		if got := mateArgsOK(c.args); got != c.ok {
			t.Errorf("mateArgsOK(%v) = %v, want %v", c.args, got, c.ok)
		}
	}
}
