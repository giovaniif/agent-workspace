package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func bashInput(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func fileInput(key, path string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{key: path})
	return b
}

var testScope = ActionScope{Root: "/w/api"}

func TestClassifyShellCommands(t *testing.T) {
	cases := []struct {
		cmd  string
		want ActionKind
	}{
		{"ls -la", ActionSafe},
		{"cat README.md", ActionSafe},
		{"grep -rn foo internal | head -20", ActionSafe},
		{"go test ./...", ActionSafe},
		{"make lint", ActionSafe},
		{"npm test", ActionSafe},
		{"git status", ActionSafe},
		{"git diff HEAD~1", ActionSafe},
		{"git log --oneline -5", ActionSafe},
		{"git add -A && git commit -m 'feat: x'", ActionSafe},
		{"git checkout -b 42-x", ActionSafe},
		{"git fetch origin", ActionSafe},
		{"git branch", ActionSafe},
		{"git branch -a", ActionSafe},
		{"git config user.name", ActionSafe},
		{"gh pr view 42", ActionSafe},
		{"gh pr list --state open", ActionSafe},
		{"gh issue view 7", ActionSafe},
		{"gh api repos/o/r/pulls", ActionSafe},
		{"curl -s https://example.com", ActionSafe},
		{"rm -rf build", ActionSafe},
		{"rm ./tmp/x.txt", ActionSafe},
		{"mkdir -p out && touch out/a", ActionSafe},
		{"echo hi > notes.txt", ActionSafe},
		{"go test ./... 2>/dev/null", ActionSafe},
		{"FOO=1 go test ./...", ActionSafe},
		{"env GOFLAGS=-count=1 go test ./...", ActionSafe},
		{"cd internal && go test ./...", ActionSafe},
		{"sed -n 1,20p main.go", ActionSafe},
		{"rm -rf /w/api/build", ActionSafe},
		{"echo 'git push'", ActionSafe},
		{"grep -n \"rm -rf /\" notes.txt", ActionSafe},
		{"", ActionSafe},

		{"git push", ActionPush},
		{"git push origin main --force", ActionPush},
		{"git -C /w/api push", ActionPush},
		{"git -c core.pager=cat push", ActionPush},
		{"git --no-pager push", ActionPush},
		{"go test ./... && git push", ActionPush},
		{"go test ./...; git push", ActionPush},
		{"go test ./... || git push", ActionPush},
		{"true | git push", ActionPush},
		{"go vet ./... & git push", ActionPush},
		{"go test ./...\ngit push", ActionPush},
		{"sh -c 'git push'", ActionPush},
		{"bash -c \"go test ./... && git push\"", ActionPush},
		{"bash -lc 'git push'", ActionPush},
		{"env git push", ActionPush},
		{"env -i PATH=/bin git push", ActionPush},
		{"FOO=bar git push", ActionPush},
		{"command git push", ActionPush},
		{"exec git push", ActionPush},
		{"nohup git push", ActionPush},
		{"timeout 30 git push", ActionPush},
		{"time git push", ActionPush},
		{"eval git push", ActionPush},
		{"eval 'git push'", ActionPush},
		{"g\"i\"t pu'sh'", ActionPush},
		{"/usr/bin/git push", ActionPush},
		{"(git push)", ActionPush},
		{"{ git push; }", ActionPush},
		{"echo y | xargs git push", ActionPush},

		{"gh pr create --fill", ActionCreatePR},
		{"gh pr merge 42 --squash", ActionMerge},
		{"git merge main", ActionMerge},
		{"git branch -D 42-x", ActionDeleteBranch},
		{"git branch -d old", ActionDeleteBranch},
		{"git branch --delete old", ActionDeleteBranch},

		{"rm -rf /", ActionOutsideWorktree},
		{"rm -rf ../other", ActionOutsideWorktree},
		{"rm -rf build/../../other", ActionOutsideWorktree},
		{"rm ~/x", ActionOutsideWorktree},
		{"rm -rf $HOME/x", ActionOutsideWorktree},
		{"cd .. && rm -rf api", ActionOutsideWorktree},
		{"cd /tmp && rm -rf x", ActionOutsideWorktree},
		{"echo x > /etc/hosts", ActionOutsideWorktree},
		{"echo x >> ../log", ActionOutsideWorktree},
		{"cp a.txt /tmp/a.txt", ActionOutsideWorktree},
		{"mv a.txt ../a.txt", ActionOutsideWorktree},
		{"tee /tmp/out < a.txt", ActionOutsideWorktree},
		{"sed -i s/a/b/ /etc/hosts", ActionOutsideWorktree},
		{"git -C /w/other commit -m x", ActionOutsideWorktree},
		{"git config --global user.name x", ActionOutsideWorktree},

		{"kubectl apply -f k8s.yaml", ActionDeploy},
		{"terraform apply", ActionDeploy},
		{"helm upgrade api ./chart", ActionDeploy},
		{"npm publish", ActionDeploy},
		{"docker push api:latest", ActionDeploy},
		{"vercel --prod", ActionDeploy},
		{"fly deploy", ActionDeploy},
		{"wrangler deploy", ActionDeploy},

		{"curl -X POST https://api.example.com/x", ActionExternalWrite},
		{"curl -d a=1 https://api.example.com/x", ActionExternalWrite},
		{"curl --data-binary @f https://x", ActionExternalWrite},
		{"curl -F f=@a https://x", ActionExternalWrite},
		{"gh api -X DELETE repos/o/r/git/refs/heads/x", ActionExternalWrite},
		{"gh api repos/o/r/issues -f title=x", ActionExternalWrite},
		{"gh issue comment 7 --body hi", ActionExternalWrite},
		{"gh release create v1", ActionExternalWrite},
		{"ssh host rm -rf /", ActionExternalWrite},
		{"scp a host:/tmp", ActionExternalWrite},

		{"cat .env", ActionSecret},
		{"cat ~/.ssh/id_rsa", ActionSecret},
		{"cat /home/u/.config/homelab/secrets.env", ActionSecret},
		{"printenv", ActionSecret},
		{"env", ActionSecret},
		{"grep TOKEN .env.local", ActionSecret},
		{"cat ~/.aws/credentials", ActionSecret},
		{"gh auth token", ActionSecret},

		{"frobnicate --all", ActionUnknown},
		{"sudo ls", ActionUnknown},
		{"echo $(git push)", ActionUnknown},
		{"echo `git push`", ActionUnknown},
		{"echo 'unterminated", ActionUnknown},
		{"git x", ActionUnknown},
		{"git config alias.x '!git push'", ActionUnknown},
		{"python3 -c 'import os'", ActionSafe},
		{"$CMD push", ActionUnknown},
		{"find . -name x -exec rm {} \\;", ActionUnknown},
		{"find / -delete", ActionOutsideWorktree},
		{"find . -name '*.tmp' -delete", ActionSafe},
	}
	for _, c := range cases {
		got := ClassifyAction(HarnessClaude, "Bash", bashInput(c.cmd), testScope)
		if got.Kind != c.want {
			t.Errorf("%q: got %s, want %s", c.cmd, got.Kind, c.want)
		}
	}
}

