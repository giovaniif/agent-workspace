package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
	"github.com/giovaniif/crew/internal/rpc"
)

const (
	promptCaptureLines   = 80
	promptRawLines       = 40
	promptCaptureTimeout = 3 * time.Second
)

func (d *Daemon) promptTarget(req rpc.Request, id string) (domain.Session, app.PermissionPrompter, *rpc.Response, bool) {
	if d.hs.host == nil {
		return domain.Session{}, nil, errorResponse(req.ID, rpc.CodeUnavailable, "no terminal host"), true
	}
	var session domain.Session
	var found bool
	if !d.query(func(s *state) { session, found = s.sessions[id] }) {
		return domain.Session{}, nil, nil, false
	}
	if !found || session.Pane == "" {
		return domain.Session{}, nil, errorResponse(req.ID, rpc.CodeNotFound, "no session "+id), true
	}
	prompter, ok := d.hs.adapters[session.Harness].(app.PermissionPrompter)
	if !ok {
		return domain.Session{}, nil, errorResponse(req.ID, rpc.CodeUnavailable, "no permission prompts for "+string(session.Harness)), true
	}
	return session, prompter, nil, true
}

func (d *Daemon) capturePrompt(session domain.Session, prompter app.PermissionPrompter) (app.PermissionPrompt, string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), promptCaptureTimeout)
	defer cancel()
	screen, err := d.hs.host.Capture(ctx, app.PaneID(session.Pane), promptCaptureLines)
	if err != nil {
		return app.PermissionPrompt{}, "", false, err
	}
	prompt, ok := prompter.PermissionPrompt(screen)
	return prompt, screen, ok, nil
}

func promptID(prompt app.PermissionPrompt) string {
	h := sha256.New()
	h.Write([]byte(prompt.Text))
	for _, c := range prompt.Choices {
		h.Write([]byte{0})
		h.Write([]byte(c.ID + "\x00" + c.Label))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func visiblePane(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, " \t\r\n"), "\n")
	if len(lines) > promptRawLines {
		lines = lines[len(lines)-promptRawLines:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \t\r\n")
}

func (d *Daemon) sessionPrompt(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.PromptParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "prompt params: "+err.Error()), true
	}
	session, prompter, resp, ok := d.promptTarget(req, p.Session)
	if resp != nil || !ok {
		return resp, ok
	}
	prompt, screen, found, err := d.capturePrompt(session, prompter)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
	}
	if !found {
		if raw := visiblePane(screen); raw != "" && d.stillAwaitsPermission(session.ID) {
			return result(req.ID, rpc.Prompt{Choices: []rpc.PromptChoice{}, Raw: raw}), true
		}
		return errorResponse(req.ID, rpc.CodeNotFound, "no permission dialog is showing"), true
	}
	out := rpc.Prompt{ID: promptID(prompt), Text: prompt.Text, Choices: make([]rpc.PromptChoice, 0, len(prompt.Choices))}
	for _, c := range prompt.Choices {
		out.Choices = append(out.Choices, rpc.PromptChoice{ID: c.ID, Label: c.Label})
	}
	return result(req.ID, out), true
}

func (d *Daemon) sessionAnswer(req rpc.Request) (*rpc.Response, bool) {
	var p rpc.AnswerParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, rpc.CodeBadRequest, "answer params: "+err.Error()), true
	}
	session, prompter, resp, ok := d.promptTarget(req, p.Session)
	if resp != nil || !ok {
		return resp, ok
	}
	d.hs.sendMu.Lock()
	defer d.hs.sendMu.Unlock()
	if !d.stillAwaitsPermission(session.ID) {
		return errorResponse(req.ID, rpc.CodeStale, "session is no longer waiting for a permission"), true
	}
	prompt, _, found, err := d.capturePrompt(session, prompter)
	if err != nil {
		return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
	}
	if !found {
		return errorResponse(req.ID, rpc.CodeNotFound, "no permission dialog is showing"), true
	}
	if p.Prompt != "" && p.Prompt != promptID(prompt) {
		return errorResponse(req.ID, rpc.CodeStale, "the permission dialog changed since it was shown"), true
	}
	for _, c := range prompt.Choices {
		if c.ID != p.Choice {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), promptCaptureTimeout)
		defer cancel()
		if err := d.hs.host.SendKeys(ctx, app.PaneID(session.Pane), c.Keys...); err != nil {
			return errorResponse(req.ID, rpc.CodeFailed, err.Error()), true
		}
		return result(req.ID, struct{}{}), true
	}
	return errorResponse(req.ID, rpc.CodeBadRequest, "no choice "+p.Choice), true
}

func (d *Daemon) stillAwaitsPermission(id string) bool {
	awaiting := false
	d.query(func(s *state) {
		current, ok := s.sessions[id]
		awaiting = ok && current.State == domain.StatePermission
	})
	return awaiting
}
