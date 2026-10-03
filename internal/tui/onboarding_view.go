package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const setupMaxWidth = 80

var stepNames = map[domain.OnboardStep]string{
	domain.OnboardPick:   "Agents",
	domain.OnboardNvim:   "Neovim",
	domain.OnboardFinish: "Done",
}

func stepName(step domain.OnboardStep) string {
	if h, ok := step.Harness(); ok {
		return domain.Spec(h).Name
	}
	return stepNames[step]
}

type harnessCopy struct{ change, undo string }

var harnessCopies = map[domain.Harness]harnessCopy{
	domain.HarnessClaude: {"Adds the agentws hooks and wraps your status line. Your own hooks and status line keep running.", "agentws setup claude --remove"},
	domain.HarnessCodex:  {"Adds the agentws hooks. Your other hooks stay as they are.", "agentws setup codex --remove"},
	domain.HarnessOmp:    {"Writes an agentws hook file of its own. A file at that path that agentws did not write is left alone.", "agentws setup omp --remove"},
}

// why: it is laid out like the new-session popup (ADR 0037) so both read as one app.
func (m Model) setupScreen() string {
	f := m
	f.width = min(m.width-4, setupMaxWidth)
	lines := dialogViewport(f.setupLines(), 0, m.height)
	margin := strings.Repeat(" ", max((m.width-f.width)/2, 0))
	for i, l := range lines {
		lines[i] = margin + l
	}
	if len(lines) < m.height {
		return "\n" + strings.Join(lines, "\n")
	}
	return strings.Join(lines, "\n")
}

func (m Model) setupLines() []string {
	s := m.styles
	ob := m.ob
	out := []string{m.titleBar(" Set up agentws", "esc skip "), "", m.stepTrail(), ""}
	_, harnessStep := ob.step.Harness()
	switch {
	case ob.step == domain.OnboardPick:
		out = append(out, m.pickLines()...)
	case harnessStep:
		out = append(out, m.harnessLines()...)
	case ob.step == domain.OnboardNvim:
		out = append(out, m.nvimLines()...)
	case ob.step == domain.OnboardFinish:
		out = append(out, m.finishLines()...)
	}
	out = append(out, "")
	switch {
	case ob.busy:
		out = append(out, m.line(false, []piece{{s.sub, " working…"}}, nil))
	case ob.err != "":
		out = append(out, m.line(false, []piece{{s.peach, " ✗ " + ob.err}}, nil))
	}
	hint, action := m.setupActions()
	buttons := []piece{{s.sub, "esc skip"}, {s.text, "  "}, {s.badge, " ⏎ " + action + " "}, {s.text, " "}}
	return append(out, m.line(false, []piece{{s.dim, hint}}, buttons))
}

func (m Model) setupActions() (hint, action string) {
	ob := m.ob
	_, harnessStep := ob.step.Harness()
	switch {
	case ob.step == domain.OnboardPick:
		return " ↑/↓ move · space pick", "next"
	case harnessStep:
		_, setup := ob.harness()
		if domain.HarnessOffer(setup) == domain.OfferInstall && ob.results[ob.step] != resultInstalled {
			return " s skip", "install"
		}
		return "", "next"
	case ob.step == domain.OnboardNvim:
		if domain.NvimOfferFor(ob.status.Nvim) == domain.NvimShowSnippet && ob.results[ob.step] != resultInstalled {
			return " s skip", "write it"
		}
		return "", "next"
	}
	return "", "done"
}

func (m Model) stepTrail() string {
	s := m.styles
	ob := m.ob
	var ps []piece
	passed := true
	for i, step := range ob.steps() {
		if i > 0 {
			ps = append(ps, piece{s.dim, " › "})
		}
		st := s.dim
		switch {
		case step == ob.step:
			st, passed = s.brand, false
		case passed:
			st = s.sub
		}
		ps = append(ps, piece{st, stepName(step)})
	}
	return m.line(false, append([]piece{{s.text, " "}}, ps...), nil)
}

