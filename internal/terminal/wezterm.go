package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
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
		// pane_id / tab_id は環境により数値または文字列になるため RawMessage で受ける。
		ID         json.RawMessage `json:"pane_id"`
		Title      string          `json:"title"`
		TabID      json.RawMessage `json:"tab_id"`
		WorkingDir string          `json:"cwd"`
	}
	if err := json.Unmarshal(out, &rawPanes); err != nil {
		return nil, err
	}

	panes := make([]Pane, 0, len(rawPanes))
	for _, rawPane := range rawPanes {
		paneID, err := jsonValueToString(rawPane.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid pane_id: %w", err)
		}

		tabID, err := jsonValueToString(rawPane.TabID)
		if err != nil {
			return nil, fmt.Errorf("invalid tab_id: %w", err)
		}

		panes = append(panes, Pane{
			ID:         paneID,
			Title:      rawPane.Title,
			TabID:      tabID,
			WorkingDir: rawPane.WorkingDir,
		})
	}

	return panes, nil
}

// FocusPane は指定ペインをアクティブ化する。
func (w *WezTerminal) FocusPane(paneID string) error {
	if w == nil || w.execFn == nil {
		return fmt.Errorf("wezterm exec function is not configured")
	}

	_, err := w.execFn("cli", "activate-pane", "--pane-id", paneID)
	if err != nil {
		return mapWeztermExecError(err)
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

func mapWeztermExecError(err error) error {
	// 実行ファイル未検出は呼び出し側で扱いやすい共通エラーへ変換する。
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrTerminalNotFound, err)
	}

	return err
}

func jsonValueToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("empty value")
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		// 数値 ID は内部表現をそろえるため文字列へ正規化する。
		return strconv.FormatInt(i, 10), nil
	}

	return "", fmt.Errorf("unsupported value: %s", string(raw))
}
