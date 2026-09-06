package core

import (
	"errors"
	"fmt"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// ApprovalAction は承認プロンプトへの応答種別を表す。
type ApprovalAction int

const (
	// ApprovalApprove は承認（Enter 送信）を表す。
	ApprovalApprove ApprovalAction = iota
	// ApprovalDeny は拒否（Escape 送信）を表す。
	ApprovalDeny
)

// Key は ApprovalAction に対応する送信キーシーケンスを返す。
func (a ApprovalAction) Key() string {
	if a == ApprovalDeny {
		return "Escape"
	}
	return "Enter"
}

// String は ApprovalAction の文字列表現を返す（ログ・エラーメッセージ用）。
func (a ApprovalAction) String() string {
	if a == ApprovalDeny {
		return "deny"
	}
	return "approve"
}

// ErrNotApprovable は CanRespondToApproval(s) が false のセッションに対して
// SendApproval が呼ばれた場合に返される sentinel error。
var ErrNotApprovable = errors.New("session is not in an approvable state")

// CanRespondToApproval はセッション s が承認/拒否の送信対象になり得るかを返す。
// 条件: Waiting 状態、Claude Code または Codex セッション、PaneID が確定済み（非空かつ非 Ambiguous）。
//
// TUI の canApprove() と同じ条件に加え !Ambiguous を要求する。現状 Ambiguous は
// どの本番コードパスでも true に設定されないため、この追加は現時点では無挙動
// （no-op）だが、将来 Ambiguous 解決が実装された際に TUI・CLI 双方が一貫して
// 曖昧なセッションへの応答を拒否できるようにするための予防的措置。
func CanRespondToApproval(s Session) bool {
	if s.State != Waiting {
		return false
	}
	if s.Tool != ToolClaude && s.Tool != ToolCodex {
		return false
	}
	if s.PaneID == "" {
		return false
	}
	if s.Ambiguous {
		return false
	}
	return true
}

// SendApproval はゲート（CanRespondToApproval）を検証し、通過すれば term.SendKeys で
// s.PaneID に action のキーを送信する。ゲート不通過時は ErrNotApprovable をラップして返す。
func SendApproval(term terminal.Terminal, s Session, action ApprovalAction) error {
	if !CanRespondToApproval(s) {
		return fmt.Errorf(
			"%w: state=%s tool=%s pane_id=%q ambiguous=%v",
			ErrNotApprovable, s.State, s.Tool, s.PaneID, s.Ambiguous,
		)
	}
	return term.SendKeys(s.PaneID, action.Key())
}
