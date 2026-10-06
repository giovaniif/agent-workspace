package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
	"github.com/giovaniif/agent-workspace/internal/rpc"
)

const (
	APIVersion         = "v1"
	DefaultAuthTimeout = 5 * time.Second
	maxBody            = 64 << 10
	codeForbidden      = "forbidden"
)

type Daemon interface {
	Call(ctx context.Context, method string, params, out any) error
	Subscribe(ctx context.Context) (rpc.Subscription, error)
	WatchTranscript(ctx context.Context, session string, after int64) (rpc.TranscriptWatch, error)
	Close() error
}

type Config struct {
	Build       string
	URL         string
	Dial        func(ctx context.Context) (Daemon, error)
	AuthTimeout time.Duration
	Log         io.Writer
	Now         func() time.Time
}

type Server struct {
	cfg     Config
	origin  string
	base    context.Context
	streams *registry
	log     *limitedLog
}

type Hello struct {
	API   string `json:"api"`
	Build string `json:"build"`
}

type ErrorBody struct {
	Error rpc.Error `json:"error"`
}

type route struct {
	pattern string
	handle  func(r *http.Request, d Daemon) (any, error)
}

func New(cfg Config) (*Server, error) {
	if cfg.AuthTimeout <= 0 {
		cfg.AuthTimeout = DefaultAuthTimeout
	}
	s := &Server{cfg: cfg, base: context.Background(), streams: newRegistry(), log: newLimitedLog(cfg.Log, cfg.Now)}
	if strings.TrimSpace(cfg.URL) != "" {
		origin, err := publicOrigin(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("public URL %q: %w", cfg.URL, err)
		}
		s.origin = origin
	}
	return s, nil
}

func (s *Server) routes() []route {
	return []route{
		{"GET /api/v1/workspaces", s.workspaces},
		{"POST /api/v1/sessions", newSession},
		{"GET /api/v1/work-items/resolve", resolveWorkItem},
		{"GET /api/v1/sessions/{id}/messages", messages},
		{"POST /api/v1/sessions/{id}/end", sessionAction(rpc.MethodEndSession, refParams)},
		{"POST /api/v1/sessions/{id}/resume", sessionAction(rpc.MethodResumeSession, refParams)},
		{"POST /api/v1/sessions/{id}/mute", sessionAction(rpc.MethodSessionMute, muteParams)},
		{"POST /api/v1/sessions/{id}/rename", sessionAction(rpc.MethodSessionRename, renameParams)},
		{"POST /api/v1/sessions/{id}/messages", sessionAction(rpc.MethodSessionSend, sendParams)},
		{"DELETE /api/v1/sessions/{id}/sends/{send}", sessionAction(rpc.MethodSessionUnsend, unsendParams)},
		{"POST /api/v1/sessions/{id}/interrupt", sessionAction(rpc.MethodSessionInterrupt, targetParams)},
		{"GET /api/v1/sessions/{id}/prompt", sessionAction(rpc.MethodSessionPrompt, promptParams)},
		{"POST /api/v1/sessions/{id}/answer", sessionAction(rpc.MethodSessionAnswer, answerParams)},
		{"GET /api/v1/push/key", pushKey},
		{"POST /api/v1/push/subscribe", pushSubscribe},
		{"POST /api/v1/push/unsubscribe", pushUnsubscribe},
	}
}

func (s *Server) Handler(fallback http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/hello", s.hello)
	mux.HandleFunc("POST /api/v1/pair", s.pair)
	mux.HandleFunc("GET /api/v1/stream", s.stream)
	for _, rt := range s.routes() {
		mux.Handle(rt.pattern, s.authed(rt.handle))
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &rpc.Error{Code: rpc.CodeNotFound, Message: "no endpoint " + r.Method + " " + r.URL.Path})
	})
	if fallback != nil {
		mux.Handle("/", fallback)
	}
	return mux
}

func (s *Server) hello(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Hello{API: APIVersion, Build: s.cfg.Build})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := decodeBody(r, &p); err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.dial(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer func() { _ = d.Close() }()
	var out rpc.PairRedeemed
	params := rpc.PairRedeemParams{Code: p.Code, Name: p.Name, Addr: clientAddr(r)}
	if err := d.Call(r.Context(), rpc.MethodPairRedeem, params, &out); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) authed(h func(r *http.Request, d Daemon) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			s.fail(w, r, &rpc.Error{Code: rpc.CodeUnauthorized, Message: "send Authorization: Bearer <device token>"})
			return
		}
		d, err := s.dial(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		defer func() { _ = d.Close() }()
		dev, err := s.check(r.Context(), d, token)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out, err := h(r.WithContext(context.WithValue(r.Context(), deviceKey{}, dev)), d)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func (s *Server) check(ctx context.Context, d Daemon, token string) (rpc.Device, error) {
	var out rpc.DeviceChecked
	if err := d.Call(ctx, rpc.MethodDeviceCheck, rpc.DeviceCheckParams{Token: token}, &out); err != nil {
		return rpc.Device{}, err
	}
	if s.streams.isRevoked(out.Device.ID) {
		return rpc.Device{}, &rpc.Error{Code: rpc.CodeUnauthorized, Message: "this device was revoked"}
	}
	return out.Device, nil
}

func (s *Server) dial(ctx context.Context) (Daemon, error) {
	d, err := s.cfg.Dial(ctx)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeUnavailable, Message: "the agentws daemon is not reachable: " + err.Error()}
	}
	return d, nil
}

