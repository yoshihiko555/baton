package core

import (
	"context"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// statePriority は Projects のソート用状態優先度マップ。
// 値が小さいほど優先度が高い（先頭に表示される）。
var statePriority = map[SessionState]int{
	Waiting:  0,
	Error:    1,
	Thinking: 2,
	ToolUse:  3,
	Idle:     4,
}

// projectKey はプロセスのグルーピングキー。
// Workspace が設定されている場合はワークスペース優先、それ以外は CWD 使用。
type projectKey struct {
	Workspace string // 空の場合は CWD ベースでグルーピング
	CWD       string // Workspace が空の場合のフォールバック
}

// resolveProjectKey はプロセスとペインマップからプロジェクトキーを解決する。
// Workspace が空でなく "default" でもない場合はワークスペース優先でグルーピングする。
func resolveProjectKey(proc DetectedProcess, paneWorkspaceMap map[string]string) projectKey {
	ws := paneWorkspaceMap[proc.PaneID]
	if ws != "" && ws != "default" {
		return projectKey{Workspace: ws}
	}
	return projectKey{CWD: proc.CWD}
}

// StateManager はスキャン結果をプロジェクト/セッション単位に集約するコンポーネント。
// v2 ではポーリング + スナップショット照合方式を採用し、Watcher への依存を排除した。
type StateManager struct {
	resolver       *StateResolver  // JSONL 解析・状態判定の委譲先
	processScanner *ProcessScanner // Codex 子プロセス検査用
	projects       []Project       // 最新プロジェクト一覧スナップショット（ソート済み）
	summary        Summary         // 最新集計キャッシュ
	panes          []terminal.Pane // 最新ペイン一覧（Ambiguous セッション解決用）
	prevPIDSet     map[int]bool    // 前回スキャンの PID セット（差分検出用）
	mu             sync.RWMutex    // 読み書き保護
}

// NewStateManager は StateManager を初期化して返す。
func NewStateManager(resolver *StateResolver) *StateManager {
	return &StateManager{
		resolver:   resolver,
		prevPIDSet: make(map[int]bool),
	}
}

// SetProcessScanner は Codex 子プロセス検査用の ProcessScanner を設定する。
func (s *StateManager) SetProcessScanner(ps *ProcessScanner) {
	s.processScanner = ps
}

// UpdateFromScan はスキャン結果から状態を更新する（StateUpdater 実装）。
//
// 処理フロー:
//  1. ScanResult.Err != nil → 前回スナップショットを保持して return nil
//  2. Panes からワークスペースマップを構築
//  3. Processes をワークスペース優先でグルーピング
//  4. 各プロセスをセッションに変換（Claude は StateResolver 経由、Codex/Gemini は最小構成）
//  5. Summary 再計算 + panes/prevPIDSet を更新
func (s *StateManager) UpdateFromScan(result ScanResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Step 1: エラーチェック — 過渡的なエラーは前回スナップショットを維持する
	if result.Err != nil {
		return nil
	}

	// Step 2: PaneID → SessionName マッピングを構築する
	paneWorkspaceMap := make(map[string]string, len(result.Panes))
	for _, pane := range result.Panes {
		paneWorkspaceMap[pane.ID] = pane.SessionName
	}

	// Step 3 & 4: プロセスをグルーピングしてセッションに変換する
	type sessionEntry struct {
		key     projectKey
		session *Session
	}

	entries := make([]sessionEntry, 0, len(result.Processes))
	currentPIDSet := make(map[int]bool, len(result.Processes))

	// CWD ごとに Claude セッションをグループ化し、ResolveMultiple で状態分布を取得する。
	// PID との1対1対応はできないが、重要度順に状態を割り当てる。
	cwdClaudeProcs := make(map[string][]int) // CWD → プロセスインデックス
	for i, proc := range result.Processes {
		currentPIDSet[proc.PID] = true
		if proc.ToolType == ToolClaude {
			cwdClaudeProcs[proc.CWD] = append(cwdClaudeProcs[proc.CWD], i)
		}
	}

	// CWD ごとに状態分布を解決する
	cwdStates := make(map[string][]ResolvedSession)
	if s.resolver != nil {
		for cwd, indices := range cwdClaudeProcs {
			states, err := s.resolver.ResolveMultiple(cwd, len(indices))
			if err != nil {
				log.Printf("ResolveMultiple error for CWD %s: %v", cwd, err)
				continue
			}
			cwdStates[cwd] = states
		}
	}

	// 各プロセスをセッションに変換する
	cwdStateIndex := make(map[string]int) // CWD ごとの割り当てカウンタ
	for _, proc := range result.Processes {
		key := resolveProjectKey(proc, paneWorkspaceMap)
		sess := s.buildSessionFromStates(proc, cwdStates, cwdStateIndex)
		entries = append(entries, sessionEntry{key: key, session: &sess})
	}

	// Step 5: グルーピング結果からプロジェクト一覧を構築する
	projectMap := make(map[projectKey][]*Session)
	for _, e := range entries {
		projectMap[e.key] = append(projectMap[e.key], e.session)
	}

	projects := make([]Project, 0, len(projectMap))
	for key, sessions := range projectMap {
		// セッションをソートする（状態優先度順 → LastActivity 降順）
		sortSessionPtrs(sessions)

		proj := Project{
			Sessions: sessions,
		}
		if key.Workspace != "" {
			ws := strings.TrimSpace(key.Workspace)
			proj.Name = ws
			proj.Workspace = ws
			proj.Path = ws
		} else {
			proj.Name = filepath.Base(key.CWD)
			proj.Path = key.CWD
		}
		projects = append(projects, proj)
	}

	// プロジェクト一覧をソートする。
	// Waiting/Error を持つプロジェクトを上に浮かせ、それ以外はプロジェクト名昇順で安定化。
	sort.Slice(projects, func(i, j int) bool {
		pi := projectNeedsAttention(projects[i])
		pj := projectNeedsAttention(projects[j])
		if pi != pj {
			return pi
		}
		return projects[i].Name < projects[j].Name
	})

	// Step 6: Summary 再計算 + キャッシュ更新
	s.projects = projects
	s.summary = calcSummary(projects)
	s.panes = result.Panes
	s.prevPIDSet = currentPIDSet

	return nil
}

// buildSessionFromStates はプロセス情報と事前解決済みの状態分布からセッションを構築する。
// Claude セッションは cwdStates から重要度順に状態を割り当てる。
// Codex はプロセスツリー検査で Working(Thinking)/Idle を判定する。
// Gemini はプロセス存在＝Thinking として最小構成を返す（承認待ちは RefineToolUseState で検出）。
func (s *StateManager) buildSessionFromStates(proc DetectedProcess, cwdStates map[string][]ResolvedSession, cwdStateIndex map[string]int) Session {
	sess := Session{
		PID:        proc.PID,
		Tool:       proc.ToolType,
		WorkingDir: proc.CWD,
		State:      Thinking,
		PaneID:     proc.PaneID,
	}

	if proc.ToolType == ToolCodex && s.processScanner != nil {
		hasChildren, err := s.processScanner.HasChildProcesses(context.Background(), proc.PID)
		if err == nil && !hasChildren {
			sess.State = Idle
		}
		return sess
	}

	if proc.ToolType != ToolClaude {
		return sess
	}

	states := cwdStates[proc.CWD]
	idx := cwdStateIndex[proc.CWD]
	if idx < len(states) {
		resolved := states[idx]
		sess.State = resolved.State
		sess.Branch = resolved.Branch
		sess.CurrentTool = resolved.CurrentTool
		sess.FirstPrompt = resolved.FirstPrompt
		sess.InputTokens = resolved.InputTokens
		sess.OutputTokens = resolved.OutputTokens
		cwdStateIndex[proc.CWD] = idx + 1
	}

	return sess
}

// Projects は全プロジェクトのスナップショット（コピー）を返す（StateReader 実装）。
// ソート順: 状態優先度（Waiting > Error > Thinking > ToolUse > Idle）、同一状態内は LastActivity 降順。
func (s *StateManager) Projects() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.projects == nil {
		return []Project{}
	}

	copied := make([]Project, len(s.projects))
	for i, p := range s.projects {
		proj := p
		sessions := make([]*Session, len(p.Sessions))
		for j, sess := range p.Sessions {
			if sess == nil {
				continue
			}
			clone := *sess
			sessions[j] = &clone
		}
		proj.Sessions = sessions
		copied[i] = proj
	}
	return copied
}

