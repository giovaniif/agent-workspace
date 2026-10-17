package domain

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
)

type ActionKind string

const (
	ActionSafe            ActionKind = "safe"
	ActionMerge           ActionKind = "merge"
	ActionCreatePR        ActionKind = "create_pr"
	ActionPush            ActionKind = "push"
	ActionDeleteBranch    ActionKind = "delete_branch"
	ActionOutsideWorktree ActionKind = "outside_worktree"
	ActionDeploy          ActionKind = "deploy"
	ActionExternalWrite   ActionKind = "external_write"
	ActionSecret          ActionKind = "secret"
	ActionUnknown         ActionKind = "unknown"
)

type Action struct {
	Kind  ActionKind
	Tool  string
	Input string
}

func (a Action) Dangerous() bool {
	return a.Kind != ActionSafe
}

type MateSeat struct {
	Name      string
	Owner     string
	Authority string
	Origin    string       `json:",omitempty"`
	Asked     []ActionKind `json:",omitempty"`
	Hops      int          `json:",omitempty"`
}

type ActionScope struct {
	Root string
	Real func(string) string
}

type ApprovalState string

const (
	ApprovalPending ApprovalState = "pending"
	ApprovalGranted ApprovalState = "granted"
	ApprovalDenied  ApprovalState = "denied"
	ApprovalUsed    ApprovalState = "used"
)

type Approval struct {
	ID        string
	SessionID string
	Mate      string
	Requester string
	Channel   string
	ThreadTS  string
	Message   string
	Action    Action
	At        time.Time
	State     ApprovalState
}

const ApprovalTTL = 24 * time.Hour

type GateDecision struct {
	Allow bool
	Use   int
}

func Gate(authority, owner string, action Action, approvals []Approval, now time.Time) GateDecision {
	if !action.Dangerous() {
		return GateDecision{Allow: true, Use: -1}
	}
	if owner != "" && authority == owner {
		return GateDecision{Allow: true, Use: -1}
	}
	for i, a := range approvals {
		if a.State == ApprovalGranted && a.Action == action && !a.Expired(now) {
			return GateDecision{Allow: true, Use: i}
		}
	}
	return GateDecision{Use: -1}
}

func GateDenyOutput(action Action) json.RawMessage {
	out := map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": "waiting for owner approval: " + strings.ReplaceAll(string(action.Kind), "_", " ") + " needs the owner of this mate",
	}}
	b, _ := json.Marshal(out)
	return b
}

func ClassifyAction(h Harness, tool string, input json.RawMessage, scope ActionScope) Action {
	a := Action{Tool: tool, Kind: ActionUnknown}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil || fields == nil {
		return a
	}
	buf, _ := json.Marshal(fields)
	a.Input = string(buf)
	a.Kind = classifyTool(tool, fields, scope)
	return a
}

var readTools = map[string]bool{
	"Glob": true, "Grep": true, "LS": true, "NotebookRead": true, "WebFetch": true, "WebSearch": true,
	"TodoWrite": true, "Task": true, "Agent": true, "ExitPlanMode": true, "AskUserQuestion": true,
	"BashOutput": true, "KillShell": true, "ToolSearch": true,
}

var writeTools = map[string]string{
	"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path",
}

func classifyTool(tool string, fields map[string]json.RawMessage, scope ActionScope) ActionKind {
	switch {
	case tool == "Grep" && isSecretPath(stringField(fields, "path")):
		return ActionSecret
	case readTools[tool]:
		return ActionSafe
	case tool == "Read":
		if isSecretPath(stringField(fields, "file_path")) {
			return ActionSecret
		}
		return ActionSafe
	case writeTools[tool] != "":
		p := stringField(fields, writeTools[tool])
		if p == "" {
			return ActionUnknown
		}
		return writeKind(newShellCtx(scope), p)
	case tool == "Bash" || tool == "shell" || tool == "exec_command" || tool == "local_shell":
		return classifyCommandField(fields["command"], scope)
	case tool == "apply_patch" || tool == "ApplyPatch":
		return classifyPatch(fields, scope)
	case strings.HasPrefix(tool, "mcp__"):
		return ActionExternalWrite
	}
	return ActionUnknown
}

func stringField(fields map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(fields[key], &s)
	return s
}

