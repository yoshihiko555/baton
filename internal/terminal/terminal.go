package terminal

import "errors"

// Terminal は各ターミナル実装で共通利用するインターフェースを定義する。
type Terminal interface {
	// ListPanes はターミナル上の全ペイン情報を返す。
	ListPanes() ([]Pane, error)
	// FocusPane は指定 paneID のペインをアクティブにする。
	FocusPane(paneID int) error
	// GetPaneText は指定ペインの画面テキスト末尾を返す。
	GetPaneText(paneID int) (string, error)
	// IsAvailable はターミナルが利用可能かを返す。
	IsAvailable() bool
	// Name はターミナル識別子（例: "wezterm"）を返す。
	Name() string
}

// Pane はターミナルのペイン（タブ情報含む）を表す内部型。
type Pane struct {
	ID         int    // WezTerm CLI が返す数値 pane_id
	Title      string // ペインタイトル
	TabID      int    // WezTerm CLI が返す数値 tab_id
	WorkingDir string // 正規化済み CWD (file:// プレフィックスなし)
	TTYName    string // TTY デバイス名 (例: /dev/ttys003)
	IsActive   bool   // そのペインがフォーカス中か
	Workspace  string // WezTerm ワークスペース名
}

// Terminal 実装が返す代表的なエラー。
var (
	ErrTerminalNotFound = errors.New("terminal not found")
	ErrPaneNotFound     = errors.New("pane not found")
)
