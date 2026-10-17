package tui

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

type matesPanel struct {
	cursor    int
	approvals []domain.Approval
	loading   bool
	stopping  bool
	err       string
}

type mateApprovalsMsg struct {
	list []domain.Approval
	err  error
}

func (m *Model) loadMates(st rpc.State) {
	m.mateBoard = st.Mates
	m.mateLinks = map[string]domain.MateLink{}
	for _, l := range st.MateThreads {
		m.mateLinks[mateLinkKey(l)] = l
	}
}

func (m *Model) applyMates(d rpc.Diff) {
	if d.Mates != nil {
		m.mateBoard = *d.Mates
	}
	if d.MateThread != nil {
		if m.mateLinks == nil {
			m.mateLinks = map[string]domain.MateLink{}
		}
		m.mateLinks[mateLinkKey(*d.MateThread)] = *d.MateThread
	}
}

func mateLinkKey(l domain.MateLink) string {
	return domain.MateThreadKey(l.Team, l.Channel, l.ThreadTS, l.Mate)
}

func (m Model) openMates() (Model, tea.Cmd) {
	m.mp = &matesPanel{loading: true}
	return m, m.fetchApprovals()
}

func (m Model) fetchApprovals() tea.Cmd {
	c := m.opts.Calls
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var list []domain.Approval
		err := c.Call(ctx, rpc.MethodApprovalList, struct{}{}, &list)
		return mateApprovalsMsg{list: list, err: err}
	}
}

func (m Model) approvalsLoaded(msg mateApprovalsMsg) Model {
	if m.mp == nil {
		return m
	}
	p := *m.mp
	p.loading, p.approvals, p.err = false, msg.list, ""
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	p.cursor = min(p.cursor, max(len(m.mateBoard.Mates)+len(p.approvals)-1, 0))
	m.mp = &p
	return m
}

func (m Model) decideApproval(id string, approve bool) tea.Cmd {
	c := m.opts.Calls
	if c == nil {
		return nil
	}
	refresh := m.fetchApprovals()
	kept := m.mp.approvals
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		err := c.Call(ctx, rpc.MethodApprovalDecide, rpc.ApprovalDecideParams{ID: id, Approve: approve}, nil)
		out := refresh().(mateApprovalsMsg)
		if out.err != nil {
			out.list = kept
		}
		if err != nil {
			out.err = err
		}
		return out
	}
}

func (m Model) matesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.mp
	m.mp = &p
	k := msg.String()
	if p.stopping {
		p.stopping = false
		if k == "y" {
			return m, m.call(rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: true})
		}
		return m, nil
	}
	mates := m.mateBoard.Mates
	rows := len(mates) + len(p.approvals)
	switch k {
	case "esc", "q", "C":
		m.mp = nil
	case "j", "down":
		p.cursor = min(p.cursor+1, max(rows-1, 0))
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
	case "p":
		if p.cursor < len(mates) {
			method := rpc.MethodMatePause
			if mates[p.cursor].State == domain.MatePaused {
				method = rpc.MethodMateResume
			}
			return m, m.call(method, rpc.MateNameParams{Name: mates[p.cursor].Name})
		}
	case "X":
		if m.mateBoard.Stopped {
			return m, m.call(rpc.MethodMateStopAll, rpc.MateStopAllParams{Stop: false})
		}
		p.stopping = true
	case "y", "n":
		if i := p.cursor - len(mates); i >= 0 && i < len(p.approvals) {
			return m, m.decideApproval(p.approvals[i].ID, k == "y")
		}
	case "g":
		return m, m.fetchApprovals()
	}
	return m, nil
}

func mateStateWords(s domain.MateStatus) string {
	words := strings.ReplaceAll(string(s.State), "_", " ")
	if s.Guard != domain.GuardNone {
		words += " · " + strings.ReplaceAll(string(s.Guard), "_", " ")
	}
	return words
}

