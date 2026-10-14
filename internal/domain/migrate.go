package domain

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	HomeDirName       = ".crew"
	LegacyHomeDirName = ".agentws"
	TmuxServer        = "crew"
	LegacyTmuxServer  = "agentws"
	LegacyEnvPrefix   = "AGENTWS_"

	RepoConfigFile       = ".crew.toml"
	LegacyRepoConfigFile = ".agentws.toml"
	LegacyTurnPrefix     = "refs/agentws/turns/"
)

type HomeFacts struct {
	Env       string
	User      string
	NewExists bool
	OldDir    bool
}

func ResolveHome(f HomeFacts) string {
	switch {
	case f.Env != "":
		return f.Env
	case !f.NewExists && f.OldDir:
		return filepath.Join(f.User, LegacyHomeDirName)
	}
	return filepath.Join(f.User, HomeDirName)
}

type HomeMove int

const (
	HomeStay HomeMove = iota
	HomeMoveDir
	HomeBlocked
	HomeBoth
)

func PlanHomeMove(oldDir, newExists, oldDaemonLive bool) HomeMove {
	switch {
	case !oldDir:
		return HomeStay
	case newExists:
		return HomeBoth
	case oldDaemonLive:
		return HomeBlocked
	}
	return HomeMoveDir
}

func TmuxSocket(env string, legacyHasPanes bool) string {
	switch {
	case env != "":
		return env
	case legacyHasPanes:
		return LegacyTmuxServer
	}
	return TmuxServer
}

var legacyHook = regexp.MustCompile(`(^|[/"'\s])agentws'? hook --harness `)

func HasLegacyHook(content string) bool { return legacyHook.MatchString(content) }

var legacyBinary = regexp.MustCompile(`[^\s"'<>=]*/agentws([\s"'<])`)

func RewriteServiceFile(content, bin, oldHome, newHome string) string {
	content = legacyBinary.ReplaceAllString(content, bin+"$1")
	return strings.NewReplacer(
		oldHome, newHome,
		"dev.agentws.", "dev.crew.",
		"agentws-daemon", "crew-daemon",
		"agentws-serve", "crew-serve",
		"AGENTWS_HOME", "CREW_HOME",
	).Replace(content)
}

func LegacyServiceName(name string) (string, bool) {
	for _, p := range [][2]string{{"agentws-daemon.", "crew-daemon."}, {"agentws-serve.", "crew-serve."}, {"dev.agentws.", "dev.crew."}} {
		if strings.HasPrefix(name, p[0]) {
			return p[1] + strings.TrimPrefix(name, p[0]), true
		}
	}
	return "", false
}
