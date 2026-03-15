package core

import (
	"log"
	"path/filepath"
	"sort"
	"strconv"
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
func resolveProjectKey(proc DetectedProcess, paneWorkspaceMap map[int]string) projectKey {
	ws := paneWorkspaceMap[proc.PaneID]
	if ws != "" && ws != "default" {
		return projectKey{Workspace: ws}
	}
	return projectKey{CWD: proc.CWD}
}

// StateManager はスキャン結果をプロジェクト/セッション単位に集約するコンポーネント。
// v2 ではポーリング + スナップショット照合方式を採用し、Watcher への依存を排除した。
type StateManager struct {
	resolver   *StateResolver  // JSONL 解析・状態判定の委譲先
	projects   []Project       // 最新プロジェクト一覧スナップショット（ソート済み）
	summary    Summary         // 最新集計キャッシュ
	panes      []terminal.Pane // 最新ペイン一覧（Ambiguous セッション解決用）
	prevPIDSet map[int]bool    // 前回スキャンの PID セット（差分検出用）
	mu         sync.RWMutex   // 読み書き保護
}

// NewStateManager は StateManager を初期化して返す。
func NewStateManager(resolver *StateResolver) *StateManager {
	return &StateManager{
		resolver:   resolver,
		prevPIDSet: make(map[int]bool),
	}
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

	// Step 2: PaneID → Workspace マッピングを構築する
	paneWorkspaceMap := make(map[int]string, len(result.Panes))
	for _, pane := range result.Panes {
		paneWorkspaceMap[pane.ID] = pane.Workspace
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
// Codex/Gemini はプロセス存在＝Thinking として最小構成を返す。
func (s *StateManager) buildSessionFromStates(proc DetectedProcess, cwdStates map[string][]ResolvedSession, cwdStateIndex map[string]int) Session {
	sess := Session{
		PID:        proc.PID,
		Tool:       proc.ToolType,
		WorkingDir: proc.CWD,
		State:      Thinking,
		PaneID:     strconv.Itoa(proc.PaneID),
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
