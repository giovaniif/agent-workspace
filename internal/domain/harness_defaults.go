package domain

import (
	"cmp"
	"strings"
)

func DefaultLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return "default (" + value + ")"
}

func ResolveDefault(h Harness, model, effort string) (string, string) {
	spec := Spec(h)
	return cmp.Or(model, spec.DefaultModel), cmp.Or(effort, spec.DefaultEffort)
}