// GetProjects は v1 互換メソッド。Projects() に委譲する（tui が参照。v2 完全移行後に削除予定）。
func (s *StateManager) GetProjects() []Project {
	return s.Projects()
}

// Summary はキャッシュ済み集計情報を返す（StateReader 実装）。
func (s *StateManager) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summary
}

// Panes はキャッシュ済みペイン一覧を返す（StateReader 実装）。
func (s *StateManager) Panes() []terminal.Pane {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.panes
}

// calcSummary は全プロジェクトのセッションを集計して Summary を返す。
func calcSummary(projects []Project) Summary {
	s := Summary{ByTool: make(map[string]int)}
	for _, p := range projects {
		for _, sess := range p.Sessions {
			if sess == nil {
				continue
			}
			s.TotalSessions++
			switch sess.State {
			case Thinking, ToolUse, Waiting:
				s.Active++
			}
			if sess.State == Waiting {
				s.Waiting++
			}
			s.ByTool[sess.Tool.String()]++
		}
	}
	return s
}

// sortSessionPtrs はポインタスライスを状態優先度順（昇順）→ LastActivity 降順にソートする。
func sortSessionPtrs(sessions []*Session) {
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i] == nil || sessions[j] == nil {
			return sessions[i] != nil
		}
		pi := statePriority[sessions[i].State]
		pj := statePriority[sessions[j].State]
		if pi != pj {
			return pi < pj
		}
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
}