func TestClassifyFileTools(t *testing.T) {
	cases := []struct {
		tool, key, path string
		want            ActionKind
	}{
		{"Read", "file_path", "/w/api/main.go", ActionSafe},
		{"Read", "file_path", "/usr/share/doc/x", ActionSafe},
		{"Read", "file_path", "/w/api/.env", ActionSecret},
		{"Read", "file_path", "/home/u/.ssh/id_ed25519", ActionSecret},
		{"Read", "file_path", "/home/u/.crew/secrets/ada.bot", ActionSecret},
		{"Edit", "file_path", "/w/api/main.go", ActionSafe},
		{"Write", "file_path", "/w/api/sub/new.go", ActionSafe},
		{"Write", "file_path", "sub/new.go", ActionSafe},
		{"Write", "file_path", "/w/api/../other/x.go", ActionOutsideWorktree},
		{"Write", "file_path", "/w/api-old/x.go", ActionOutsideWorktree},
		{"Edit", "file_path", "/etc/hosts", ActionOutsideWorktree},
		{"MultiEdit", "file_path", "/home/u/.bashrc", ActionOutsideWorktree},
		{"NotebookEdit", "notebook_path", "/tmp/n.ipynb", ActionOutsideWorktree},
		{"Write", "file_path", "/w/api/.env", ActionSecret},
		{"Write", "file_path", "", ActionUnknown},
		{"Glob", "pattern", "**/*.go", ActionSafe},
		{"Grep", "pattern", "func main", ActionSafe},
		{"WebFetch", "url", "https://example.com", ActionSafe},
		{"WebSearch", "query", "go generics", ActionSafe},
		{"TodoWrite", "todos", "x", ActionSafe},
		{"Task", "prompt", "look", ActionSafe},
		{"mcp__slack__post_message", "text", "hi", ActionExternalWrite},
		{"SomeNewTool", "x", "y", ActionUnknown},
	}
	for _, c := range cases {
		got := ClassifyAction(HarnessClaude, c.tool, fileInput(c.key, c.path), testScope)
		if got.Kind != c.want {
			t.Errorf("%s %q: got %s, want %s", c.tool, c.path, got.Kind, c.want)
		}
	}
}

func TestClassifySymlinkOutOfTheWorktreeIsOutside(t *testing.T) {
	links := map[string]string{"/w/api/out": "/etc", "/w/api/in": "/w/api/sub"}
	scope := ActionScope{Root: "/w/api", Real: func(p string) string {
		for from, to := range links {
			if p == from || strings.HasPrefix(p, from+"/") {
				return to + strings.TrimPrefix(p, from)
			}
		}
		return p
	}}
	cases := []struct {
		tool  string
		input json.RawMessage
		want  ActionKind
	}{
		{"Write", fileInput("file_path", "/w/api/out/hosts"), ActionOutsideWorktree},
		{"Write", fileInput("file_path", "/w/api/in/a.go"), ActionSafe},
		{"Bash", bashInput("rm -rf out/x"), ActionOutsideWorktree},
		{"Bash", bashInput("echo x > in/a"), ActionSafe},
	}
	for _, c := range cases {
		if got := ClassifyAction(HarnessClaude, c.tool, c.input, scope); got.Kind != c.want {
			t.Errorf("%s %s: got %s, want %s", c.tool, c.input, got.Kind, c.want)
		}
	}
}

