package terminal

import "errors"

// Terminal は各ターミナル実装で共通利用するインターフェースを定義する。
type Terminal interface {
	// ListPanes はターミナル上の全ペイン情報を返す。
	ListPanes() ([]Pane, error)
	// FocusPane は指定 paneID のペインをアクティブにする。
	FocusPane(paneID string) error
	// IsAvailable はターミナルが利用可能かを返す。
	IsAvailable() bool
	// Name はターミナル識別子（例: "wezterm"）を返す。
	Name() string
}

// Pane はターミナルのペイン（タブ情報含む）を表す。
type Pane struct {
	ID         string `json:"pane_id"`
	Title      string `json:"title"`
	TabID      string `json:"tab_id"`
	WorkingDir string `json:"cwd"`
}

// Terminal 実装が返す代表的なエラー。
var (
	ErrTerminalNotFound = errors.New("terminal not found")
	ErrPaneNotFound     = errors.New("pane not found")
)
