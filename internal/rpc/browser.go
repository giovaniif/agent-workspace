package rpc

import (
	"encoding/json"

	"github.com/giovaniif/crew/internal/domain"
)

const (
	MethodBrowserAttach  = "browser.attach"
	MethodBrowserCommand = "browser.command"
	MethodBrowserReply   = "browser.reply"
)

const NoBrowser = "no browser: the macOS app is not connected"

type BrowserCommandParams struct {
	Session   string         `json:"session,omitempty"`
	Pane      string         `json:"pane,omitempty"`
	Command   string         `json:"command"`
	Args      map[string]any `json:"args,omitempty"`
	TimeoutMS int            `json:"timeout_ms,omitempty"`
}

type BrowserCall struct {
	Call    uint64          `json:"call"`
	Session string          `json:"session"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}

type BrowserReplyParams struct {
	Call   uint64          `json:"call"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

const MethodBrowserCookiesImport = "browser.cookies.import"

type BrowserCookiesParams struct {
	Cookies []domain.BrowserCookie `json:"cookies"`
}

const MethodBrowserHeadlessView = "browser.headless.view"

type BrowserHeadlessViewParams struct {
	Session string `json:"session"`
}

type BrowserHeadlessView struct {
	Live   bool   `json:"live"`
	PNG    string `json:"png,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type BrowserCookiesResult struct {
	Imported int `json:"imported"`
	Total    int `json:"total"`
}
