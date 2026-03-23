package terminal

import "errors"

// Terminal は各ターミナル実装で共通利用するインターフェースを定義する。
type Terminal interface {
	// ListPanes はターミナル上の全ペイン情報を返す。
	ListPanes() ([]Pane, error)
	// FocusPane は指定 paneID のペインをアクティブにする。
	FocusPane(paneID string) error
	// GetPaneText は指定ペインの画面テキスト末尾を返す。
	GetPaneText(paneID string) (string, error)
	// SendKeys は指定ペインにキーシーケンスを送信する。
	SendKeys(paneID string, keys ...string) error
	// IsAvailable はターミナルが利用可能かを返す。
	IsAvailable() bool
	// Name はターミナル識別子（例: "wezterm", "tmux"）を返す。
	Name() string
}

// Pane はターミナルのペイン情報を表す内部型。
type Pane struct {
	ID             string // tmux: "%5", wezterm: "42"(stringified)
	Title          string // ペインタイトル
	WorkingDir     string // 正規化済み CWD (file:// プレフィックスなし)
	TTYName        string // TTY デバイス名 (例: /dev/ttys003)
	IsActive       bool   // そのペインがフォーカス中か
	CurrentCommand string // tmux: pane_current_command

	// tmux 固有フィールド
	SessionName     string // tmux セッション名
	SessionAttached bool   // tmux セッションがアタッチされているか
	WindowIndex     int    // tmux ウィンドウインデックス
	PaneIndex       int    // tmux ペインインデックス
}

// Terminal 実装が返す代表的なエラー。
var (
	ErrTerminalNotFound = errors.New("terminal not found")
	ErrPaneNotFound     = errors.New("pane not found")
)