func classifyCommandField(raw json.RawMessage, scope ActionScope) ActionKind {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return ClassifyShell(s, scope)
	}
	var argv []string
	if err := json.Unmarshal(raw, &argv); err == nil && len(argv) > 0 {
		c := newShellCtx(scope)
		return c.command(shellCmd{words: argv})
	}
	return ActionUnknown
}

func classifyPatch(fields map[string]json.RawMessage, scope ActionScope) ActionKind {
	text := stringField(fields, "command")
	if text == "" {
		text = stringField(fields, "input")
	}
	if text == "" {
		return ActionUnknown
	}
	c := newShellCtx(scope)
	for _, line := range strings.Split(text, "\n") {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if p, ok := strings.CutPrefix(line, prefix); ok {
				if k := writeKind(c, strings.TrimSpace(p)); k != ActionSafe {
					return k
				}
			}
		}
	}
	return ActionSafe
}

func ClassifyShell(script string, scope ActionScope) ActionKind {
	c := newShellCtx(scope)
	return c.script(script)
}

type shellCtx struct {
	root  string
	real  func(string) string
	cwd   string
	depth int
}

func newShellCtx(scope ActionScope) *shellCtx {
	real := scope.Real
	if real == nil {
		real = func(p string) string { return p }
	}
	root := path.Clean(scope.Root)
	return &shellCtx{root: root, real: real, cwd: root}
}

const maxShellDepth = 6

func (c *shellCtx) script(script string) ActionKind {
	if c.depth > maxShellDepth {
		return ActionUnknown
	}
	cmds, err := parseShell(script)
	if err != nil {
		return ActionUnknown
	}
	for _, cmd := range cmds {
		if k := c.command(cmd); k != ActionSafe {
			return k
		}
	}
	return ActionSafe
}

func (c *shellCtx) nested(script string) ActionKind {
	inner := *c
	inner.depth++
	return inner.script(script)
}

type shellCmd struct {
	words  []string
	writes []string
	reads  []string
}

var errShell = errors.New("shell: cannot parse")

func parseShell(s string) ([]shellCmd, error) {
	var cmds []shellCmd
	var cur shellCmd
	var word strings.Builder
	inWord, quoted := false, false
	pending := ""
	flushCmd := func() {
		if len(cur.words) > 0 || len(cur.writes) > 0 || len(cur.reads) > 0 {
			cmds = append(cmds, cur)
		}
		cur = shellCmd{}
	}
	flushWord := func() error {
		if !inWord {
			return nil
		}
		w := word.String()
		word.Reset()
		inWord, quoted = false, false
		switch pending {
		case "":
			cur.words = append(cur.words, w)
		case ">":
			cur.writes = append(cur.writes, w)
		case "<":
			cur.reads = append(cur.reads, w)
		}
		pending = ""
		return nil
	}
	redirect := func(op string) error {
		if inWord && !quoted && isDigits(word.String()) {
			word.Reset()
			inWord = false
		}
		if err := flushWord(); err != nil {
			return err
		}
		if pending != "" {
			return errShell
		}
		pending = op
		return nil
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\':
			if i+1 >= len(s) {
				return nil, errShell
			}
			i++
			if s[i] != '\n' {
				word.WriteByte(s[i])
				inWord = true
				quoted = true
			}
		case ch == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, errShell
			}
			word.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord, quoted = true, true
		case ch == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				switch {
				case s[j] == '\\' && j+1 < len(s):
					j++
					word.WriteByte(s[j])
				case s[j] == '`' || (s[j] == '$' && j+1 < len(s) && s[j+1] == '('):
					return nil, errShell
				default:
					word.WriteByte(s[j])
				}
			}
			if j >= len(s) {
				return nil, errShell
			}
			i = j
			inWord, quoted = true, true
		case ch == '`' || (ch == '$' && i+1 < len(s) && s[i+1] == '('):
			return nil, errShell
		case ch == '#' && !inWord:
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				i = len(s)
			} else {
				i += end - 1
			}
		case ch == ' ' || ch == '\t':
			if err := flushWord(); err != nil {
				return nil, err
			}
		case ch == '\n' || ch == ';' || ch == '|' || ch == '(' || ch == ')':
			if err := flushWord(); err != nil {
				return nil, err
			}
			if pending != "" {
				return nil, errShell
			}
			flushCmd()
		case ch == '&':
			if i+1 < len(s) && s[i+1] == '>' {
				if err := redirect(">"); err != nil {
					return nil, err
				}
				i++
				if i+1 < len(s) && s[i+1] == '>' {
					i++
				}
				continue
			}
			if err := flushWord(); err != nil {
				return nil, err
			}
			if pending != "" {
				return nil, errShell
			}
			flushCmd()
		case ch == '>' || ch == '<':
			op := string(ch)
			if err := redirect(op); err != nil {
				return nil, err
			}
			for i+1 < len(s) && (s[i+1] == '>' || s[i+1] == '<' || s[i+1] == '|') {
				i++
			}
			if i+1 < len(s) && s[i+1] == '&' {
				i++
				j := i + 1
				for j < len(s) && (isDigit(s[j]) || s[j] == '-') {
					j++
				}
				if j > i+1 {
					i = j - 1
					pending = ""
				}
			}
		default:
			word.WriteByte(ch)
			inWord = true
		}
	}
	if err := flushWord(); err != nil {
		return nil, err
	}
	if pending != "" {
		return nil, errShell
	}
	flushCmd()
	return cmds, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isNameByte(b byte) bool {
	return b == '_' || isDigit(b) || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func isAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		if !isNameByte(w[i]) {
			return false
		}
	}
	return true
}

