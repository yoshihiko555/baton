package terminal

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// hookSessionPattern は ai-orchestra の hook セッション名パターン。
// "claude-<name>-<digits>" 形式のセッションを除外する。
var hookSessionPattern = regexp.MustCompile(`^claude-.*-\d{4,}$`)

// TmuxTerminal は tmux 向けの Terminal 実装。
type TmuxTerminal struct {
	execFn func(args ...string) ([]byte, error)
}

// NewTmuxTerminal は tmux CLI を利用する実装を生成する。
func NewTmuxTerminal() *TmuxTerminal {
	return &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return exec.Command("tmux", args...).Output()
		},
	}
}

// ListPanes は tmux から全ペイン情報を取得する。
// hook セッション（claude-*-<digits>）は除外する。
func (t *TmuxTerminal) ListPanes() ([]Pane, error) {
	if t == nil || t.execFn == nil {
		return nil, fmt.Errorf("tmux exec function is not configured")
	}

	format := strings.Join([]string{
		"#{session_name}",
		"#{session_attached}",
		"#{window_index}",
		"#{pane_index}",
		"#{pane_id}",
		"#{pane_title}",
		"#{pane_current_command}",
		"#{pane_current_path}",
		"#{pane_tty}",
	}, "\t")

	out, err := t.execFn("list-panes", "-a", "-F", format)
	if err != nil {
		return nil, mapTmuxExecError(err)
	}

	var panes []Pane
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 9 {
			continue
		}

		sessionName := fields[0]
		sessionAttached := fields[1] == "1"
		windowIndex, _ := strconv.Atoi(fields[2])
		paneIndex, _ := strconv.Atoi(fields[3])
		paneID := fields[4] // "%5" 形式
		title := fields[5]
		currentCommand := fields[6]
		currentPath := fields[7]
		ttyName := fields[8]

		// hook セッション除外: unattached かつパターンマッチ
		if !sessionAttached && hookSessionPattern.MatchString(sessionName) {
			continue
		}

		panes = append(panes, Pane{
			ID:              paneID,
			Title:           title,
			WorkingDir:      currentPath,
			TTYName:         ttyName,
			IsActive:        false, // tmux では pane_active で判定可能だが現時点では不要
			CurrentCommand:  currentCommand,
			SessionName:     sessionName,
			SessionAttached: sessionAttached,
			WindowIndex:     windowIndex,
			PaneIndex:       paneIndex,
		})
	}

	return panes, nil
}

// FocusPane は指定ペインをアクティブ化する。
// tmux は同期的に切り替えるため WezTerm のような sleep は不要。
func (t *TmuxTerminal) FocusPane(paneID string) error {
	if t == nil || t.execFn == nil {
		return fmt.Errorf("tmux exec function is not configured")
	}

	// 対象ペインの情報を取得してセッション・ウィンドウを特定する
	panes, err := t.ListPanes()
	if err != nil {
		return err
	}

	var target *Pane
	for i := range panes {
		if panes[i].ID == paneID {
			target = &panes[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: pane %s", ErrPaneNotFound, paneID)
	}

	// セッション切り替え
	windowTarget := fmt.Sprintf("%s:%d", target.SessionName, target.WindowIndex)
	if _, err := t.execFn("switch-client", "-t", target.SessionName); err != nil {
		return fmt.Errorf("switch-client: %w", err)
	}

	// ウィンドウ選択
	if _, err := t.execFn("select-window", "-t", windowTarget); err != nil {
		return fmt.Errorf("select-window: %w", err)
	}

	// ペイン選択
	if _, err := t.execFn("select-pane", "-t", paneID); err != nil {
		return fmt.Errorf("select-pane: %w", err)
	}

	return nil
}

// GetPaneText は指定ペインの画面テキスト末尾を返す。
func (t *TmuxTerminal) GetPaneText(paneID string) (string, error) {
	if t == nil || t.execFn == nil {
		return "", fmt.Errorf("tmux exec function is not configured")
	}

	out, err := t.execFn("capture-pane", "-t", paneID, "-p", "-J")
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}

	// 末尾80行を返す（承認プロンプト検出用）
	lines := strings.Split(string(out), "\n")
	start := max(0, len(lines)-80)
	return strings.Join(lines[start:], "\n"), nil
}

// SendKeys は指定ペインにキーシーケンスを送信する。
func (t *TmuxTerminal) SendKeys(paneID string, keys ...string) error {
	if t == nil || t.execFn == nil {
		return fmt.Errorf("tmux exec function is not configured")
	}

	args := []string{"send-keys", "-t", paneID}
	args = append(args, keys...)
	if _, err := t.execFn(args...); err != nil {
		return fmt.Errorf("send-keys: %w", err)
	}
	return nil
}

// IsAvailable は tmux CLI が利用可能かを返す。
func (t *TmuxTerminal) IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// Name は terminal 識別子を返す。
func (t *TmuxTerminal) Name() string {
	return "tmux"
}

func mapTmuxExecError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tmux: %w", err)
}
