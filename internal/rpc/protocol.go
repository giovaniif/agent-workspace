package rpc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/giovaniif/agent-workspace/internal/domain"
)

const Version = 1

const (
	MethodStatus           = "status"
	MethodSubscribe        = "subscribe"
	MethodNotifyStream     = "notify.stream"
	MethodHook             = "hook"
	MethodWorkspaceAdd     = "workspace.add"
	MethodWorkspaceList    = "workspace.list"
	MethodWorkspaceRemove  = "workspace.remove"
	MethodWorkspaceDirs    = "workspace.dirs"
	MethodOpenClient       = "client.open"
	MethodFocusMain        = "client.focus_main"
	MethodDebugSeed        = "debug.seed"
	MethodStatusLine       = "statusline"
	MethodLaunch           = "session.launch"
	MethodSessionMute      = "session.mute"
	MethodSessionFocus     = "session.focus"
	MethodWorktreeAssign   = "worktree.assign"
	MethodNewSession       = "session.new"
	MethodSessionResolve   = "session.resolve"
	MethodEndSession       = "session.end"
	MethodResumeSession    = "session.resume"
	MethodSessionRename    = "session.rename"
	MethodSessionUnpin     = "session.unpin"
	MethodLauncherEnqueue  = "launcher.enqueue"
	MethodLauncherDrop     = "launcher.drop"
	MethodLauncherRetarget = "launcher.retarget"
	MethodPortsKill        = "ports.kill"
	MethodReviewOpen       = "review.open"
	MethodReviewViewed     = "review.viewed"
	MethodReviewSend       = "review.send"
	MethodReviewHunk       = "review.hunk"
	MethodClientReview     = "client.review"
	MethodClientPopup      = "client.popup"
	MethodClientDetach     = "client.detach"
	MethodCleanupPlan      = "cleanup.plan"
	MethodCleanupRun       = "cleanup.run"
	MethodDiskView         = "disk.view"
	MethodCleanupWorktree  = "cleanup.worktree"
	MethodSessionSend      = "session.send"
	MethodSessionUnsend    = "session.unsend"
	MethodSessionInterrupt = "session.interrupt"
	MethodPairCode         = "pair.code"
	MethodPairRedeem       = "pair.redeem"
	MethodDeviceCheck      = "device.check"
	MethodDeviceList       = "device.list"
	MethodDeviceRevoke     = "device.revoke"
	MethodPushKey          = "push.key"
	MethodPushSubscribe    = "push.subscribe"
	MethodPushUnsubscribe  = "push.unsubscribe"
	MethodDeviceViewing    = "device.viewing"
)

type PairCodeParams struct {
	Name string `json:"name,omitempty"`
}

type PairCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PairRedeemParams struct {
	Code string `json:"code"`
	Name string `json:"name,omitempty"`
	Addr string `json:"addr"`
}

type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

func DeviceOf(d domain.Device) Device {
	return Device{ID: d.ID, Name: d.Name, CreatedAt: d.Created, LastSeen: d.LastSeen}
}

type PairRedeemed struct {
	Device Device `json:"device"`
	Token  string `json:"token"`
}

type DeviceCheckParams struct {
	Token string `json:"token"`
}

type DeviceChecked struct {
	Device Device `json:"device"`
}

type DeviceList struct {
	Devices []Device `json:"devices"`
}

type DeviceRevokeParams struct {
	ID string `json:"id"`
}

type PushKey struct {
	PublicKey string `json:"public_key"`
}

type PushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type DeviceViewingParams struct {
	Device  string `json:"device"`
	Visible bool   `json:"visible"`
}

type PushUnsubscribeParams struct {
	Device string `json:"device"`
}

type PushSubscribeParams struct {
	Device   string   `json:"device"`
	Endpoint string   `json:"endpoint"`
	Keys     PushKeys `json:"keys"`
}

type DiskView struct {
	Free           uint64           `json:"free"`
	Total          uint64           `json:"total"`
	AutoCleanEvery time.Duration    `json:"auto_clean_every"`
	DepsStore      *DepsStore       `json:"deps_store,omitempty"`
	Rows           []domain.DiskRow `json:"rows"`
	Recent         []RecentCleanup  `json:"recent"`
}

type DepsStore struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type RecentCleanup struct {
	At      time.Time            `json:"at"`
	Path    string               `json:"path"`
	Branch  string               `json:"branch,omitempty"`
	Action  domain.CleanupAction `json:"action"`
	Outcome string               `json:"outcome"`
}