func TestClassifyUnparsableInputIsUnknown(t *testing.T) {
	for _, in := range []string{``, `not json`, `{"command": 3}`, `[]`} {
		if got := ClassifyAction(HarnessClaude, "Bash", json.RawMessage(in), testScope); got.Kind != ActionUnknown {
			t.Errorf("%q: got %s", in, got.Kind)
		}
	}
}

func TestClassifyCodexTools(t *testing.T) {
	cases := []struct {
		tool  string
		input string
		want  ActionKind
	}{
		{"Bash", `{"command":"git push"}`, ActionPush},
		{"Bash", `{"command":["bash","-lc","git push"]}`, ActionPush},
		{"Bash", `{"command":["go","test","./..."]}`, ActionSafe},
		{"apply_patch", `{"command":"*** Begin Patch\n*** Update File: main.go\n@@\n-a\n+b\n*** End Patch"}`, ActionSafe},
		{"apply_patch", `{"command":"*** Begin Patch\n*** Add File: /etc/x\n+b\n*** End Patch"}`, ActionOutsideWorktree},
		{"apply_patch", `{"command":"*** Begin Patch\n*** Delete File: ../x\n*** End Patch"}`, ActionOutsideWorktree},
	}
	for _, c := range cases {
		if got := ClassifyAction(HarnessCodex, c.tool, json.RawMessage(c.input), testScope); got.Kind != c.want {
			t.Errorf("%s %s: got %s, want %s", c.tool, c.input, got.Kind, c.want)
		}
	}
}

func TestClassifiedActionKeepsTheToolAndTheCompactInput(t *testing.T) {
	got := ClassifyAction(HarnessClaude, "Bash", json.RawMessage("{ \"command\" : \"git push\" }"), testScope)
	if got.Tool != "Bash" || got.Input != `{"command":"git push"}` || !got.Dangerous() {
		t.Fatalf("%+v", got)
	}
	if (Action{Kind: ActionSafe}).Dangerous() {
		t.Fatal("safe is dangerous")
	}
}

func TestGate(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	push := Action{Kind: ActionPush, Tool: "Bash", Input: `{"command":"git push"}`}
	other := Action{Kind: ActionPush, Tool: "Bash", Input: `{"command":"git push -f"}`}
	safe := Action{Kind: ActionSafe, Tool: "Read"}
	granted := func(a Action, at time.Time) Approval {
		return Approval{ID: "p1", Action: a, At: at, State: ApprovalGranted}
	}
	cases := []struct {
		name      string
		authority string
		owner     string
		action    Action
		approvals []Approval
		want      GateDecision
	}{
		{"safe from anyone", "U2", "U1", safe, nil, GateDecision{Allow: true, Use: -1}},
		{"safe with no owner", "U2", "", safe, nil, GateDecision{Allow: true, Use: -1}},
		{"owner runs dangerous", "U1", "U1", push, nil, GateDecision{Allow: true, Use: -1}},
		{"non-owner is denied", "U2", "U1", push, nil, GateDecision{Use: -1}},
		{"no authority is denied", "", "U1", push, nil, GateDecision{Use: -1}},
		{"no owner denies everyone", "", "", push, nil, GateDecision{Use: -1}},
		{"granted approval is used once", "U2", "U1", push, []Approval{granted(other, now), granted(push, now)}, GateDecision{Allow: true, Use: 1}},
		{"approval of another input does not count", "U2", "U1", push, []Approval{granted(other, now)}, GateDecision{Use: -1}},
		{"pending approval does not count", "U2", "U1", push, []Approval{{Action: push, At: now, State: ApprovalPending}}, GateDecision{Use: -1}},
		{"denied approval does not count", "U2", "U1", push, []Approval{{Action: push, At: now, State: ApprovalDenied}}, GateDecision{Use: -1}},
		{"expired approval does not count", "U2", "U1", push, []Approval{granted(push, now.Add(-ApprovalTTL-time.Second))}, GateDecision{Use: -1}},
		{"approval at the limit counts", "U2", "U1", push, []Approval{granted(push, now.Add(-ApprovalTTL))}, GateDecision{Allow: true, Use: 0}},
	}
	for _, c := range cases {
		if got := Gate(c.authority, c.owner, c.action, c.approvals, now); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestGateDenialOutputIsAClaudeDeny(t *testing.T) {
	var out struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(GateDenyOutput(Action{Kind: ActionPush}), &out); err != nil {
		t.Fatal(err)
	}
	h := out.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || !strings.Contains(h.PermissionDecisionReason, "waiting for owner approval") || !strings.Contains(h.PermissionDecisionReason, "push") {
		t.Fatalf("%+v", h)
	}
}
