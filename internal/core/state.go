package core

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yoshihiko555/baton/internal/hook"
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

// taktTerminalSessionPrefix は takt の claude-terminal provider が開く tmux セッション名の prefix
// （"takt-claude-terminal-<uuid>"）。このセッション名の pane 上の Claude セッションは本物の TUI
// （対話可能）なので、Via のみラベル付けし CWD 束ね・pane 精緻化は従来通り動かす（ADR-0016 Decision 3）。
const taktTerminalSessionPrefix = "takt-claude-terminal-"

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
	resolver                *StateResolver  // JSONL 解析・状態判定の委譲先
	processScanner          *ProcessScanner // Codex 子プロセス検査用
	hookStore               *hook.Store     // Claude Code hooks 由来の Waiting 状態ストア（nil なら hook 連携なし）
	hookStatusOverlayPath   string
	hookStatusOverlayMaxAge time.Duration
	scanOverlayStatus       StatusOutput
	scanOverlayValid        bool
	projects                []Project         // 最新プロジェクト一覧スナップショット（ソート済み）
	summary                 Summary           // 最新集計キャッシュ
	panes                   []terminal.Pane   // 最新ペイン一覧（Ambiguous セッション解決用）
	prevPIDSet              map[int]bool      // 前回スキャンの PID セット（差分検出用）
	lastDiagKey             map[string]string // ペインごとの直近診断キー（重複ログ抑制用）
	mu                      sync.RWMutex      // 読み書き保護
}

// NewStateManager は StateManager を初期化して返す。
func NewStateManager(resolver *StateResolver) *StateManager {
	return &StateManager{
		resolver:    resolver,
		prevPIDSet:  make(map[int]bool),
		lastDiagKey: make(map[string]string),
	}
}

// SetProcessScanner は Codex 子プロセス検査用の ProcessScanner を設定する。
func (s *StateManager) SetProcessScanner(ps *ProcessScanner) {
	s.processScanner = ps
}

// SetHookStore は Claude Code hooks 由来の状態ストアを設定する。
// nil のままなら ApplyHookStates は Claude セッションの StateSource を "jsonl" にするだけの no-op になる
// （--once / --exit 起動、または hook socket の listen に失敗した場合）。
func (s *StateManager) SetHookStore(store *hook.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hookStore = store
}

// SetHookStatusOverlay configures StateManager to read a status JSON file written by a
// resident baton instance and overlay hook-derived Waiting state onto local Claude sessions
// when this instance itself has no hookStore (i.e. is not the hook socket listener).
// path == "" disables the overlay (no-op).
func (s *StateManager) SetHookStatusOverlay(path string, maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hookStatusOverlayPath = path
	s.hookStatusOverlayMaxAge = maxAge
}

