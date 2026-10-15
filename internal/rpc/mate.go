package rpc

import "github.com/giovaniif/crew/internal/domain"

const (
	MethodMateList   = "mate.list"
	MethodMateShow   = "mate.show"
	MethodMateSet    = "mate.set"
	MethodMateRemove = "mate.remove"
)

type Mate = domain.Mate

type MateList struct {
	Mates    []Mate   `json:"mates"`
	Problems []string `json:"problems,omitempty"`
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