type CleanupWorktreeParams struct {
	Path   string `json:"path"`
	Backup bool   `json:"backup,omitempty"`
}

type ReviewParams struct {
	Session  string             `json:"session"`
	Scope    domain.ReviewScope `json:"scope"`
	Worktree string             `json:"worktree,omitempty"`
}

type Review struct {
	Scope     domain.ReviewScope      `json:"scope"`
	Worktrees []domain.WorktreeReview `json:"worktrees"`
	Viewed    []domain.ViewedMark     `json:"viewed"`
	Draft     domain.ReviewDraft      `json:"draft"`
}

type ReviewSendParams struct {
	Session string `json:"session"`
}

type SessionSendParams struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

type SessionSent struct {
	ID     string `json:"id"`
	Queued bool   `json:"queued"`
}

type SessionUnsendParams struct {
	Session string `json:"session"`
	ID      string `json:"id"`
}

type SessionTarget struct {
	Session string `json:"session"`
}

type HunkParams struct {
	Session  string            `json:"session"`
	Worktree string            `json:"worktree"`
	File     domain.FileDiff   `json:"file"`
	Hunk     int               `json:"hunk"`
	Action   domain.HunkAction `json:"action"`
}

type ViewedParams struct {
	Mark   domain.ViewedMark `json:"mark"`
	Viewed bool              `json:"viewed"`
}

type ClientReviewParams struct {
	Open bool `json:"open"`
}

type ClientPopupParams struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

type PortsKillParams struct {
	PGIDs []int `json:"pgids"`
}

type PortsKilled struct {
	Killed []int `json:"killed"`
}

type CleanupItem struct {
	Path    string               `json:"path"`
	Branch  string               `json:"branch,omitempty"`
	Action  domain.CleanupAction `json:"action"`
	Reason  string               `json:"reason"`
	Outcome string               `json:"outcome,omitempty"`
}

type SessionMuteParams struct {
	ID    string `json:"id"`
	Muted bool   `json:"muted"`
}

type SessionRenameParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SessionFocusParams struct {
	ID string `json:"id"`
}

type NewSessionParams struct {
	Workspace string `json:"workspace,omitempty"`
	WorkItem  string `json:"work_item"`
	Harness   string `json:"harness"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

type ResolveWorkItemParams struct {
	Workspace string `json:"workspace,omitempty"`
	WorkItem  string `json:"work_item"`
}

type WorkItemResolved struct {
	Source    string `json:"source"`
	Ref       string `json:"ref,omitempty"`
	Title     string `json:"title,omitempty"`
	Worktree  string `json:"worktree"`
	Workspace string `json:"workspace"`
}

type LauncherEnqueueParams struct {
	Workspace string `json:"workspace,omitempty"`
	Input     string `json:"input"`
	Harness   string `json:"harness"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
}

type LauncherEnqueued struct {
	Queued   []string `json:"queued"`
	Rejected []string `json:"rejected"`
}

type LauncherItemRef struct {
	ID string `json:"id"`
}

