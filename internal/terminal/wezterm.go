package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// WezTerminal は wezterm 向けの Terminal 実装。
type WezTerminal struct {
	execFn func(args ...string) ([]byte, error)
}

// NewWezTerminal は wezterm CLI を利用する実装を生成する。
func NewWezTerminal() *WezTerminal {
	return &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return exec.Command("wezterm", args...).Output()
		},
	}
}

// ListPanes は wezterm CLI からペイン一覧を取得する。
func (w *WezTerminal) ListPanes() ([]Pane, error) {
	if w == nil || w.execFn == nil {
		return nil, fmt.Errorf("wezterm exec function is not configured")
	}

	out, err := w.execFn("cli", "list", "--format", "json")
	if err != nil {
		return nil, mapWeztermExecError(err)
	}

	var rawPanes []struct {
		ID         int    `json:"pane_id"`
		Title      string `json:"title"`
		TabID      int    `json:"tab_id"`
		WorkingDir string `json:"cwd"`
		TTYName    string `json:"tty_name"`
		IsActive   bool   `json:"is_active"`
		Workspace  string `json:"workspace"`
	}
	if err := json.Unmarshal(out, &rawPanes); err != nil {
		return nil, err
	}

	panes := make([]Pane, 0, len(rawPanes))
	for _, rawPane := range rawPanes {
		panes = append(panes, Pane{
			ID:          strconv.Itoa(rawPane.ID),
			Title:       rawPane.Title,
			WorkingDir:  normalizeCWD(rawPane.WorkingDir),
			TTYName:     rawPane.TTYName,
			IsActive:    rawPane.IsActive,
			SessionName: rawPane.Workspace,
		})
	}

	return panes, nil
}

// wsTriggerPath は WezTerm Lua のワークスペース切り替えトリガーファイルのパス。
// WezTerm の actions.lua (setup_alfred_watcher) が update-status で検知し、
// SwitchToWorkspace を実行する。
const wsTriggerPath = "/tmp/wezterm-alfred-workspace.json"

// FocusPane は指定ペインをアクティブ化する。
// 別ワークスペースのペインの場合は、トリガーファイル経由で WezTerm Lua に
// ワークスペース切り替えを依頼してから activate-pane を実行する。
func (w *WezTerminal) FocusPane(paneID string) error {
	if w == nil || w.execFn == nil {
		return fmt.Errorf("wezterm exec function is not configured")
	}

	// 対象ペインの情報を取得する
	panes, err := w.ListPanes()
	if err != nil {
		return err
	}

	currentWS := ""
	targetWS := ""
	myPaneID := os.Getenv("WEZTERM_PANE")
	for _, p := range panes {
		if p.ID == myPaneID {
			currentWS = p.SessionName
		}
		if p.ID == paneID {
			targetWS = p.SessionName
		}
	}

	// 別ワークスペースの場合は Alfred watcher のトリガーファイルで WS を切り替え、
	// 切り替え完了後に activate-pane でペインにフォーカスする。
	if targetWS != "" && currentWS != "" && targetWS != currentWS {
		targetCWD := ""
		for _, p := range panes {
			if p.ID == paneID {
				targetCWD = p.WorkingDir
				break
			}
		}
		// Alfred watcher 互換のトリガーファイルでワークスペースを切り替える
		trigger := fmt.Sprintf(`{"name":%q,"cwd":%q,"timestamp":%d}`, targetWS, targetCWD, time.Now().Unix())
		if writeErr := os.WriteFile(wsTriggerPath, []byte(trigger), 0644); writeErr != nil {
			return fmt.Errorf("write ws trigger: %w", writeErr)
		}
		// Lua が検知して WS 切り替えを完了するまで待機し、その後ペインにフォーカスする
		time.Sleep(2 * time.Second)
		_, _ = w.execFn("cli", "activate-pane", "--pane-id", paneID)
		return nil
	}

	// 同一ワークスペースの場合は直接 activate-pane
	_, err = w.execFn("cli", "activate-pane", "--pane-id", paneID)
	if err != nil {
		return mapWeztermExecError(err)
	}

	return nil
}

// GetPaneText は指定ペインの画面テキスト末尾を返す。
func (w *WezTerminal) GetPaneText(paneID string) (string, error) {
	if w == nil || w.execFn == nil {
		return "", fmt.Errorf("wezterm exec function is not configured")
	}

	out, err := w.execFn("cli", "get-text", "--pane-id", paneID)
	if err != nil {
		return "", mapWeztermExecError(err)
	}

	// 末尾30行程度を返す（承認プロンプトの検出に十分）
	lines := strings.Split(string(out), "\n")
	start := len(lines) - 30
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:], "\n"), nil
}

// SendKeys は指定ペインにテキストを送信する。
func (w *WezTerminal) SendKeys(paneID string, keys ...string) error {
	if w == nil || w.execFn == nil {
		return fmt.Errorf("wezterm exec function is not configured")
	}

	text := strings.Join(keys, "")
	_, err := w.execFn("cli", "send-text", "--pane-id", paneID, "--no-paste", text)
	if err != nil {
		return fmt.Errorf("send-text: %w", err)
	}
	return nil
}

// IsAvailable は PATH 上に wezterm 実行ファイルがあるかを返す。
func (w *WezTerminal) IsAvailable() bool {
	_, err := exec.LookPath("wezterm")
	return err == nil
}

// Name は terminal 識別子を返す。
func (w *WezTerminal) Name() string {
	return "wezterm"
}

// normalizeCWD は file:// URI を絶対パスに正規化し、末尾スラッシュを除去する。
func normalizeCWD(cwd string) string {
	switch {
	case strings.HasPrefix(cwd, "file://localhost/"):
		cwd = cwd[len("file://localhost"):]
	case strings.HasPrefix(cwd, "file://"):
		cwd = cwd[len("file://"):]
	}
	if len(cwd) > 1 {
		cwd = strings.TrimRight(cwd, "/")
	}
	return cwd
}

func mapWeztermExecError(err error) error {
	// 実行ファイル未検出は呼び出し側で扱いやすい共通エラーへ変換する。
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrTerminalNotFound, err)
	}

	return err
}