func (c *shellCtx) command(cmd shellCmd) ActionKind {
	words := cmd.words
	for len(words) > 0 && isAssignment(words[0]) {
		words = words[1:]
	}
	k := c.words(words)
	if k != ActionSafe {
		return k
	}
	for _, w := range cmd.writes {
		if k := writeKind(c, w); k != ActionSafe {
			return k
		}
	}
	for _, r := range cmd.reads {
		if isSecretPath(r) {
			return ActionSecret
		}
	}
	return ActionSafe
}

func skipFlags(args []string, withValue string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		f := args[0]
		args = args[1:]
		if f == "--" {
			break
		}
		if len(f) == 2 && strings.ContainsRune(withValue, rune(f[1])) && len(args) > 0 {
			args = args[1:]
		}
	}
	return args
}

func (c *shellCtx) words(words []string) ActionKind {
	if len(words) == 0 {
		return ActionSafe
	}
	if strings.Contains(words[0], "$") {
		return ActionUnknown
	}
	name := path.Base(words[0])
	args := words[1:]
	switch name {
	case "{", "}", "!":
		return c.words(args)
	case "env":
		rest := skipFlags(args, "uCS")
		for _, f := range args {
			if f == "-S" || f == "-C" || strings.HasPrefix(f, "--split-string") || strings.HasPrefix(f, "--chdir") ||
				(strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.ContainsAny(f, "CS")) {
				return ActionUnknown
			}
		}
		for len(rest) > 0 && isAssignment(rest[0]) {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return ActionSecret
		}
		return c.words(rest)
	case "command", "builtin", "exec", "nohup", "time":
		return c.words(skipFlags(args, ""))
	case "nice":
		return c.words(skipFlags(args, "n"))
	case "timeout":
		rest := skipFlags(args, "sk")
		if len(rest) == 0 {
			return ActionSafe
		}
		return c.words(rest[1:])
	case "xargs":
		return c.words(skipFlags(args, "InPLdsEa"))
	case "sudo", "su", "doas":
		return ActionUnknown
	case "eval":
		c.depth++
		k := c.script(strings.Join(args, " "))
		c.depth--
		return k
	case "sh", "bash", "zsh", "dash", "ksh":
		return c.shell(args)
	}
	k := c.program(name, args)
	if k != ActionSafe {
		return k
	}
	for _, a := range args {
		if isSecretPath(a) {
			return ActionSecret
		}
	}
	return ActionSafe
}

func (c *shellCtx) shell(args []string) ActionKind {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			return ActionUnknown
		}
		if !strings.HasPrefix(a, "--") && strings.ContainsRune(a, 'c') {
			if i+1 >= len(args) {
				return ActionUnknown
			}
			return c.nested(args[i+1])
		}
	}
	return ActionUnknown
}

