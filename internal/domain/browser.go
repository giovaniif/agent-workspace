package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type BrowserCall struct {
	Command string
	Args    map[string]any
}

var browserRef = regexp.MustCompile(`^@e[0-9]+$`)

var browserDevices = map[string]bool{"desktop": true, "ipad": true, "iphone": true}

func BrowserCommands() []string {
	return []string{"goto", "snapshot", "click", "fill", "type", "press", "wait", "screenshot", "eval", "tabs", "cookies", "set-device"}
}

func ParseBrowserCommand(args []string) (BrowserCall, error) {
	if len(args) == 0 {
		return BrowserCall{}, errors.New("usage: crew browser <" + strings.Join(BrowserCommands(), "|") + "> [args]")
	}
	cmd, rest := args[0], args[1:]
	call := BrowserCall{Command: cmd, Args: map[string]any{}}
	switch cmd {
	case "snapshot", "tabs", "screenshot":
		if len(rest) > 0 {
			return BrowserCall{}, fmt.Errorf("%s takes no arguments", cmd)
		}
	case "goto":
		if err := one(cmd, rest, "a URL", "one URL"); err != nil {
			return BrowserCall{}, err
		}
		call.Args["url"] = rest[0]
	case "click":
		if err := one(cmd, rest, "an element ref", "one element ref"); err != nil {
			return BrowserCall{}, err
		}
		if err := checkRef(rest[0]); err != nil {
			return BrowserCall{}, err
		}
		call.Args["ref"] = rest[0]
	case "fill":
		if len(rest) < 2 {
			return BrowserCall{}, errors.New("fill needs an element ref and a text")
		}
		if err := checkRef(rest[0]); err != nil {
			return BrowserCall{}, err
		}
		call.Args["ref"] = rest[0]
		call.Args["text"] = strings.Join(rest[1:], " ")
	case "type":
		if len(rest) == 0 {
			return BrowserCall{}, errors.New("type needs a text")
		}
		call.Args["text"] = strings.Join(rest, " ")
	case "press":
		if err := one(cmd, rest, "a key", "one key"); err != nil {
			return BrowserCall{}, err
		}
		call.Args["key"] = rest[0]
	case "wait":
		return waitCall(call, rest)
	case "eval":
		if len(rest) == 0 {
			return BrowserCall{}, errors.New("eval needs a script")
		}
		call.Args["js"] = strings.Join(rest, " ")
	case "cookies":
		if len(rest) > 1 {
			return BrowserCall{}, errors.New("cookies takes one domain")
		}
		if len(rest) == 1 {
			call.Args["domain"] = rest[0]
		}
	case "set-device":
		if len(rest) != 1 || !browserDevices[strings.ToLower(rest[0])] {
			return BrowserCall{}, errors.New("set-device needs desktop, ipad or iphone")
		}
		call.Args["device"] = strings.ToLower(rest[0])
	default:
		return BrowserCall{}, fmt.Errorf("unknown browser command %q (one of %s)", cmd, strings.Join(BrowserCommands(), ", "))
	}
	return call, nil
}

func one(cmd string, rest []string, needs, takes string) error {
	if len(rest) == 0 {
		return fmt.Errorf("%s needs %s", cmd, needs)
	}
	if len(rest) > 1 {
		return fmt.Errorf("%s takes %s", cmd, takes)
	}
	return nil
}

func checkRef(ref string) error {
	if !browserRef.MatchString(ref) {
		return fmt.Errorf("%q is not an element ref: take one from snapshot, such as @e1", ref)
	}
	return nil
}

func waitCall(call BrowserCall, rest []string) (BrowserCall, error) {
	if len(rest) == 0 {
		return BrowserCall{}, errors.New("wait needs an element ref, a time in ms or a text")
	}
	if len(rest) == 1 && browserRef.MatchString(rest[0]) {
		call.Args["ref"] = rest[0]
		return call, nil
	}
	if len(rest) == 1 {
		if ms, err := strconv.Atoi(rest[0]); err == nil {
			if ms < 0 {
				return BrowserCall{}, errors.New("wait needs a time of 0 ms or more")
			}
			call.Args["ms"] = ms
			return call, nil
		}
	}
	call.Args["text"] = strings.Join(rest, " ")
	return call, nil
}
