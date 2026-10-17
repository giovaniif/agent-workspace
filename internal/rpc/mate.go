package rpc

import (
	"time"

	"github.com/giovaniif/crew/internal/domain"
)

const (
	MethodMateList   = "mate.list"
	MethodMateShow   = "mate.show"
	MethodMateSet    = "mate.set"
	MethodMateRemove = "mate.remove"
)

type Mate = domain.Mate

type MateList struct {
	Mates    []Mate            `json:"mates"`
	Problems []string          `json:"problems,omitempty"`
	States   []MateState       `json:"states,omitempty"`
	Limits   domain.MateLimits `json:"limits"`
}

type MateState = domain.MateStatus

const (
	MethodMatePause   = "mate.pause"
	MethodMateResume  = "mate.resume"
	MethodMateStopAll = "mate.stop_all"
)

type MateNameParams struct {
	Name string `json:"name"`
}

const MethodMateSetup = "mate.setup"

type MateSetup struct {
	Manifest string `json:"manifest"`
	URL      string `json:"url"`
}

type MateStopAllParams struct {
	Stop bool `json:"stop"`
}

type MateShowParams struct {
	Name string `json:"name"`
}

type MateSetParams struct {
	Mate Mate `json:"mate"`
}

type MateRemoveParams struct {
	Name string `json:"name"`
}

const (
	MethodMateSetTokens   = "mate.set_tokens"
	MethodMateClearTokens = "mate.clear_tokens"
)

type MateSetTokensParams struct {
	Name     string `json:"name"`
	BotToken string `json:"bot_token"`
	AppToken string `json:"app_token"`
}

type MateClearTokensParams struct {
	Name string `json:"name"`
}

type MateTokenStatus struct {
	Name        string `json:"name"`
	HasBotToken bool   `json:"has_bot_token"`
	HasAppToken bool   `json:"has_app_token"`
}

const MethodMateClaim = "mate.claim"

type MateClaimParams struct {
	Name string `json:"name"`
}

type MateClaim struct {
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ApprovalDecideParams struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
}