type LauncherRetargetParams struct {
	ID      string `json:"id"`
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

type SessionRef struct {
	ID string `json:"id"`
}

type StatusLine struct {
	Pane   string              `json:"pane"`
	Report domain.StatusReport `json:"report"`
}

type LaunchParams struct {
	Harness string `json:"harness"`
	Name    string `json:"name,omitempty"`
	Dir     string `json:"dir"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
}

type Hook struct {
	Harness string          `json:"harness"`
	Event   string          `json:"event"`
	Pane    string          `json:"pane"`
	At      time.Time       `json:"at"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type HookReply struct {
	Output json.RawMessage `json:"output,omitempty"`
}

const (
	CodeUnsupportedVersion = "unsupported_version"
	CodeUnknownMethod      = "unknown_method"
	CodeBadRequest         = "bad_request"
	CodeNotFound           = "not_found"
	CodeUnavailable        = "unavailable"
	CodeFailed             = "failed"
	CodeLaunchFailed       = "launch_failed"
	CodeVersionMismatch    = "version_mismatch"
	CodeUnauthorized       = "unauthorized"
	CodeRateLimited        = "rate_limited"
	CodeStale              = "stale"
)

func AnyBuild(method string) bool {
	return method == MethodStatus || method == MethodHook || method == MethodStatusLine
}

func Mismatch(daemonBuild, clientBuild string, daemonBuilt, clientBuilt int64) *Error {
	fix := "restart the daemon: run `agentws daemon stop`; the next command starts the new one"
	if clientBuilt != 0 && daemonBuilt > clientBuilt {
		fix = "restart this client: it is older than the daemon"
	}
	if daemonBuild == "" {
		daemonBuild = "an unknown build"
	}
	return &Error{Code: CodeVersionMismatch, Message: fmt.Sprintf("daemon runs agentws %s but this client is %s; %s", daemonBuild, clientBuild, fix)}
}

type WorkspaceAddParams struct {
	Path string `json:"path"`
}

type WorkspaceDirsParams struct {
	Path string `json:"path"`
}

type WorkspaceDirs struct {
	Dirs []domain.Child `json:"dirs"`
}

type WorkspaceRemoveParams struct {
	Root string `json:"root"`
}

type WorkspaceList struct {
	Workspaces []domain.Workspace `json:"workspaces"`
	LastUsed   string             `json:"last_used"`
}

type OpenClientParams struct {
	Command  []string          `json:"command"`
	Env      map[string]string `json:"env,omitempty"`
	Dir      string            `json:"dir,omitempty"`
	Terminal string            `json:"terminal,omitempty"`
}

type OpenClient struct {
	Slot   string   `json:"slot"`
	Attach []string `json:"attach"`
}

type DebugSeedParams struct {
	Count int  `json:"count"`
	Codex bool `json:"codex,omitempty"`
}

type WorktreeAssignParams struct {
	ID      string `json:"id"`
	Session string `json:"session"`
}

type Request struct {
	V       int             `json:"v"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Build   string          `json:"build,omitempty"`
	BuiltAt int64           `json:"built_at,omitempty"`
}

type Response struct {
	V          int              `json:"v"`
	ID         uint64           `json:"id"`
	Result     json.RawMessage  `json:"result,omitempty"`
	Diff       *Diff            `json:"diff,omitempty"`
	Notice     *Notice          `json:"notice,omitempty"`
	Transcript *TranscriptEvent `json:"transcript,omitempty"`
	Error      *Error           `json:"error,omitempty"`
	Build      string           `json:"build,omitempty"`
}

type Notice struct {
	Banner  *domain.Banner `json:"banner,omitempty"`
	Remove  string         `json:"remove,omitempty"`
	Focused bool           `json:"focused,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type Status struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Sessions  int       `json:"sessions"`
	Worktrees int       `json:"worktrees"`
}

type State struct {
	Seq        uint64                `json:"seq"`
	Workspaces []domain.Workspace    `json:"workspaces"`
	Tasks      []domain.Task         `json:"tasks"`
	Worktrees  []domain.Worktree     `json:"worktrees"`
	Sessions   []domain.Session      `json:"sessions"`
	Events     []domain.SessionEvent `json:"events"`
	Subagents  []domain.Subagent     `json:"subagents"`
	Queue      []domain.LaunchItem   `json:"queue"`
	Drafts     []domain.ReviewDraft  `json:"drafts"`
	Sends      []domain.QueuedSend   `json:"sends"`
}

type Diff struct {
	Seq              uint64                `json:"seq"`
	RemovedWorkspace string                `json:"removed_workspace,omitempty"`
	RemovedWorktree  string                `json:"removed_worktree,omitempty"`
	RemovedSession   string                `json:"removed_session,omitempty"`
	RevokedDevice    string                `json:"revoked_device,omitempty"`
	Workspace        *domain.Workspace     `json:"workspace,omitempty"`
	Task             *domain.Task          `json:"task,omitempty"`
	Worktree         *domain.Worktree      `json:"worktree,omitempty"`
	Session          *domain.Session       `json:"session,omitempty"`
	Event            *domain.SessionEvent  `json:"event,omitempty"`
	Subagent         *domain.Subagent      `json:"subagent,omitempty"`
	Queue            *[]domain.LaunchItem  `json:"queue,omitempty"`
	Draft            *domain.ReviewDraft   `json:"draft,omitempty"`
	Comment          *domain.ReviewComment `json:"comment,omitempty"`
	Sends            *[]domain.QueuedSend  `json:"sends,omitempty"`
}

func Home() (string, error) {
	if home := os.Getenv("AGENTWS_HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agentws"), nil
}

func SocketPath(home string) string { return filepath.Join(home, "agentws.sock") }