// UpdateFromScan はスキャン結果から状態を更新する（StateUpdater 実装）。
//
// 処理フロー:
//  1. ScanResult.Err != nil → 前回スナップショットを保持して return nil
//  2. Panes からワークスペースマップを構築
//  3. Processes をワークスペース優先でグルーピング
//  4. 各プロセスをセッションに変換（Claude は StateResolver 経由、その他のツールは最小構成）
//  5. Summary 再計算 + panes/prevPIDSet を更新
func (s *StateManager) UpdateFromScan(result ScanResult) error {
	s.mu.RLock()
	useOverlay := s.hookStore == nil
	overlayPath := s.hookStatusOverlayPath
	overlayMaxAge := s.hookStatusOverlayMaxAge
	s.mu.RUnlock()

	var overlayStatus StatusOutput
	haveOverlay := false
	if useOverlay {
		overlayStatus, haveOverlay = loadHookStatusOverlay(overlayPath, overlayMaxAge)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// スキャン結果と独立した overlay は、過渡的なエラーでも最新の鮮度判定を失わないよう早期 return 前にキャッシュする。
	if s.hookStore == nil {
		s.scanOverlayStatus = overlayStatus
		s.scanOverlayValid = haveOverlay
	} else {
		overlayStatus = StatusOutput{}
		haveOverlay = false
		s.scanOverlayStatus = StatusOutput{}
		s.scanOverlayValid = false
	}

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

	// CWD ごとに Claude セッションをグループ化し、ResolveMultipleExcluding で状態分布を取得する。
	// PID との1対1対応はできないが、重要度順に状態を割り当てる。
	overlayByPane := make(map[string]SessionOutput)
	if haveOverlay {
		for _, project := range overlayStatus.Projects {
			for _, session := range project.Sessions {
				if session.PaneID == "" {
					continue
				}
				overlayByPane[session.PaneID] = session
			}
		}
	}

	cwdClaudeProcs := make(map[string][]int)        // CWD → プロセスインデックス
	cwdExclude := make(map[string]map[string]bool)  // CWD → ピン留め済み transcript パス
	pinnedResolved := make(map[int]ResolvedSession) // プロセスインデックス → 解決済み状態
	pinnedTranscript := make(map[int]string)        // プロセスインデックス → transcript パス
	pinnedSessionID := make(map[int]string)         // プロセスインデックス → Claude session ID
	for i, proc := range result.Processes {
		currentPIDSet[proc.PID] = true
		if proc.ToolType != ToolClaude {
			continue
		}
		if proc.Via == ViaTakt {
			// takt が stdio=pipe で起動した非対話セッションは CWD 束ねに参加させない
			// （同一 CWD の対話 Claude セッションのスロットを消費してしまうため。ADR-0016 Decision 3）。
			continue
		}

		var pinPath, pinSessionID string
		if s.hookStore != nil {
			if hs, ok := s.hookStore.Get(proc.PaneID); ok {
				pinPath = hs.TranscriptPath
				pinSessionID = hs.SessionID
			}
		} else if haveOverlay {
			if so, ok := overlayByPane[proc.PaneID]; ok {
				pinPath = so.TranscriptPath
				pinSessionID = so.SessionID
			}
		}

		if pinPath != "" && s.resolver != nil {
			resolved, normalizedPath, err := s.resolver.ResolvePath(pinPath)
			if err == nil {
				pinnedResolved[i] = resolved
				pinnedTranscript[i] = pinPath
				pinnedSessionID[i] = pinSessionID
				if cwdExclude[proc.CWD] == nil {
					cwdExclude[proc.CWD] = make(map[string]bool)
				}
				cwdExclude[proc.CWD][normalizedPath] = true
				continue
			}
		}

		cwdClaudeProcs[proc.CWD] = append(cwdClaudeProcs[proc.CWD], i)
	}

	// CWD ごとに状態分布を解決する
	cwdStates := make(map[string][]ResolvedSession)
	if s.resolver != nil {
		for cwd, indices := range cwdClaudeProcs {
			states, err := s.resolver.ResolveMultipleExcluding(cwd, len(indices), cwdExclude[cwd])
			if err != nil {
				log.Printf("ResolveMultiple error for CWD %s: %v", cwd, err)
				continue
			}
			cwdStates[cwd] = states
		}
	}

	// 各プロセスをセッションに変換する
	cwdStateIndex := make(map[string]int) // CWD ごとの割り当てカウンタ
	for i, proc := range result.Processes {
		key := resolveProjectKey(proc, paneWorkspaceMap)
		var sess Session
		if resolved, ok := pinnedResolved[i]; ok {
			sess = s.buildSessionFromPinned(proc, resolved, pinnedTranscript[i], pinnedSessionID[i])
		} else {
			sess = s.buildSessionFromStates(proc, cwdStates, cwdStateIndex)
		}
		// takt の claude-terminal provider は別 tmux セッション "takt-claude-terminal-<uuid>" に
		// 本物の TUI を開く（ADR-0016 Decision 3）。こちらは pane 判定・CWD 束ねとも従来通り動かし、
		// ラベル表示用に Via のみ付与する（isTaktPipe は立てない）。
		if sess.Tool == ToolClaude && strings.HasPrefix(paneWorkspaceMap[proc.PaneID], taktTerminalSessionPrefix) {
			sess.Via = ViaTakt
		}
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

// ApplyHookStates は Claude Code hooks 由来の Waiting 状態を Session に反映する。
// UpdateFromScan の直後、RefineToolUseState の前に呼び出すこと。
//
// 処理内容:
//  1. hookStore が設定されていれば、直近スキャンの pane ID 集合で RetainPanes を呼び、
//     tmux から消えた pane の hook 状態を掃除する
//  2. Tool == ToolClaude かつ PaneID != "" の Session について、対応する hook.State があれば
//     SessionID / TranscriptPath を転記し、Waiting なら State を Waiting に固定して
//     HookWaiting=true, StateSource=SourceHook とする
//  3. hook 由来の Waiting が確定しなかった Claude セッションは StateSource=SourceJSONL
//     （RefineToolUseState が pane 判定に成功すれば SourcePane に更新される）
//  4. Claude 以外のセッションの StateSource は変更しない（空のまま）
func (s *StateManager) ApplyHookStates() {
	s.mu.Lock()
	defer s.mu.Unlock()
	overlayStatus := s.scanOverlayStatus
	haveOverlay := s.scanOverlayValid

	if s.hookStore != nil {
		alive := make(map[string]bool, len(s.panes))
		for _, pane := range s.panes {
			alive[pane.ID] = true
		}
		s.hookStore.RetainPanes(alive)
	}

	for i := range s.projects {
		for _, sess := range s.projects[i].Sessions {
			if sess == nil || sess.Tool != ToolClaude || sess.PaneID == "" {
				continue
			}
			sess.StateSource = SourceJSONL
			if s.hookStore == nil {
				continue
			}
			state, ok := s.hookStore.Get(sess.PaneID)
			if !ok {
				continue
			}
			if state.SessionID != "" {
				sess.SessionID = state.SessionID
			}
			if state.TranscriptPath != "" {
				sess.TranscriptPath = state.TranscriptPath
			}
			if state.Waiting && !sess.isTaktPipe {
				// takt pipe 配下には hook Waiting を載せない。headless claude -p は TMUX_PANE を
				// takt の pane から継承するため hook が届き得るが、Waiting にすると承認操作や
				// 自動承認モードが takt の pane に Enter を送ってしまう（ADR-0016 Decision 3）。
				sess.State = Waiting
				sess.HookWaiting = true
				sess.StateSource = SourceHook
			}
		}
	}

	if s.hookStore == nil && haveOverlay {
		s.applyHookStatusOverlayLocked(overlayStatus)
	}

	s.summary = calcSummary(s.projects)
}

// loadHookStatusOverlay は status JSON を読み込み、overlay 元として有効な場合に返す。
func loadHookStatusOverlay(path string, maxAge time.Duration) (StatusOutput, bool) {
	if path == "" {
		return StatusOutput{}, false
	}
	status, err := ReadStatus(path)
	if err != nil {
		debugf("hook status overlay: read %q failed: %v", path, err)
		return StatusOutput{}, false
	}
	if !status.HookListener {
		debugf("hook status overlay: %q has hook_listener=false, skipping", path)
		return StatusOutput{}, false
	}
	if status.Version != 2 {
		debugf("hook status overlay: %q has unsupported version %d, skipping", path, status.Version)
		return StatusOutput{}, false
	}
	ts, err := time.Parse(time.RFC3339, status.Timestamp)
	if err != nil {
		debugf("hook status overlay: %q has unparsable timestamp %q: %v", path, status.Timestamp, err)
		return StatusOutput{}, false
	}
	age := time.Since(ts)
	if age < 0 {
		debugf("hook status overlay: %q has a future timestamp (age=%s), skipping", path, age)
		return StatusOutput{}, false
	}
	if maxAge > 0 && age > maxAge {
		debugf("hook status overlay: %q is stale (age=%s > max=%s), skipping", path, age, maxAge)
		return StatusOutput{}, false
	}

	return status, true
}

// applyHookStatusOverlayLocked は検証済みの status をセッションへ反映する。
// 呼び出し元が事前にファイル I/O と鮮度検証を完了し、s.mu を保持していること。
func (s *StateManager) applyHookStatusOverlayLocked(status StatusOutput) {
	byPane := make(map[string]SessionOutput)
	for _, p := range status.Projects {
		for _, so := range p.Sessions {
			if so.PaneID == "" {
				continue
			}
			byPane[so.PaneID] = so
		}
	}

	for i := range s.projects {
		for _, sess := range s.projects[i].Sessions {
			if sess == nil || sess.Tool != ToolClaude || sess.PaneID == "" {
				continue
			}
			so, ok := byPane[sess.PaneID]
			if !ok {
				continue
			}
			if so.SessionID != "" {
				sess.SessionID = so.SessionID
			}
			if so.TranscriptPath != "" {
				sess.TranscriptPath = so.TranscriptPath
			}
			if so.StateSource == SourceHook && !sess.isTaktPipe {
				// ApplyHookStates と同じ理由で takt pipe 配下には Waiting を載せない。
				sess.State = Waiting
				sess.HookWaiting = true
				sess.StateSource = SourceHook
			}
		}
	}
}

// buildSessionFromPinned は hook でピン留めされた JSONL の解決結果からセッションを構築する。
func (s *StateManager) buildSessionFromPinned(proc DetectedProcess, resolved ResolvedSession, transcriptPath, sessionID string) Session {
	return Session{
		PID:            proc.PID,
		Tool:           proc.ToolType,
		WorkingDir:     proc.CWD,
		PaneID:         proc.PaneID,
		State:          resolved.State,
		Branch:         resolved.Branch,
		CurrentTool:    resolved.CurrentTool,
		FirstPrompt:    resolved.FirstPrompt,
		InputTokens:    resolved.InputTokens,
		OutputTokens:   resolved.OutputTokens,
		TranscriptPath: transcriptPath,
		SessionID:      sessionID,
		Via:            proc.Via,
		isTaktPipe:     proc.Via == ViaTakt,
	}
}

// buildSessionFromStates はプロセス情報と事前解決済みの状態分布からセッションを構築する。
// Claude セッションは cwdStates から重要度順に状態を割り当てる。
// Codex はプロセスツリー検査で Working(Thinking)/Idle を判定する。
// Codex 以外の非 Claude ツール（agy 等）はプロセス存在＝Thinking として最小構成を返す（詳細な状態は RefineToolUseState のルールテーブルで判定する）。
func (s *StateManager) buildSessionFromStates(proc DetectedProcess, cwdStates map[string][]ResolvedSession, cwdStateIndex map[string]int) Session {
	sess := Session{
		PID:        proc.PID,
		Tool:       proc.ToolType,
		WorkingDir: proc.CWD,
		State:      Thinking,
		PaneID:     proc.PaneID,
		Via:        proc.Via,
		isTaktPipe: proc.Via == ViaTakt,
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

	if sess.isTaktPipe {
		// CWD 束ねから除外済み（cwdClaudeProcs に含まれない）のため cwdStates を消費しない。
		// State は既定の Thinking のまま返し、承認待ちの誤判定は RefineToolUseState 側で降格する。
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
// 番号付き選択肢（"1. Yes..." / "1. Allow..." + "2. ..."）の連続で判定する。
// 単独の "1. Yes" ではなく後続行も確認することで誤検知を防ぐ。
var codexApprovalPattern = regexp.MustCompile(`(?im)^\s*[›>❯]?\s*1\.\s+(?:yes|allow)\b.*\n\s*[›>❯]?\s*2\.\s+`)

// paneRules は子プロセスを持たない TUI ツール向けの画面テキスト判定ルール（ADR-0016）。
// waiting → working の順に評価し、いずれにも一致しなければ Idle（herdr の残余 idle 方式）。
type paneRules struct {
	waiting []*regexp.Regexp
	working []*regexp.Regexp
}

// toolPaneRules はツールごとの画面テキスト判定ルールテーブル。
// 子プロセスを生成しない TUI ツール（agy 等）を対象とし、Claude/Codex は対象外
// （Claude は classifyClaudePane、Codex は HasChildProcesses + codexApprovalPattern を使う）。
var toolPaneRules = map[ToolType]paneRules{
	ToolAntigravity: {
		waiting: []*regexp.Regexp{
			// "Requesting permission for:" と "Do you want to proceed?" が両方画面にあることを
			// 1本の正規表現で要求する（(?s) で改行跨ぎを許容）。
			regexp.MustCompile(`(?is)requesting permission for:.*do you want to proceed\?`),
		},
		working: []*regexp.Regexp{
			regexp.MustCompile(`(?m)^\s*[\x{2800}-\x{28FF}]+\s+\p{L}`), // braille スピナー行（例: "⣻  Generating..."）
			regexp.MustCompile(`(?i)esc to cancel`),
		},
	},
	ToolOpenCode: {
		waiting: []*regexp.Regexp{
			regexp.MustCompile(`(?i)△ permission required`),
			regexp.MustCompile(`(?i)allow once\s+allow always\s+reject`),
		},
		working: []*regexp.Regexp{
			regexp.MustCompile(`(?i)esc (to )?interrupt`),
			regexp.MustCompile(`(■|⬝){4,}`), // 進捗バー（例: "⬝⬝■■■■■■  esc interrupt"）
		},
	},
}

// classifyByRules は rules に基づいて画面テキストを判定する。
// waiting を最優先、次に working、いずれにも一致しなければ Idle を返す（残余 idle 方式）。
func classifyByRules(rules paneRules, text string) SessionState {
	for _, re := range rules.waiting {
		if re.MatchString(text) {
			return Waiting
		}
	}
	for _, re := range rules.working {
		if re.MatchString(text) {
			return Thinking
		}
	}
	return Idle
}

// RefineToolUseState はペインテキストから状態を精緻化する。
//   - Claude: 全状態でペインテキストをチェック。classifyClaudePane が権威的ソース。
//     判定できた場合はその状態を採用。判定不能かつ JSONL=Waiting → ToolUse に降格。
//   - Codex: プロセス由来の Thinking/Idle → Waiting（承認待ち検出）
//   - ルールテーブル対象ツール（agy 等）: 画面テキストから Waiting/Thinking/Idle を判定
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
			if sess.isTaktPipe {
				// takt が stdio=pipe で起動した非対話 claude -p / codex exec は pane に takt 自身の
				// 出力しか映らないため、ツール種別を問わず画面判定をスキップする（ADR-0016 Decision 3）。
				// Codex を除外しないのは、takt のログが番号付き行を含むと codexApprovalPattern が
				// 誤って Waiting にし、自動承認モードが takt の pane に Enter を送りかねないため。
				// JSONL 由来の Waiting は --permission-mode 固定のため誤検知とみなし ToolUse に降格する
				// （hook Waiting は保護）。
				if !sess.HookWaiting && sess.State == Waiting {
					s.projects[i].Sessions[j].State = ToolUse
				}
				continue
			}
			// Claude / Codex を toolPaneRules に登録してはならない。登録すると下の hasRules 分岐が
			// classifyClaudePane や hook Waiting 保護（ADR-0015）を迂回してしまう。
			rules, hasRules := toolPaneRules[sess.Tool]
			switch {
			case sess.Tool == ToolClaude && sess.PaneID != "":
				// Claude: 全状態でペインテキストをチェック
			case sess.Tool == ToolCodex:
				// Codex: 子プロセス有無で Thinking/Idle 判定後、承認待ちなら Waiting に上書きする
			case hasRules && sess.PaneID != "":
				// ルールテーブルにエントリのあるツール（agy 等）: 子プロセス検査ができないため画面テキストで判定する
			default:
				continue
			}
			text, ok := getPaneText(sess.PaneID)
			if !ok {
				continue
			}

			if hasRules {
				// pane テキストが取得できた場合はそのまま採用する（取得失敗時は既存の Thinking を維持）。
				s.projects[i].Sessions[j].State = classifyByRules(rules, text)
				continue
			}

			if sess.Tool == ToolClaude {
				beforeState := sess.State
				newState, classified := classifyClaudePane(text)

				if sess.HookWaiting {
					// hook 由来の Waiting は classifyClaudePane の結果で上書きしない（ADR-0015）。
					// Idle 連続カウント（解除の安全網）だけを進める。
					if s.hookStore != nil {
						s.hookStore.NoteScanResult(sess.PaneID, classified && newState == Idle)
					}
					continue
				}

				afterState := beforeState
				if classified {
					s.projects[i].Sessions[j].State = newState
					s.projects[i].Sessions[j].StateSource = SourcePane
					afterState = newState
				} else if beforeState == Waiting {
					// ペインテキストから判定不能だが JSONL が Waiting → ToolUse に降格
					s.projects[i].Sessions[j].State = ToolUse
					afterState = ToolUse
				}
				downgradedFromWaiting := beforeState == Waiting && afterState != Waiting

				if !classified || downgradedFromWaiting {
					diagKey := fmt.Sprintf("%s|%s|%t", beforeState, afterState, classified)
					if s.lastDiagKey[sess.PaneID] != diagKey {
						s.lastDiagKey[sess.PaneID] = diagKey
						debugf(
							"claude pane diagnostic: pane_id=%q pid=%d cwd=%q before=%s after=%s ok=%t downgraded=%t pane_tail(last 30 lines):\n%s",
							sess.PaneID,
							sess.PID,
							sess.WorkingDir,
							beforeState,
							afterState,
							classified,
							downgradedFromWaiting,
							tailLines(text, 30),
						)
					}
				} else {
					// 通常状態に戻ったらキーを消し、次の取りこぼしを再度記録できるようにする
					delete(s.lastDiagKey, sess.PaneID)
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

func tailLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[len(lines)-n:], "\n")
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
		if isSpinnerLine(trimmed) || strings.Contains(trimmed, "Running…") {
			return Thinking, true
		}
	}

	// Step 5: Working シグナルなし + 入力プロンプト行あり → Idle
	return Idle, true
}

// claudeSpinnerGlyphs は Claude Code のスピナーがフレームごとに切り替える記号。
var claudeSpinnerGlyphs = []string{"·", "✢", "✳", "✶", "✻", "✽"}

// isSpinnerLine は行が作業中スピナー（例: "✻ Perambulating… (37s)"）か返す。
// 完了時の "✻ Worked for 4m 12s" も同じ記号で始まるため、進行中を示す "…" を必須にする。
func isSpinnerLine(line string) bool {
	if !strings.Contains(line, "…") {
		return false
	}
	for _, g := range claudeSpinnerGlyphs {
		if strings.HasPrefix(line, g+" ") {
			return true
		}
	}
	return false
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
