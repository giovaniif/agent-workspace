package app

type MateSecrets interface {
	SetMateTokens(name, bot, app string) error
	ClearMateTokens(name string) error
	HasMateTokens(name string) (bot, app bool)
}
