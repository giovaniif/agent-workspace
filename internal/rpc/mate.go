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
