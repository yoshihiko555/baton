package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
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
			ID:         rawPane.ID,
			Title:      rawPane.Title,
			TabID:      rawPane.TabID,
			WorkingDir: normalizeCWD(rawPane.WorkingDir),
			TTYName:    rawPane.TTYName,
			IsActive:   rawPane.IsActive,
			Workspace:  rawPane.Workspace,
		})
	}

	return panes, nil
}

// FocusPane は指定ペインをアクティブ化する。
func (w *WezTerminal) FocusPane(paneID int) error {
	if w == nil || w.execFn == nil {
		return fmt.Errorf("wezterm exec function is not configured")
	}

	_, err := w.execFn("cli", "activate-pane", "--pane-id", strconv.Itoa(paneID))
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