// claudeApprovalPattern は Claude Code の ToolUse 承認プロンプトを検出する正規表現。
// 文言ベース: "Allow <tool>?", "Do you want to ...", "(y/n)" 等
var claudeApprovalPattern = regexp.MustCompile(
	`(?i)(?:allow\s+\S+.*\?\s*\(?y|do you want to \w+|(?:^|\s)allow (?:once|always)|yes[,.]?\s*allow|\(y/n\)|\[y/n\]|\[n/y\]|\byes/no\b)`,
)

// codexApprovalPattern は Codex CLI の承認プロンプトの構造を検出する正規表現。
// 番号付き選択肢（"1. Yes..." + "2. Yes..." or "2. No..."）の連続で判定する。
// 単独の "1. Yes" ではなく後続行も確認することで誤検知を防ぐ。
var codexApprovalPattern = regexp.MustCompile(`(?m)^\s*[›>]?\s*1\.\s+Yes.*\n\s*[›>]?\s*2\.\s+`)

// geminiIdlePattern は Gemini CLI の入力待ちプロンプトを検出する正規表現。
// Gemini はアイドル時にステータスバーに "workspace" と "sandbox" を表示する。
// このパターンは Gemini 固有の UI 要素であり、他のツールとの誤検知リスクが低い。
var geminiIdlePattern = regexp.MustCompile(`(?m)workspace\s+\(.+\)\s+.*sandbox`)