var safePrograms = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "less": true, "more": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true, "ag": true, "wc": true, "sort": true, "uniq": true, "cut": true, "tr": true,
	"echo": true, "printf": true, "pwd": true, "true": true, "false": true, "test": true, "[": true, "which": true,
	"type": true, "date": true, "sleep": true, "basename": true, "dirname": true, "realpath": true, "readlink": true,
	"stat": true, "file": true, "du": true, "df": true, "tree": true, "diff": true, "cmp": true, "comm": true,
	"jq": true, "yq": true, "xxd": true, "od": true, "hexdump": true, "md5sum": true, "sha256sum": true,
	"shasum": true, "seq": true, "nl": true, "fold": true, "column": true, "paste": true, "join": true, "tac": true,
	"rev": true, "go": true, "gofmt": true, "goimports": true, "golangci-lint": true, "gopls": true, "make": true,
	"cmake": true, "ninja": true, "tsc": true, "pip": true, "pip3": true, "pytest": true, "ruff": true, "black": true, "mypy": true,
	"rustc": true, "rustfmt": true, "java": true, "javac": true, "mvn": true, "gradle": true, "swift": true,
	"export": true, "unset": true, "set": true, ":": true, "shellcheck": true, "staticcheck": true,
}

var writePrograms = map[string]bool{
	"rm": true, "rmdir": true, "mv": true, "touch": true, "mkdir": true, "chmod": true, "chown": true,
	"truncate": true, "tee": true, "unlink": true, "shred": true,
}

var targetPrograms = map[string]bool{"cp": true, "ln": true, "install": true}

var externalPrograms = map[string]bool{
	"ssh": true, "scp": true, "rsync": true, "sftp": true, "nc": true, "ncat": true, "telnet": true, "ftp": true,
	"mail": true, "sendmail": true, "aws": true, "gcloud": true, "az": true,
}

var secretPrograms = map[string]bool{
	"printenv": true, "security": true, "pass": true, "op": true, "vault": true, "keyring": true,
}

var deployPrograms = map[string]bool{
	"vercel": true, "netlify": true, "fly": true, "flyctl": true, "heroku": true, "firebase": true,
	"serverless": true, "sls": true, "eb": true, "ansible-playbook": true, "twine": true,
}

var deployReads = map[string]map[string]bool{
	"kubectl":   {"get": true, "describe": true, "logs": true, "explain": true, "version": true, "diff": true},
	"terraform": {"plan": true, "validate": true, "fmt": true, "init": true, "show": true, "output": true, "version": true},
	"helm":      {"list": true, "template": true, "lint": true, "status": true, "show": true, "version": true},
	"wrangler":  {"dev": true, "whoami": true, "tail": true},
}

func firstArg(args []string) string {
	rest := skipFlags(args, "")
	if len(rest) == 0 {
		return ""
	}
	return rest[0]
}

func (c *shellCtx) program(name string, args []string) ActionKind {
	switch {
	case name == "cd":
		c.cd(args)
		return ActionSafe
	case name == "git":
		return c.git(args)
	case name == "gh":
		return ghKind(args)
	case name == "curl":
		return curlKind(args)
	case name == "wget":
		for _, a := range args {
			if strings.HasPrefix(a, "--post") || strings.HasPrefix(a, "--method") || strings.HasPrefix(a, "--body") {
				return ActionExternalWrite
			}
		}
		return ActionSafe
	case name == "python" || name == "python3" || name == "node" || name == "deno":
		return interpreterKind(name, args)
	case name == "awk":
		for _, a := range args {
			if strings.Contains(a, "system") || strings.ContainsAny(a, "|>") || strings.Contains(a, "getline") {
				return ActionUnknown
			}
		}
		return ActionSafe
	case name == "find":
		return c.find(args)
	case name == "sed":
		return c.sed(args)
	case name == "dd":
		for _, a := range args {
			if p, ok := strings.CutPrefix(a, "of="); ok {
				return writeKind(c, p)
			}
		}
		return ActionSafe
	case name == "npm" || name == "pnpm" || name == "yarn" || name == "bun" || name == "cargo" || name == "gem":
		switch firstArg(args) {
		case "publish", "push", "deploy":
			return ActionDeploy
		}
		return ActionSafe
	case name == "docker" || name == "podman":
		switch firstArg(args) {
		case "push":
			return ActionDeploy
		case "build", "ps", "images", "logs", "inspect", "version", "info", "compose":
			return ActionSafe
		}
		return ActionUnknown
	case deployReads[name] != nil:
		if deployReads[name][firstArg(args)] {
			return ActionSafe
		}
		return ActionDeploy
	case deployPrograms[name]:
		return ActionDeploy
	case externalPrograms[name]:
		return ActionExternalWrite
	case secretPrograms[name]:
		return ActionSecret
	case writePrograms[name]:
		if name == "rm" || name == "rmdir" || name == "mv" || name == "shred" {
			for _, a := range skipFlags(args, "") {
				if r := c.resolve(a); r == c.root {
					return ActionOutsideWorktree
				}
			}
		}
		return c.writeAll(skipFlags(args, ""))
	case targetPrograms[name]:
		return c.target(args)
	case safePrograms[name]:
		return ActionSafe
	}
	return ActionUnknown
}

