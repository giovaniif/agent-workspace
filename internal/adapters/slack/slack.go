package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/giovaniif/crew/internal/app"
	"github.com/giovaniif/crew/internal/domain"
)

const (
	defaultAPI = "https://slack.com/api/"
	seenMax    = 512
	readLimit  = 1 << 20
)

var ErrToken = errors.New("slack rejected the token")

var tokenErrors = []string{"invalid_auth", "not_authed", "account_inactive", "token_revoked", "token_expired", "not_allowed_token_type", "missing_scope"}

type Client struct {
	Bot     string
	App     string
	API     string
	HTTP    *http.Client
	Backoff func(attempt int) time.Duration

	selfUser string
	selfBot  string
	seen     []string
}

func New(bot, appToken string) *Client {
	return &Client{Bot: bot, App: appToken, API: defaultAPI, HTTP: &http.Client{Timeout: 30 * time.Second}, Backoff: Backoff}
}

func Backoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 6)
	return min(d, time.Minute)
}

type apiError struct {
	method string
	code   string
}

func (e *apiError) Error() string { return fmt.Sprintf("slack %s: %s", e.method, e.code) }

func (e *apiError) Is(target error) bool {
	return target == ErrToken && slices.Contains(tokenErrors, e.code)
}

func (c *Client) call(ctx context.Context, method, token string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.API+method, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("slack %s: bad request", method)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("slack %s: the request failed", method)
	}
	defer func() { _ = resp.Body.Close() }()
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("slack %s: HTTP %d", method, resp.StatusCode)
	}
	var head struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return fmt.Errorf("slack %s: a bad reply", method)
	}
	if !head.OK {
		if head.Error == "" {
			head.Error = "unknown_error"
		}
		return &apiError{method: method, code: head.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("slack %s: a bad reply", method)
	}
	return nil
}

func (c *Client) PostMessage(ctx context.Context, channel, threadTS, text string) (string, error) {
	form := url.Values{"channel": {channel}, "text": {text}}
	if threadTS != "" {
		form.Set("thread_ts", threadTS)
	}
	var out struct {
		TS string `json:"ts"`
	}
	err := c.call(ctx, "chat.postMessage", c.Bot, form, &out)
	return out.TS, err
}

func (c *Client) UpdateMessage(ctx context.Context, channel, ts, text string) error {
	return c.call(ctx, "chat.update", c.Bot, url.Values{"channel": {channel}, "ts": {ts}, "text": {text}}, nil)
}

func (c *Client) Replies(ctx context.Context, channel, threadTS string) ([]app.SlackMessage, error) {
	var out struct {
		Messages []struct {
			User  string `json:"user"`
			BotID string `json:"bot_id"`
			Text  string `json:"text"`
			TS    string `json:"ts"`
		} `json:"messages"`
	}
	if err := c.call(ctx, "conversations.replies", c.Bot, url.Values{"channel": {channel}, "ts": {threadTS}}, &out); err != nil {
		return nil, err
	}
	msgs := make([]app.SlackMessage, len(out.Messages))
	for i, m := range out.Messages {
		msgs[i] = app.SlackMessage{User: m.User, Text: m.Text, TS: m.TS, Bot: m.BotID != ""}
	}
	return msgs, nil
}