// RefineToolUseState はペインテキストから状態を精緻化する。
// - Claude: 全状態でペインテキストをチェック。classifyClaudePane が権威的ソース。
//   判定できた場合はその状態を採用。判定不能かつ JSONL=Waiting → ToolUse に降格。
// - Codex Idle → Waiting（承認待ち検出）
// - Gemini Thinking → Waiting（承認待ち）または Idle（入力プロンプト検出）
func (s *StateManager) RefineToolUseState(term terminal.Terminal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if term == nil {
		return
	}

	paneTextCache := make(map[string]string)
	paneTextError := make(map[string]bool)
	getPaneText := func(paneID string) (string, bool) {
		if text, ok := paneTextCache[paneID]; ok {
			return text, true
		}
		if paneTextError[paneID] {
			return "", false
		}
		text, err := term.GetPaneText(paneID)
		if err != nil {
			paneTextError[paneID] = true
			return "", false
		}
		paneTextCache[paneID] = text
		return text, true
	}

	for i := range s.projects {
		for j, sess := range s.projects[i].Sessions {
			if sess == nil {
				continue
			}
			switch {
			case sess.Tool == ToolClaude && sess.PaneID != "":
				// Claude: 全状態でペインテキストをチェック
			case sess.State == Idle && sess.Tool == ToolCodex:
				// Codex: 子プロセスなし(Idle) → 承認待ちかもしれない
			case sess.State == Thinking && sess.Tool == ToolGemini:
				// Gemini: 子プロセス検査不可 → ペインテキストで状態判定
			default:
				continue
			}
			text, ok := getPaneText(sess.PaneID)
			if !ok {
				continue
			}

			if sess.Tool == ToolGemini {
				// Gemini: 承認待ち → Waiting、入力プロンプト → Idle、それ以外 → Thinking 維持
				if containsApprovalPrompt(text) {
					s.projects[i].Sessions[j].State = Waiting
				} else if geminiIdlePattern.MatchString(text) {
					s.projects[i].Sessions[j].State = Idle
				}
				continue
			}

			if sess.Tool == ToolClaude {
				if newState, ok := classifyClaudePane(text); ok {
					s.projects[i].Sessions[j].State = newState
				} else if sess.State == Waiting {
					// ペインテキストから判定不能だが JSONL が Waiting → ToolUse に降格
					s.projects[i].Sessions[j].State = ToolUse
				}
				continue
			}

			// Codex: 番号付き選択肢の構造パターンで検出
			if codexApprovalPattern.MatchString(text) {
				s.projects[i].Sessions[j].State = Waiting
			}
		}
	}

	// 状態が変更された可能性があるので Summary を再計算する
	s.summary = calcSummary(s.projects)
}

func containsApprovalPrompt(text string) bool {
	return claudeApprovalPattern.MatchString(text)
}

// allDash は s がすべて '─'（U+2500）文字で構成され、4文字以上であるか返す。
func allDash(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if r != '─' {
			return false
		}
	}
	return true
}

// classifyClaudePane はペインテキストから Claude Code の状態を判定する。
// 末尾から逆順スキャンし、入力プロンプト位置を基準に状態を決定する。
// 判定できない場合は ok=false を返し、呼び出し元は JSONL 状態を維持する。
func classifyClaudePane(text string) (state SessionState, ok bool) {
	lines := strings.Split(text, "\n")

	// Step 1: WAITING チェック（最優先）— テキスト全体を検索
	// 選択肢 UI: ❯ + 1. + Yes を含む行
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "❯") && strings.Contains(trimmed, "1.") && strings.Contains(trimmed, "Yes") {
			return Waiting, true
		}
	}
	// 承認プロンプト: claudeApprovalPattern にマッチ
	if containsApprovalPrompt(text) {
		return Waiting, true
	}

	// Step 2: 入力プロンプト行を末尾から逆順スキャンして探す
	// 入力プロンプト行 = ❯ を含む行で、直前行が区切り線（─ が4文字以上連続）
	promptIdx := -1
	for i := len(lines) - 1; i >= 1; i-- {
		stripped := strings.TrimRight(lines[i], " \t\r\n\u00a0")
		if strings.Contains(stripped, "❯") {
			prevStripped := strings.TrimSpace(lines[i-1])
			if allDash(prevStripped) {
				promptIdx = i
				break
			}
		}
	}

	// Step 3: 入力プロンプト行が見つからない → 判定不能
	if promptIdx < 0 {
		return 0, false
	}

	// Step 4: WORKING チェック — 入力プロンプト行より上（最大20行）を検索
	start := promptIdx - 20
	if start < 0 {
		start = 0
	}
	for i := start; i < promptIdx; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "· ") || strings.HasPrefix(trimmed, "✢ ") || strings.HasPrefix(trimmed, "✶ ") || strings.Contains(trimmed, "Running…") {
			return Thinking, true
		}
	}

	// Step 5: Working シグナルなし + 入力プロンプト行あり → Idle
	return Idle, true
}

// projectNeedsAttention はプロジェクト内に Waiting または Error のセッションがあるか返す。
func projectNeedsAttention(p Project) bool {
	for _, sess := range p.Sessions {
		if sess == nil {
			continue
		}
		if sess.State == Waiting || sess.State == Error {
			return true
		}
	}
	return false
}