func (m Model) pickLines() []string {
	s := m.styles
	ob := m.ob
	rows := [][]piece{}
	for i, h := range domain.Harnesses() {
		cursor, check, name := "  ", "[ ] ", s.text
		if i == ob.cursor {
			cursor, name = "▸ ", s.brand
		}
		if slices.Contains(ob.picked, h) {
			check = "[x] "
		}
		rows = append(rows, []piece{{s.bar, cursor}, {name, check + padRight(domain.Spec(h).Name, 14)}, setupState(s, ob.status.Harnesses[h])})
	}
	out := []string{m.line(false, []piece{{s.bold, " Which agents do you use?"}}, nil)}
	out = append(out, m.framed(rows, true)...)
	return append(out, m.para(piece{s.text, " "}, s.sub, "agentws reads their hooks to show what each session is doing. Pick any of them; you set up each one next.")...)
}

func setupState(s styles, h domain.HarnessSetup) piece {
	switch domain.HarnessOffer(h) {
	case domain.OfferInstalled:
		return piece{s.green, "✓ set up"}
	case domain.OfferBroken:
		return piece{s.peach, "✗ cannot read its config"}
	}
	return piece{s.dim, "not set up"}
}

func (m Model) harnessLines() []string {
	s := m.styles
	ob := m.ob
	h, setup := ob.harness()
	name, text := domain.Spec(h).Name, harnessCopies[h]
	bar := piece{s.dim, " ▌ "}
	out := []string{m.line(false, []piece{{s.bold, " " + name}}, []piece{setupState(s, setup), {s.text, " "}}), ""}
	if domain.HarnessOffer(setup) == domain.OfferBroken {
		out = append(out, m.para(piece{s.peach, " ▌ "}, s.text, setup.Err)...)
		return append(out, m.para(piece{s.peach, " ▌ "}, s.sub, "Fix or move the file, then open this again with S.")...)
	}
	out = append(out, m.line(false, []piece{bar, {s.sub, "Changes "}, {s.bold, setup.File}}, nil))
	out = append(out, m.para(bar, s.text, text.change)...)
	backup := setup.Backup
	if backup == "" {
		backup = "none needed: the file does not exist yet"
	}
	out = append(out, m.line(false, []piece{bar, {s.sub, "Backup  "}, {s.text, backup}}, nil))
	out = append(out, m.line(false, []piece{bar, {s.sub, "Undo    "}, {s.text, text.undo}}, nil))
	if h == domain.HarnessCodex {
		trust := piece{s.peach, " ▌ "}
		out = append(out, "", m.line(false, []piece{trust, {s.need, "! One more step in Codex"}}, nil))
		out = append(out, m.para(trust, s.sub, domain.CodexTrustStep)...)
	}
	return out
}

func (m Model) nvimLines() []string {
	s := m.styles
	ob := m.ob
	n := ob.status.Nvim
	bar := piece{s.dim, " ▌ "}
	out := []string{m.line(false, []piece{{s.bold, " Neovim"}, {s.sub, "  optional"}}, nil), ""}
	if ob.results[domain.OnboardNvim] == resultInstalled {
		out = append(out, m.line(false, []piece{bar, {s.green, "✓ wrote " + n.ConfigFile}}, nil))
		return append(out, m.line(false, []piece{bar, {s.sub, "Undo    "}, {s.text, "agentws setup nvim --remove"}}, nil))
	}
	switch domain.NvimOfferFor(n) {
	case domain.NvimMissing:
		out = append(out, m.para(bar, s.text, "nvim is not on PATH, and that's fine: everything else in agentws works without it. With Neovim 0.10+, e opens a session's files in nvim, diffs open in diffview, and you can write review comments from nvim.")...)
		out = append(out, "", m.line(false, []piece{bar, {s.sub, "macOS   "}, {s.text, "brew install neovim"}}, nil))
		out = append(out, m.line(false, []piece{bar, {s.sub, "Linux   "}, {s.text, "your package manager, e.g. apt install neovim"}}, nil))
		return append(append(out, ""), m.para(bar, s.sub, "Then press S in the sidebar to add the plugin.")...)
	case domain.NvimReady:
		out = append(out, m.line(false, []piece{bar, {s.green, "✓ the agentws plugin is configured"}}, nil))
		return append(out, m.para(bar, s.sub, "found in "+n.ConfigFile)...)
	case domain.NvimNoPlugin:
		out = append(out, m.line(false, []piece{{s.peach, " ▌ "}, {s.text, "The plugin files are not at " + n.PluginDir + "."}}, nil))
		out = append(out, m.para(piece{s.peach, " ▌ "}, s.sub, "The install script puts them there; from a source checkout, add these lines yourself with the path to its nvim/ directory.")...)
		out = append(out, "")
		for _, l := range domain.NvimSnippet(n.PluginDir, n.ConfigFile) {
			// why: no frame and no truncation, so selecting the lines copies exactly the code.
			out = append(out, "   "+s.teal.Render(l))
		}
		return out
	}
	out = append(out, m.line(false, []piece{{s.text, " ⏎ writes "}, {s.bold, n.ConfigFile}}, nil))
	out = append(out, m.para(piece{s.text, " "}, s.sub, "nvim loads every file in plugin/ at startup, whatever else your config uses (init.lua, init.vim, lazy.nvim), so nothing of yours is edited.")...)
	out = append(out, "")
	for _, l := range strings.Split(strings.TrimSuffix(domain.NvimSetupFile(n.PluginDir), "\n"), "\n") {
		// why: no frame and no truncation, so selecting the lines copies exactly the code.
		out = append(out, "   "+s.teal.Render(l))
	}
	out = append(out, "")
	return append(out, m.line(false, []piece{bar, {s.sub, "Undo    "}, {s.text, "agentws setup nvim --remove"}}, nil))
}