func (s *Server) workspaces(r *http.Request, d Daemon) (any, error) {
	var out rpc.WorkspaceList
	if err := d.Call(r.Context(), rpc.MethodWorkspaceList, nil, &out); err != nil {
		return nil, err
	}
	if out.Workspaces == nil {
		out.Workspaces = []domain.Workspace{}
	}
	return out, nil
}

func messages(r *http.Request, d Daemon) (any, error) {
	p := rpc.TranscriptPageParams{Session: r.PathValue("id")}
	q := r.URL.Query()
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "before is a byte offset: " + v}
		}
		p.Before = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: "limit is a number of messages: " + v}
		}
		p.Limit = n
	}
	var out rpc.TranscriptPage
	if err := d.Call(r.Context(), rpc.MethodTranscriptPage, p, &out); err != nil {
		return nil, err
	}
	out.Messages = orEmpty(out.Messages)
	return out, nil
}

func sessionAction(method string, params func(r *http.Request, id string) (any, error)) func(r *http.Request, d Daemon) (any, error) {
	return func(r *http.Request, d Daemon) (any, error) {
		p, err := params(r, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		var out json.RawMessage
		if err := d.Call(r.Context(), method, p, &out); err != nil {
			return nil, err
		}
		if len(out) == 0 {
			out = json.RawMessage(`{}`)
		}
		return out, nil
	}
}

func refParams(_ *http.Request, id string) (any, error) {
	return rpc.SessionRef{ID: id}, nil
}

func muteParams(r *http.Request, id string) (any, error) {
	var body struct {
		Muted *bool `json:"muted"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if body.Muted == nil {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: `send {"muted": true} or {"muted": false}`}
	}
	return rpc.SessionMuteParams{ID: id, Muted: *body.Muted}, nil
}

func renameParams(r *http.Request, id string) (any, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	return rpc.SessionRenameParams{ID: id, Name: body.Name}, nil
}

func sendParams(r *http.Request, id string) (any, error) {
	var body struct {
		Text string `json:"text"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if !domain.SendableText(body.Text) {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: `send {"text": "<message>"} with some text`}
	}
	return rpc.SessionSendParams{Session: id, Text: body.Text}, nil
}

func promptParams(_ *http.Request, id string) (any, error) {
	return rpc.PromptParams{Session: id}, nil
}

func answerParams(r *http.Request, id string) (any, error) {
	var body struct {
		Choice string `json:"choice"`
		Prompt string `json:"prompt"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Choice) == "" {
		return nil, &rpc.Error{Code: rpc.CodeBadRequest, Message: `answer {"choice": "<id from the prompt>"}`}
	}
	return rpc.AnswerParams{Session: id, Choice: body.Choice, Prompt: body.Prompt}, nil
}

func unsendParams(r *http.Request, id string) (any, error) {
	return rpc.SessionUnsendParams{Session: id, ID: r.PathValue("send")}, nil
}

func targetParams(_ *http.Request, id string) (any, error) {
	return rpc.SessionTarget{Session: id}, nil
}

func decodeBody(r *http.Request, out any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody)).Decode(out); err != nil {
		return &rpc.Error{Code: rpc.CodeBadRequest, Message: "the body is not the JSON this endpoint takes: " + err.Error()}
	}
	return nil
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	return token, token != ""
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return r.RemoteAddr
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(last) != nil {
			return last
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(real) != nil {
		return real
	}
	return r.RemoteAddr
}

func publicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return originOf(raw)
}

func originOf(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "https" && scheme != "http") || u.Host == "" {
		return "", errors.New("want an http or https URL with a host")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, nil
}

func statusOf(code string) int {
	switch code {
	case rpc.CodeBadRequest:
		return http.StatusBadRequest
	case rpc.CodeUnauthorized:
		return http.StatusUnauthorized
	case codeForbidden:
		return http.StatusForbidden
	case rpc.CodeNotFound:
		return http.StatusNotFound
	case rpc.CodeStale:
		return http.StatusConflict
	case rpc.CodeRateLimited:
		return http.StatusTooManyRequests
	case rpc.CodeUnknownMethod:
		return http.StatusNotImplemented
	case rpc.CodeVersionMismatch:
		return http.StatusBadGateway
	case rpc.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func apiError(err error) *rpc.Error {
	var rerr *rpc.Error
	if errors.As(err, &rerr) {
		return rerr
	}
	return &rpc.Error{Code: rpc.CodeUnavailable, Message: "the agentws daemon is not reachable: " + err.Error()}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := apiError(err)
	status := statusOf(e.Code)
	if status == http.StatusUnauthorized {
		s.log.printf("%s %s %d %s from %s: %s", r.Method, r.URL.Path, status, e.Code, clientAddr(r), e.Message)
	}
	writeJSON(w, status, ErrorBody{Error: *e})
}

func writeError(w http.ResponseWriter, err error) {
	e := apiError(err)
	writeJSON(w, statusOf(e.Code), ErrorBody{Error: *e})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
