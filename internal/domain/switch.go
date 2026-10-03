package domain

import (
	"slices"
	"strings"
	"time"
)

type SwitchKind string

const (
	SwitchModel  SwitchKind = "model"
	SwitchEffort SwitchKind = "effort"
)

type Switch struct {
	Kind   SwitchKind
	Value  string
	SentAt time.Time
}

func (sw Switch) sent() bool { return !sw.SentAt.IsZero() }

func SwitchSupported(h Harness) bool { return Spec(h).Switch != SwitchNone }

func SwitchChoices(h Harness, kind SwitchKind) []string {
	spec := Spec(h)
	switch {
	case spec.Switch == SwitchNone:
		return nil
	case kind == SwitchEffort:
		return slices.Clone(spec.Efforts)
	}
	return slices.Clone(spec.Models)
}

// why: ok is false when the text cannot be formed yet, as for an omp effort
// before any model is known; the switch then stays queued.
func (s Session) SwitchCommand(sw Switch) (string, bool) {
	switch Spec(s.Harness).Switch {
	case SwitchSlash:
		return "/" + string(sw.Kind) + " " + sw.Value, true
	case SwitchPicker:
		return "/model", true
	case SwitchOmpSwitch:
		if sw.Kind == SwitchModel {
			return "/switch " + sw.Value, true
		}
		if model := s.targetModel(); model != "" {
			return "/switch " + model + ":" + sw.Value, true
		}
	}
	return "", false
}

func (s Session) targetModel() string {
	for _, sw := range s.Switches {
		if sw.Kind == SwitchModel {
			return sw.Value
		}
	}
	return s.Model
}

func (s Session) RequestSwitch(kind SwitchKind, value string) Session {
	kept := make([]Switch, 0, len(s.Switches)+1)
	for _, sw := range s.Switches {
		if sw.Kind != kind {
			kept = append(kept, sw)
		}
	}
	s.Switches = append(kept, Switch{Kind: kind, Value: value})
	s.SwitchWarning = false
	return s
}

// why: typing into a running agent, or into a permission prompt, would land
// in the wrong place.
func (s Session) Dispatch(now time.Time) (Session, []Switch) {
	if !s.AcceptsSwitch() {
		return s, nil
	}
	var out []Switch
	next := slices.Clone(s.Switches)
	for i, sw := range next {
		if _, ok := s.SwitchCommand(sw); ok && !sw.sent() {
			next[i].SentAt = now
			out = append(out, next[i])
		}
	}
	s.Switches = next
	return s, out
}

func (s Session) AcceptsSwitch() bool {
	switch s.State {
	case StateIdle, StateDone, StateWaiting:
		return true
	}
	return false
}

func (s Session) Requeue(unsent []Switch) Session {
	next := slices.Clone(s.Switches)
	for i, sw := range next {
		if slices.Contains(unsent, sw) {
			next[i].SentAt = time.Time{}
		}
	}
	s.Switches = next
	return s
}

func (s Session) SwitchFailed(failed []Switch) Session {
	s.Switches = slices.DeleteFunc(slices.Clone(s.Switches), func(sw Switch) bool { return slices.Contains(failed, sw) })
	s.SwitchWarning = true
	return s
}

// why: a report that shows the old value keeps the switch, so a later report
// showing the new value still clears it. A report stamped before the switch
// was sent cannot show it yet: Codex's rollout only has the new values once a
// turn starts after the switch.
func (s Session) confirmSwitches(r StatusReport) Session {
	if len(s.Switches) == 0 {
		return s
	}
	kept := make([]Switch, 0, len(s.Switches))
	warn := false
	for _, sw := range s.Switches {
		reported := r.Model
		if sw.Kind == SwitchEffort {
			reported = r.Effort
		}
		switch {
		case !sw.sent() || reported == "":
			kept = append(kept, sw)
		case switchShows(sw, reported):
		case !r.At.IsZero() && r.At.Before(sw.SentAt):
			kept = append(kept, sw)
		default:
			kept = append(kept, sw)
			warn = true
		}
	}
	s.Switches = kept
	if warn {
		s.SwitchWarning = true
	} else if !slices.ContainsFunc(kept, Switch.sent) {
		s.SwitchWarning = false
	}
	return s
}

// bug: Claude reports "Opus 4.7" or "opus-5.5" for the alias "opus", while
// "gpt-5" must not match "gpt-5-codex".
func switchShows(sw Switch, reported string) bool {
	want, got := strings.ToLower(sw.Value), strings.ToLower(reported)
	if sw.Kind == SwitchEffort {
		return want == got
	}
	first, _, _ := strings.Cut(got, " ")
	return got == want || first == want || withoutVersion(got) == want
}

func withoutVersion(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 || strings.Trim(name[i+1:], "0123456789.") != "" || name[i+1:] == "" {
		return name
	}
	return name[:i]
}