func (m Model) finishLines() []string {
	s := m.styles
	ob := m.ob
	out := []string{m.line(false, []piece{{s.bold, " You're set"}}, nil), ""}
	row := func(step domain.OnboardStep, name string, picked bool, setup domain.HarnessSetup) {
		mark, detail := piece{s.dim, "  – "}, "not picked"
		switch {
		case picked && ob.results[step] == resultSkipped:
			detail = "skipped"
		case picked && setup.Installed:
			mark, detail = piece{s.green, "  ✓ "}, "hooks installed"
		case picked:
			detail = "not set up"
		}
		out = append(out, m.line(false, []piece{mark, {s.text, padRight(name, 14)}, {s.sub, detail}}, nil))
	}
	for _, h := range domain.Harnesses() {
		row(domain.HarnessStep(h), domain.Spec(h).Name, slices.Contains(ob.picked, h), ob.status.Harnesses[h])
	}
	mark, detail := piece{s.dim, "  – "}, ""
	switch {
	case ob.results[domain.OnboardNvim] == resultSkipped:
		detail = "skipped"
	case ob.results[domain.OnboardNvim] == resultInstalled:
		mark, detail = piece{s.green, "  ✓ "}, "plugin configured"
	case domain.NvimOfferFor(ob.status.Nvim) == domain.NvimReady:
		mark, detail = piece{s.green, "  ✓ "}, "plugin configured"
	case domain.NvimOfferFor(ob.status.Nvim) == domain.NvimMissing:
		detail = "optional, not installed"
	default:
		detail = "not set up"
	}
	out = append(out, m.line(false, []piece{mark, {s.text, padRight("Neovim", 14)}, {s.sub, detail}}, nil), "")
	return append(out, m.para(piece{s.text, " "}, s.sub, "Open this again with S in the sidebar or agentws setup. n starts your first session.")...)
}

// why: a long path or message wraps under its bar instead of being cut off.
func (m Model) para(bar piece, st lipgloss.Style, text string) []string {
	room := max(m.width-ansi.StringWidth(bar.s)-1, 10)
	var out []string
	for _, l := range strings.Split(ansi.Wordwrap(text, room, ""), "\n") {
		out = append(out, m.line(false, []piece{bar, {st, l}}, nil))
	}
	return out
}

func (m Model) framed(rows [][]piece, active bool) []string {
	border := m.styles.dim
	if active {
		border = m.styles.bar
	}
	inner := max(m.width-4, 1)
	out := []string{border.Render(" ╭" + strings.Repeat("─", inner) + "╮")}
	for _, r := range rows {
		used := 0
		mid := border.Render(" │ ")
		for _, p := range r {
			pw := ansi.StringWidth(p.s)
			if used+pw > inner-1 {
				mid += p.st.Render(ansi.Truncate(p.s, max(inner-1-used, 0), "…"))
				used = inner - 1
				break
			}
			mid += p.st.Render(p.s)
			used += pw
		}
		out = append(out, mid+strings.Repeat(" ", max(inner-1-used, 0))+border.Render("│"))
	}
	return append(out, border.Render(" ╰"+strings.Repeat("─", inner)+"╯"))
}

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(n-ansi.StringWidth(s), 0))
}
