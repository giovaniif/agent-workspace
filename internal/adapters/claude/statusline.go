package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os/exec"
	"sort"
	"strings"

	"github.com/giovaniif/crew/internal/domain"
)

type statusInput struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Effort struct {
		Level string `json:"level"`
	} `json:"effort"`
	ContextWindow struct {
		RemainingPercentage *float64 `json:"remaining_percentage"`
	} `json:"context_window"`
	RateLimits map[string]struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       int64    `json:"resets_at"`
	} `json:"rate_limits"`
}

func ParseStatus(b []byte) (domain.StatusReport, error) {
	var in statusInput
	if err := json.Unmarshal(b, &in); err != nil {
		return domain.StatusReport{}, err
	}
	r := domain.StatusReport{Model: shortModel(in.Model.ID, in.Model.DisplayName), Effort: in.Effort.Level}
	if p := in.ContextWindow.RemainingPercentage; p != nil {
		r.ContextLeft, r.HasContext = percent(*p), true
	}
	windows := make([]string, 0, len(in.RateLimits))
	for w, l := range in.RateLimits {
		if l.UsedPercentage != nil {
			windows = append(windows, w)
		}
	}
	sort.Slice(windows, func(i, j int) bool { return windowLess(windows[i], windows[j]) })
	for _, w := range windows {
		l := in.RateLimits[w]
		r.Limits = append(r.Limits, domain.RateLimit{Window: w, UsedPercent: percent(*l.UsedPercentage), ResetsAt: l.ResetsAt})
	}
	return r, nil
}

func percent(p float64) int { return int(math.Round(p)) }

func shortModel(id, display string) string {
	id, _, _ = strings.Cut(id, "[")
	id = strings.TrimPrefix(id, "claude-")
	if id == "" {
		return display
	}
	parts := strings.Split(id, "-")
	var name, version []string
	for _, p := range parts {
		switch {
		case len(p) == 8 && strings.Trim(p, "0123456789") == "":
		case p != "" && strings.Trim(p, "0123456789") == "":
			version = append(version, p)
		default:
			name = append(name, p)
		}
	}
	if len(version) == 0 {
		return strings.Join(name, "-")
	}
	return strings.Join(name, "-") + "-" + strings.Join(version, ".")
}

func windowLess(a, b string) bool {
	rank := func(w string) int {
		switch w {
		case "five_hour":
			return 0
		case "seven_day":
			return 1
		}
		return 2
	}
	if ra, rb := rank(a), rank(b); ra != rb {
		return ra < rb
	}
	return a < b
}

func Chain(ctx context.Context, command string, input []byte, stdout io.Writer) error {
	if command == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	return cmd.Run()
}

const statusLineVerb = " statusline --harness claude"
const chainFlag = " --chain "

func StatusLineCommand(bin, chain string) string {
	cmd := shellQuote(bin) + statusLineVerb
	if chain != "" {
		cmd += chainFlag + shellQuote(chain)
	}
	return cmd
}

func ChainedStatusLine(command string) (string, bool) {
	i := strings.Index(command, statusLineVerb)
	if i < 0 {
		return "", false
	}
	rest := command[i+len(statusLineVerb):]
	if rest == "" {
		return "", true
	}
	if !strings.HasPrefix(rest, chainFlag) {
		return "", false
	}
	return unquote(rest[len(chainFlag):]), true
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, unsafeInShell) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func unsafeInShell(r rune) bool {
	safe := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+=:,@%", r)
	return !safe
}

func unquote(s string) string {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return s
	}
	return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
}