func (m Model) mateThreadsOf(name string) []domain.MateLink {
	var out []domain.MateLink
	for _, l := range m.mateLinks {
		if l.Mate == name {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ThreadTS > out[j].ThreadTS })
	return out
}

func (m Model) matePanelLines() ([]string, int) {
	s := m.styles
	p := m.mp
	out := []string{"", m.line(false, []piece{{s.header, " MATES"}}, nil), ""}
	if m.mateBoard.Stopped {
		out = append(out, m.line(false, []piece{{s.peach, " ■ all mates are stopped · X starts them"}}, nil), "")
	}
	sel := 0
	if len(m.mateBoard.Mates) == 0 {
		out = append(out, m.line(false, []piece{{s.sub, " No mates yet: crew mate add <name>"}}, nil))
	}
	for i, mt := range m.mateBoard.Mates {
		on := i == p.cursor
		bar := piece{s.text, " "}
		if on {
			bar, sel = piece{s.bar, "▌"}, len(out)
		}
		tone := s.dim
		switch {
		case mt.Guard != domain.GuardNone, mt.State == domain.MateError:
			tone = s.peach
		case mt.State == domain.MateConnected:
			tone = s.green
		}
		threads := m.mateThreadsOf(mt.Name)
		var work []piece
		for _, l := range threads {
			if se, ok := m.sessions[l.SessionID]; ok && !se.Ended && domain.MateWorking(se.State) {
				work = []piece{{s.blue, "working "}}
				break
			}
		}
		out = append(out, m.line(on, []piece{bar, {s.bold, " " + mt.Name + " "}, {tone, mateStateWords(mt)}}, work))
		if on {
			if mt.Error != "" {
				out = append(out, m.line(on, []piece{bar, {s.peach, "   " + cleanText(mt.Error)}}, nil))
			}
			for _, l := range threads[:min(len(threads), 5)] {
				name := l.SessionID
				state := "ended"
				if se, ok := m.sessions[l.SessionID]; ok {
					name = domain.NameFor(m.tasks[se.TaskID], nil)
					if !se.Ended {
						state = string(se.State)
					}
				}
				out = append(out, m.line(on, []piece{bar, {s.sub, "   " + cleanText(name)}}, []piece{{s.dim, state + " "}}))
			}
		}
	}
	out = append(out, "", m.line(false, []piece{{s.header, " APPROVALS"}}, nil))
	switch {
	case p.loading:
		out = append(out, m.line(false, []piece{{s.dim, " loading…"}}, nil))
	case p.err != "":
		out = append(out, m.line(false, []piece{{s.peach, " " + cleanText(p.err)}}, nil))
	case len(p.approvals) == 0:
		out = append(out, m.line(false, []piece{{s.sub, " No approvals wait for you."}}, nil))
	}
	for i, a := range p.approvals {
		on := len(m.mateBoard.Mates)+i == p.cursor
		bar := piece{s.text, " "}
		if on {
			bar, sel = piece{s.bar, "▌"}, len(out)
		}
		who := a.Requester
		if who == "" {
			who = "someone"
		}
		out = append(out, m.line(on, []piece{bar, {s.bold, " " + a.Mate + ": "}, {s.peach, strings.ReplaceAll(string(a.Action.Kind), "_", " ")}}, []piece{{s.dim, who + " "}}))
		if on {
			out = append(out, m.line(on, []piece{bar, {s.dim, "   " + cleanText(a.Action.Tool+": "+a.Action.Input)}}, nil))
		}
	}
	if p.stopping {
		return append(out, "", m.line(false, []piece{{s.peach, " stop all mates? y/n"}}, nil)), sel
	}
	if m.status != "" {
		out = append(out, "", m.line(false, []piece{{s.peach, " " + cleanText(m.status)}}, nil))
	}
	return append(out, "", m.line(false, []piece{{s.bold, " p"}, {s.dim, " pause "}, {s.bold, " X"}, {s.dim, " stop all "}, {s.bold, " y/n"}, {s.dim, " approve "}, {s.bold, " esc"}, {s.dim, " close"}}, nil)), sel
}
