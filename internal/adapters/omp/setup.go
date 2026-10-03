package omp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const marker = "// agentws: written by agentws setup omp; agentws setup omp --remove deletes this file."

var ErrNotOurs = errors.New("a file agentws did not write is in the way")

type SetupConfig struct {
	Dir     string
	Command string
}

type SetupResult struct {
	Changed bool
	File    string
}

func AgentDir(env func(string) string) (string, error) {
	if dir := env("PI_CODING_AGENT_DIR"); dir != "" {
		return dir, nil
	}
	home := env("HOME")
	if home == "" {
		return "", errors.New("home directory is unknown")
	}
	return filepath.Join(home, ".omp", "agent"), nil
}

func HookFile(dir string) string { return filepath.Join(dir, "hooks", "post", "agentws.ts") }

func Setup(cfg SetupConfig) (SetupResult, error) {
	file := HookFile(cfg.Dir)
	want := hookSource(cfg.Command)
	current, exists, err := readOwned(file)
	if err != nil {
		return SetupResult{File: file}, err
	}
	switch domain.PlanOwnedInstall(marker, current, exists, want) {
	case domain.OwnedUnchanged:
		return SetupResult{File: file}, nil
	case domain.OwnedConflict:
		return SetupResult{File: file}, fmt.Errorf("%w: %s", ErrNotOurs, file)
	}
	if err := writeAtomic(file, want); err != nil {
		return SetupResult{File: file}, err
	}
	return SetupResult{Changed: true, File: file}, nil
}

// why: it asks whether Setup would change nothing, so it can never disagree with what Setup does.
func Installed(cfg SetupConfig) (bool, error) {
	file := HookFile(cfg.Dir)
	current, exists, err := readOwned(file)
	if err != nil {
		return false, err
	}
	switch domain.PlanOwnedInstall(marker, current, exists, hookSource(cfg.Command)) {
	case domain.OwnedUnchanged:
		return true, nil
	case domain.OwnedConflict:
		return false, fmt.Errorf("%w: %s", ErrNotOurs, file)
	}
	return false, nil
}

func Remove(cfg SetupConfig) (SetupResult, error) {
	file := HookFile(cfg.Dir)
	current, exists, err := readOwned(file)
	if err != nil {
		return SetupResult{File: file}, err
	}
	switch domain.PlanOwnedRemove(marker, current, exists) {
	case domain.OwnedConflict:
		return SetupResult{File: file}, fmt.Errorf("%w: %s", ErrNotOurs, file)
	case domain.OwnedDelete:
		if err := os.Remove(file); err != nil {
			return SetupResult{File: file}, err
		}
		return SetupResult{Changed: true, File: file}, nil
	}
	return SetupResult{File: file}, nil
}

func readOwned(file string) ([]byte, bool, error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// why: omp loads every file in hooks/post at startup, so it must never see a half-written one.
func writeAtomic(file string, content []byte) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agentws-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

func hookSource(command string) []byte {
	var names []string
	for name := range domain.Spec(domain.HarnessOmp).Hooks {
		names = append(names, name)
	}
	slices.Sort(names)
	return []byte(strings.NewReplacer(
		"{{marker}}", marker,
		"{{agentws}}", jsString(command),
		"{{events}}", jsList(names),
	).Replace(hookTemplate))
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func jsList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = jsString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

const hookTemplate = `{{marker}}
import { spawn } from "node:child_process";

const agentws = {{agentws}};
const events = {{events}};

function text(message: any): string | undefined {
	if (!message) return undefined;
	if (typeof message.content === "string") return message.content;
	if (!Array.isArray(message.content)) return undefined;
	return message.content.filter((c: any) => c?.type === "text").map((c: any) => c.text).join("\n");
}

function send(event: string, fields: Record<string, unknown>): Promise<void> {
	return new Promise(resolve => {
		try {
			const child = spawn(agentws, ["hook", "--harness", "omp", "--event", event], { stdio: ["pipe", "ignore", "ignore"] });
			child.on("error", () => resolve());
			child.on("close", () => resolve());
			child.stdin.on("error", () => {});
			child.stdin.end(JSON.stringify(fields));
		} catch {
			resolve();
		}
	});
}

export default function (pi: any) {
	for (const name of events) {
		pi.on(name, (event: any, ctx: any) => {
			if (name === "tool_approval_resolved" && event?.approved !== false) return;
			const sent = send(name, {
				session_id: ctx?.sessionManager?.getSessionId?.(),
				cwd: ctx?.cwd,
				model: ctx?.model?.provider && ctx?.model?.id ? ctx.model.provider + "/" + ctx.model.id : ctx?.model?.id,
				effort: pi.getThinkingLevel?.(),
				tool_name: event?.toolName,
				tool_input: event?.input,
				message: event?.reason,
				last_assistant_message: text(event?.last_assistant_message),
			});
			// why: omp fails a tool closed when its tool_call handler throws or stalls.
			if (name !== "tool_call") return sent;
		});
	}
}
`