func interpreterKind(name string, args []string) ActionKind {
	if name == "deno" {
		switch firstArg(args) {
		case "eval", "repl":
			return ActionUnknown
		}
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return ActionSafe
		}
		if a == "-c" || a == "-e" || a == "-p" || a == "-" || strings.HasPrefix(a, "--eval") || strings.HasPrefix(a, "--print") ||
			(!strings.HasPrefix(a, "--") && strings.ContainsAny(a[1:], "cep")) {
			return ActionUnknown
		}
	}
	return ActionUnknown
}

var gitRunsCommand = []string{
	"alias", "sshcommand", "fsmonitor", "hookspath", "editor", "external", "textconv", "helper", "program",
	"filter.", "askpass", "proxycommand", "uploadpack", "receivepack", "sequence.", "difftool", "mergetool",
	"tool.", "cmd",
}

var gitSafePagers = map[string]bool{"cat": true, "less": true, "more": true, "": true}

func gitKeyRunsCommand(kv string) bool {
	lower := strings.ToLower(kv)
	key, val, _ := strings.Cut(lower, "=")
	for _, k := range gitRunsCommand {
		if strings.Contains(key, k) {
			return true
		}
	}
	if strings.Contains(key, "pager") {
		return !gitSafePagers[val]
	}
	return false
}

func (c *shellCtx) writeAll(paths []string) ActionKind {
	for _, p := range paths {
		if strings.HasPrefix(p, "-") {
			continue
		}
		if k := writeKind(c, p); k != ActionSafe {
			return k
		}
	}
	return ActionSafe
}

func (c *shellCtx) target(args []string) ActionKind {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return writeKind(c, args[i+1])
		}
		if p, ok := strings.CutPrefix(a, "--target-directory="); ok {
			return writeKind(c, p)
		}
	}
	var paths []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return ActionSafe
	}
	return writeKind(c, paths[len(paths)-1])
}

func (c *shellCtx) cd(args []string) {
	rest := skipFlags(args, "")
	if len(rest) == 0 || rest[0] == "-" {
		c.cwd = ""
		return
	}
	c.cwd = c.resolve(rest[0])
}

func (c *shellCtx) find(args []string) ActionKind {
	var roots []string
	i := 0
	for ; i < len(args) && !strings.HasPrefix(args[i], "-") && args[i] != "(" && args[i] != "!"; i++ {
		roots = append(roots, args[i])
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, a := range args[i:] {
		switch a {
		case "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprintf", "-fls":
			return ActionUnknown
		case "-delete":
			return c.writeAll(roots)
		}
	}
	return ActionSafe
}

func (c *shellCtx) sed(args []string) ActionKind {
	inPlace, script := false, false
	var operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-e" || a == "-f" || a == "--expression" || a == "--file":
			script = true
			i++
		case strings.HasPrefix(a, "-i") || strings.HasPrefix(a, "--in-place"):
			inPlace = true
		case strings.HasPrefix(a, "-"):
		default:
			operands = append(operands, a)
		}
	}
	if !inPlace {
		return ActionSafe
	}
	if !script && len(operands) > 0 {
		operands = operands[1:]
	}
	return c.writeAll(operands)
}

