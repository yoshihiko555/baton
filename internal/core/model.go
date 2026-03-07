package core

import (
	"encoding/json"
	"time"
)

// SessionState はセッションの現在状態を表す。
type SessionState int

const (
	// Idle は作業していない状態。
	Idle SessionState = iota
	// Thinking は推論中の状態。
	Thinking
	// ToolUse はツール実行中の状態。
	ToolUse
	// Error はエラー状態。
	Error
)

// String は SessionState の文字列表現を返す。
func (s SessionState) String() string {
	switch s {
	case Idle:
		return "idle"
	case Thinking:
		return "thinking"
	case ToolUse:
		return "tool_use"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// MarshalJSON は状態値を JSON 文字列として出力する。
func (s SessionState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// ContentBlock は message.content[] の1要素を表す。
type ContentBlock struct {
	Type string `json:"type"`
}

// Message は JSONL レコード内の message フィールドを表す。
type Message struct {
	Content []ContentBlock `json:"content"`
}

// Entry は JSONL ストリームの1レコードを表す。
type Entry struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Message   *Message        `json:"message,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// Session は監視対象となる1セッションの集約情報。
type Session struct {
	ID           string       `json:"id"`
	ProjectPath  string       `json:"project_path"`
	State        SessionState `json:"state"`
	LastActivity time.Time    `json:"last_activity"`
	PaneID       string       `json:"pane_id,omitempty"`
	FilePath     string       `json:"-"`
}

// Project はプロジェクト単位にまとめたセッション情報。
type Project struct {
	Path        string     `json:"path"`
	DisplayName string     `json:"display_name"`
	Sessions    []*Session `json:"sessions"`
	ActiveCount int        `json:"active_count"`
}

// StatusOutput は外部出力向けのステータスペイロード。
type StatusOutput struct {
	Projects  []Project `json:"projects"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WatchEventType はファイル監視イベント種別を表す。
type WatchEventType int

const (
	// Created は新規セッションファイル作成を表す。
	Created WatchEventType = iota
	// Modified はセッションファイル更新を表す。
	Modified
	// Removed はセッションファイル削除を表す。
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

// StateReader は集約済み状態への読み取り専用アクセスを定義する。
type StateReader interface {
	GetProjects() []Project
	GetStatus() StatusOutput
}

// StateWriter は集約済み状態への更新操作を定義する。
type StateWriter interface {
	HandleEvent(event WatchEvent) error
}

// EventSource は監視イベントの読み取り専用チャネルを提供する。
type EventSource interface {
	Events() <-chan WatchEvent
}
