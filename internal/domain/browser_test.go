package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBrowserCommandBuildsTheCallTheMacAppRuns(t *testing.T) {
	cases := []struct {
		args []string
		want BrowserCall
	}{
		{[]string{"goto", "http://devbox:3000/"}, BrowserCall{Command: "goto", Args: map[string]any{"url": "http://devbox:3000/"}}},
		{[]string{"snapshot"}, BrowserCall{Command: "snapshot", Args: map[string]any{}}},
		{[]string{"click", "@e1"}, BrowserCall{Command: "click", Args: map[string]any{"ref": "@e1"}}},
		{[]string{"fill", "@e12", "hello", "world"}, BrowserCall{Command: "fill", Args: map[string]any{"ref": "@e12", "text": "hello world"}}},
		{[]string{"fill", "@e2", ""}, BrowserCall{Command: "fill", Args: map[string]any{"ref": "@e2", "text": ""}}},
		{[]string{"type", "abc", "def"}, BrowserCall{Command: "type", Args: map[string]any{"text": "abc def"}}},
		{[]string{"press", "Enter"}, BrowserCall{Command: "press", Args: map[string]any{"key": "Enter"}}},
		{[]string{"wait", "@e3"}, BrowserCall{Command: "wait", Args: map[string]any{"ref": "@e3"}}},
		{[]string{"wait", "500"}, BrowserCall{Command: "wait", Args: map[string]any{"ms": 500}}},
		{[]string{"wait", "Signed", "in"}, BrowserCall{Command: "wait", Args: map[string]any{"text": "Signed in"}}},
		{[]string{"screenshot"}, BrowserCall{Command: "screenshot", Args: map[string]any{}}},
		{[]string{"eval", "document.title"}, BrowserCall{Command: "eval", Args: map[string]any{"js": "document.title"}}},
		{[]string{"tabs"}, BrowserCall{Command: "tabs", Args: map[string]any{}}},
		{[]string{"cookies"}, BrowserCall{Command: "cookies", Args: map[string]any{}}},
		{[]string{"cookies", "example.com"}, BrowserCall{Command: "cookies", Args: map[string]any{"domain": "example.com"}}},
		{[]string{"set-device", "iPhone"}, BrowserCall{Command: "set-device", Args: map[string]any{"device": "iphone"}}},
		{[]string{"set-device", "desktop"}, BrowserCall{Command: "set-device", Args: map[string]any{"device": "desktop"}}},
		{[]string{"set-device", "ipad"}, BrowserCall{Command: "set-device", Args: map[string]any{"device": "ipad"}}},
	}
	for _, c := range cases {
		got, err := ParseBrowserCommand(c.args)
		if err != nil {
			t.Fatalf("%q: %v", c.args, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: got %#v, want %#v", c.args, got, c.want)
		}
	}
}

func TestParseBrowserCommandRefusesBadArgumentsAndSaysWhy(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "usage"},
		{[]string{"fly"}, `unknown browser command "fly"`},
		{[]string{"goto"}, "goto needs a URL"},
		{[]string{"goto", "a", "b"}, "goto takes one URL"},
		{[]string{"snapshot", "x"}, "snapshot takes no arguments"},
		{[]string{"tabs", "x"}, "tabs takes no arguments"},
		{[]string{"screenshot", "x"}, "screenshot takes no arguments"},
		{[]string{"click"}, "click needs an element ref"},
		{[]string{"click", "e1"}, `"e1" is not an element ref`},
		{[]string{"click", "@e"}, `"@e" is not an element ref`},
		{[]string{"click", "@e1x"}, `"@e1x" is not an element ref`},
		{[]string{"click", "@e1", "@e2"}, "click takes one element ref"},
		{[]string{"fill", "@e1"}, "fill needs an element ref and a text"},
		{[]string{"fill", "x", "text"}, `"x" is not an element ref`},
		{[]string{"type"}, "type needs a text"},
		{[]string{"press"}, "press needs a key"},
		{[]string{"press", "a", "b"}, "press takes one key"},
		{[]string{"wait"}, "wait needs an element ref, a time in ms or a text"},
		{[]string{"wait", "-5"}, "wait needs a time of 0 ms or more"},
		{[]string{"eval"}, "eval needs a script"},
		{[]string{"cookies", "a", "b"}, "cookies takes one domain"},
		{[]string{"set-device"}, "set-device needs desktop, ipad or iphone"},
		{[]string{"set-device", "tv"}, "set-device needs desktop, ipad or iphone"},
		{[]string{"set-device", "ipad", "x"}, "set-device needs desktop, ipad or iphone"},
	}
	for _, c := range cases {
		_, err := ParseBrowserCommand(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%q: got %v, want %q", c.args, err, c.want)
		}
	}
}

func TestBrowserCommandsListsEveryCommandOnce(t *testing.T) {
	want := []string{"goto", "snapshot", "click", "fill", "type", "press", "wait", "screenshot", "eval", "tabs", "cookies", "set-device"}
	if got := BrowserCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