var gitSafe = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true, "add": true, "commit": true, "checkout": true,
	"switch": true, "restore": true, "reset": true, "stash": true, "fetch": true, "pull": true, "rebase": true,
	"cherry-pick": true, "revert": true, "tag": true, "rev-parse": true, "rev-list": true, "ls-files": true,
	"ls-tree": true, "blame": true, "grep": true, "describe": true, "shortlog": true, "init": true, "mv": true,
	"rm": true, "clean": true, "apply": true, "am": true, "format-patch": true, "merge-base": true,
	"cat-file": true, "reflog": true, "show-ref": true, "for-each-ref": true, "symbolic-ref": true,
	"name-rev": true, "range-diff": true, "whatchanged": true, "help": true, "version": true, "bisect": true,
	"notes": true, "worktree": true, "remote": true, "submodule": true, "lfs": true, "sparse-checkout": true,
}

func (c *shellCtx) git(args []string) ActionKind {
	outside := false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		f := args[0]
		args = args[1:]
		switch {
		case f == "-C" && len(args) > 0:
			if writeKind(c, args[0]) != ActionSafe {
				outside = true
			}
			args = args[1:]
		case f == "-c" && len(args) > 0:
			if gitKeyRunsCommand(args[0]) {
				return ActionUnknown
			}
			args = args[1:]
		case strings.HasPrefix(f, "--git-dir=") || strings.HasPrefix(f, "--work-tree="):
			_, p, _ := strings.Cut(f, "=")
			if writeKind(c, p) != ActionSafe {
				outside = true
			}
		}
	}
	if len(args) == 0 {
		return ActionSafe
	}
	sub, rest := args[0], args[1:]
	k := ActionSafe
	switch {
	case sub == "push":
		return ActionPush
	case sub == "merge":
		return ActionMerge
	case sub == "branch":
		for _, a := range rest {
			if a == "-d" || a == "-D" || a == "--delete" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "dD")) {
				return ActionDeleteBranch
			}
		}
	case sub == "config":
		for _, a := range rest {
			if gitKeyRunsCommand(a) {
				return ActionUnknown
			}
			switch a {
			case "--global", "--system", "--file", "-f":
				k = ActionOutsideWorktree
			}
		}
	case sub == "submodule" && slicesContain(rest, "foreach"), sub == "bisect" && firstArg(rest) == "run":
		return ActionUnknown
	case sub == "rebase":
		for _, a := range rest {
			if a == "-x" || strings.HasPrefix(a, "--exec") || (strings.HasPrefix(a, "-x") && !strings.HasPrefix(a, "--")) {
				return ActionUnknown
			}
		}
	case !gitSafe[sub]:
		return ActionUnknown
	}
	if k == ActionSafe && outside {
		return ActionOutsideWorktree
	}
	return k
}

var ghReadVerbs = map[string]bool{
	"view": true, "list": true, "diff": true, "checks": true, "status": true, "checkout": true, "watch": true,
	"download": true,
}

func ghKind(args []string) ActionKind {
	rest := skipFlags(args, "R")
	if len(rest) == 0 {
		return ActionSafe
	}
	group := rest[0]
	verb := ""
	if len(rest) > 1 {
		verb = rest[1]
	}
	switch group {
	case "pr":
		switch verb {
		case "create":
			return ActionCreatePR
		case "merge":
			return ActionMerge
		}
	case "api":
		return ghAPIKind(rest[1:])
	case "auth":
		switch verb {
		case "token":
			return ActionSecret
		case "status":
			return ActionSafe
		}
		return ActionUnknown
	case "secret":
		return ActionSecret
	case "search", "browse", "help", "version", "--version":
		return ActionSafe
	}
	if ghReadVerbs[verb] {
		return ActionSafe
	}
	return ActionExternalWrite
}