func (c *Client) UserName(ctx context.Context, user string) (string, error) {
	var out struct {
		User struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Profile  struct {
				DisplayName string `json:"display_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := c.call(ctx, "users.info", c.Bot, url.Values{"user": {user}}, &out); err != nil {
		return "", err
	}
	for _, name := range []string{out.User.Profile.DisplayName, out.User.RealName, out.User.Name} {
		if name != "" {
			return name, nil
		}
	}
	return user, nil
}

func (c *Client) Run(ctx context.Context, handle func(app.SlackEvent), status func(app.SlackStatus)) error {
	backoff := c.Backoff
	if backoff == nil {
		backoff = Backoff
	}
	attempt := 0
	for {
		status(app.SlackStatus{State: domain.MateConnecting})
		connected, err := c.connect(ctx, handle, status)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrToken) {
			status(app.SlackStatus{State: domain.MateError, Error: err.Error()})
			return err
		}
		if err != nil {
			status(app.SlackStatus{State: domain.MateError, Error: err.Error()})
		}
		if connected {
			attempt = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(attempt)):
		}
		attempt++
	}
}

func (c *Client) connect(ctx context.Context, handle func(app.SlackEvent), status func(app.SlackStatus)) (bool, error) {
	if c.selfUser == "" {
		var self struct {
			UserID string `json:"user_id"`
			BotID  string `json:"bot_id"`
		}
		if err := c.call(ctx, "auth.test", c.Bot, url.Values{}, &self); err != nil {
			return false, err
		}
		c.selfUser, c.selfBot = self.UserID, self.BotID
	}
	var open struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, "apps.connections.open", c.App, url.Values{}, &open); err != nil {
		return false, err
	}
	conn, _, err := websocket.Dial(ctx, open.URL, &websocket.DialOptions{HTTPClient: c.HTTP})
	if err != nil {
		return false, errors.New("slack: the socket did not open")
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(readLimit)
	connected := false
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return connected, nil
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if env.EnvelopeID != "" {
			ack, _ := json.Marshal(map[string]string{"envelope_id": env.EnvelopeID})
			if err := conn.Write(ctx, websocket.MessageText, ack); err != nil {
				return connected, nil
			}
		}
		switch env.Type {
		case "hello":
			connected = true
			status(app.SlackStatus{State: domain.MateConnected})
		case "disconnect":
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return connected, nil
		case "events_api":
			key := env.EnvelopeID
			var p eventsPayload
			if json.Unmarshal(env.Payload, &p) != nil {
				continue
			}
			if p.EventID != "" {
				key = p.EventID
			}
			if c.dup(key) {
				continue
			}
			if e, ok := c.event(p); ok {
				handle(e)
			}
		case "interactive":
			if c.dup(env.EnvelopeID) {
				continue
			}
			var p actionPayload
			if json.Unmarshal(env.Payload, &p) != nil || p.Type != "block_actions" {
				continue
			}
			for _, a := range p.Actions {
				handle(app.SlackEvent{
					Kind: app.SlackAction, Team: p.Team.ID, Channel: p.Channel.ID, User: p.User.ID,
					TS: p.Message.TS, ThreadTS: p.Message.ThreadTS, ActionID: a.ActionID, Value: a.Value,
				})
			}
		}
	}
}

func (c *Client) dup(key string) bool {
	if key == "" {
		return false
	}
	if slices.Contains(c.seen, key) {
		return true
	}
	c.seen = append(c.seen, key)
	if len(c.seen) > seenMax {
		c.seen = c.seen[len(c.seen)-seenMax:]
	}
	return false
}

type envelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
}

type eventsPayload struct {
	TeamID  string `json:"team_id"`
	EventID string `json:"event_id"`
	Event   struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		User        string `json:"user"`
		BotID       string `json:"bot_id"`
		Text        string `json:"text"`
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
		TS          string `json:"ts"`
		ThreadTS    string `json:"thread_ts"`
	} `json:"event"`
}

type actionPayload struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		TS       string `json:"ts"`
		ThreadTS string `json:"thread_ts"`
	} `json:"message"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

func (c *Client) event(p eventsPayload) (app.SlackEvent, bool) {
	ev := p.Event
	if ev.User == "" || ev.Subtype != "" || ev.User == c.selfUser || ev.BotID != "" && ev.BotID == c.selfBot {
		return app.SlackEvent{}, false
	}
	e := app.SlackEvent{Team: p.TeamID, Channel: ev.Channel, User: ev.User, Text: ev.Text, TS: ev.TS, ThreadTS: ev.ThreadTS}
	if e.ThreadTS == "" {
		e.ThreadTS = ev.TS
	}
	switch {
	case ev.Type == "app_mention":
		e.Kind = app.SlackMention
	case ev.Type == "message" && ev.ChannelType == "im":
		e.Kind = app.SlackDirectMessage
	case ev.Type == "message" && ev.ThreadTS != "" && ev.ThreadTS != ev.TS:
		e.Kind = app.SlackThreadMessage
	default:
		return app.SlackEvent{}, false
	}
	return e, true
}
