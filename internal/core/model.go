package core

import (
	"context"
	"time"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// ToolType は監視対象ツールの種別を表す。
type ToolType int

const (
	ToolClaude ToolType = iota
	ToolCodex
	ToolGemini
	ToolUnknown
)

// String は ToolType の文字列表現を返す。
func (t ToolType) String() string {
	switch t {
	case ToolClaude:
		return "claude"
	case ToolCodex:
		return "codex"
	case ToolGemini:
		return "gemini"
	default:
		return "unknown"
	}
}

// SessionState はセッションの現在状態を表す。
type SessionState int

const (
	// Idle は作業していない状態。
	Idle SessionState = iota
	// Thinking は推論中の状態。
	Thinking
	// ToolUse はツール実行中の状態。
	ToolUse
	// Waiting はツール承認待ちの状態。
	Waiting
	// Error はエラー状態。
	Error
)

// MarshalJSON は状態値を JSON 文字列として出力する。
func (s SessionState) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// String は SessionState の文字列表現を返す。
func (s SessionState) String() string {
	switch s {
	case Idle:
		return "idle"
	case Thinking:
		return "thinking"
	case ToolUse:
		return "tool_use"
	case Waiting:
		return "waiting"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// Session は監視対象となる1セッションの集約情報。
type Session struct {
	// v2 フィールド（プロセスベース監視）
	PID              int
	Tool             ToolType
	WorkingDir       string
	Branch           string
	CurrentTool      string
	FirstPrompt      string
	InputTokens      int
	OutputTokens     int
	Ambiguous        bool
	CandidatePaneIDs []int

	// 共通フィールド
	State        SessionState
	LastActivity time.Time

	// v1 互換フィールド（watcher / tui が参照。v2 完全移行後に削除予定）
	ID          string `json:"id,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	FilePath    string `json:"-"`
	PaneID      string `json:"pane_id,omitempty"`
}

// Project はプロジェクト単位にまとめたセッション情報。
type Project struct {
	// v2 フィールド
	Path      string
	Name      string
	Workspace string

	// v1 互換フィールド（tui が参照。v2 完全移行後に削除予定）
	DisplayName string     `json:"display_name,omitempty"`
	ActiveCount int        `json:"active_count,omitempty"`
	Sessions    []*Session `json:"sessions,omitempty"`
}

// DetectedProcess はプロセススキャンで検出された1プロセスを表す。
type DetectedProcess struct {
	PID      int
	Name     string
	ToolType ToolType
	PaneID   int
	TTY      string
	CWD      string
}

// ScanResult はプロセススキャンの結果を表す。
type ScanResult struct {
	Processes []DetectedProcess
	Panes     []terminal.Pane
	Timestamp time.Time
	Err       error
}

// Summary はセッション状態の集計情報を表す。
type Summary struct {
	TotalSessions int
	Active        int
	Waiting       int
	ByTool        map[string]int
}

// StateUpdater はスキャン結果から状態を更新するインターフェース。
type StateUpdater interface {
	UpdateFromScan(result ScanResult) error
}

// StateReader は集約済み状態への読み取り専用アクセスを定義する。
type StateReader interface {
	Projects() []Project
	Summary() Summary
	Panes() []terminal.Pane
	// GetProjects は v1 互換メソッド（tui が参照。v2 完全移行後に削除予定）
	GetProjects() []Project
}

// StateWriter は集約済み状態への更新操作を定義する（v1 互換）。
type StateWriter interface {
	HandleEvent(event WatchEvent) error
}

// Scanner はプロセス・ペイン情報をスキャンするインターフェース。
type Scanner interface {
	Scan(ctx context.Context) ScanResult
}

// --- 出力 DTO 型 ---

// StatusOutput は外部出力向けのステータスペイロード。
type StatusOutput struct {
	Version         int             `json:"version"`
	Timestamp       string          `json:"timestamp"`
	Projects        []ProjectOutput `json:"projects"`
	Summary         SummaryOutput   `json:"summary"`
	FormattedStatus string          `json:"formatted_status"`
}

// ProjectOutput はプロジェクト情報の出力 DTO。
type ProjectOutput struct {
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	Workspace string          `json:"workspace,omitempty"`
	Sessions  []SessionOutput `json:"sessions"`
}

// SessionOutput はセッション情報の出力 DTO。
type SessionOutput struct {
	PID          int    `json:"pid"`
	Tool         string `json:"tool"`
	State        string `json:"state"`
	PaneID       string `json:"pane_id,omitempty"`
	WorkingDir   string `json:"working_dir"`
	Branch       string `json:"branch,omitempty"`
	CurrentTool  string `json:"current_tool,omitempty"`
	FirstPrompt  string `json:"first_prompt,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
}

// SummaryOutput は集計情報の出力 DTO。
type SummaryOutput struct {
	TotalSessions int            `json:"total_sessions"`
	Active        int            `json:"active"`
	Waiting       int            `json:"waiting"`
	ByTool        map[string]int `json:"by_tool"`
}

// --- v1 互換型（watcher.go / state.go が参照。Phase 3 以降で削除予定） ---

// WatchEventType はファイル監視イベント種別を表す。
type WatchEventType int

const (
	Created WatchEventType = iota
	Modified
	Removed
)

// String は WatchEventType の文字列表現を返す。
func (t WatchEventType) String() string {
	switch t {
	case Created:
		return "created"
	case Modified:
		return "modified"
	case Removed:
		return "removed"
	default:
		return "unknown"
	}
}

// WatchEvent は正規化済みのファイル監視イベントを表す。
type WatchEvent struct {
	Type        WatchEventType
	Path        string
	ProjectPath string
	SessionID   string
}

// EventSource は監視イベントの読み取り専用チャネルを提供する。
type EventSource interface {
	Events() <-chan WatchEvent
}