func slicesContain(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func ghAPIKind(args []string) ActionKind {
	for i, a := range args {
		switch {
		case a == "-X" || a == "--method":
			if i+1 < len(args) && !strings.EqualFold(args[i+1], "GET") {
				return ActionExternalWrite
			}
		case strings.HasPrefix(a, "--method="):
			if !strings.EqualFold(strings.TrimPrefix(a, "--method="), "GET") {
				return ActionExternalWrite
			}
		case strings.HasPrefix(a, "-X") && len(a) > 2:
			if !strings.EqualFold(a[2:], "GET") {
				return ActionExternalWrite
			}
		case strings.HasPrefix(a, "-f") || strings.HasPrefix(a, "-F") || strings.HasPrefix(a, "--field") ||
			strings.HasPrefix(a, "--raw-field") || strings.HasPrefix(a, "--input"):
			return ActionExternalWrite
		}
	}
	return ActionSafe
}

func curlWriteMethod(m string) bool {
	m = strings.ToUpper(m)
	return m != "GET" && m != "HEAD"
}

const curlValueFlags = "oHAeuxwrbcKEmzCYyQ"

func curlKind(args []string) ActionKind {
	for i, a := range args {
		switch {
		case a == "--request" && i+1 < len(args):
			if curlWriteMethod(args[i+1]) {
				return ActionExternalWrite
			}
		case strings.HasPrefix(a, "--request="):
			if curlWriteMethod(strings.TrimPrefix(a, "--request=")) {
				return ActionExternalWrite
			}
		case strings.HasPrefix(a, "--data") || strings.HasPrefix(a, "--form") || strings.HasPrefix(a, "--json") ||
			strings.HasPrefix(a, "--upload-file"):
			return ActionExternalWrite
		case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--"):
			if curlShortKind(a[1:], args[i+1:]) {
				return ActionExternalWrite
			}
		}
	}
	return ActionSafe
}

func curlShortKind(group string, next []string) bool {
	for j := 0; j < len(group); j++ {
		ch := group[j]
		switch {
		case ch == 'd' || ch == 'F' || ch == 'T':
			return true
		case ch == 'X':
			m := group[j+1:]
			if m == "" && len(next) > 0 {
				m = next[0]
			}
			return curlWriteMethod(m)
		case strings.IndexByte(curlValueFlags, ch) >= 0:
			return false
		}
	}
	return false
}

func (c *shellCtx) resolve(p string) string {
	if p == "" || strings.HasPrefix(p, "~") || strings.Contains(p, "$") {
		return ""
	}
	if !path.IsAbs(p) {
		if c.cwd == "" {
			return ""
		}
		p = path.Join(c.cwd, p)
	}
	return c.real(path.Clean(p))
}

func (c *shellCtx) inside(p string) bool {
	return p != "" && (p == c.root || strings.HasPrefix(p, c.root+"/"))
}

var devFiles = map[string]bool{"/dev/null": true, "/dev/stdout": true, "/dev/stderr": true, "/dev/tty": true}

func writeKind(c *shellCtx, p string) ActionKind {
	if devFiles[p] {
		return ActionSafe
	}
	if isSecretPath(p) {
		return ActionSecret
	}
	if !c.inside(c.resolve(p)) {
		return ActionOutsideWorktree
	}
	return ActionSafe
}

func isSecretPath(p string) bool {
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)
	base := path.Base(lower)
	if strings.ContainsAny(lower, "*?[") {
		return globMatchesSecret(lower)
	}
	if strings.HasPrefix(base, ".env") || base == ".netrc" || base == "credentials" || base == "secrets.env" ||
		strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa") ||
		strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	for _, dir := range []string{".ssh/", ".aws/", ".gnupg/", "secrets/", ".config/gh/"} {
		if strings.HasPrefix(lower, dir) || strings.Contains(lower, "/"+dir) {
			return true
		}
	}
	return false
}

var secretSamples = []string{
	".env", ".env.local", ".netrc", "credentials", "secrets.env", "id_rsa", "id_ed25519", "id_ecdsa", "x.pem", "x.key",
}

var secretDirs = []string{".ssh", ".aws", ".gnupg", "secrets", "gh"}

func globMatchesSecret(p string) bool {
	parts := strings.Split(p, "/")
	base := parts[len(parts)-1]
	for _, s := range secretSamples {
		if ok, err := path.Match(base, s); err != nil || ok {
			return true
		}
	}
	for _, part := range parts[:len(parts)-1] {
		for _, d := range secretDirs {
			if ok, err := path.Match(part, d); err != nil || ok {
				return true
			}
		}
	}
	return false
}
