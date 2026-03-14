package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// sessionRecord は内部管理用のセッション集約レコード。
// v2 Session（プロセスベース）とは別に、ファイルウォッチング由来の状態を保持する。
type sessionRecord struct {
	id          string
	projectPath string
	filePath    string
	state       SessionState
	lastActivity time.Time
}

// projectRecord は内部管理用のプロジェクト集約レコード。
type projectRecord struct {
	path        string
	displayName string
	sessions    []*sessionRecord
	activeCount int
}

// StateManager は watcher イベントをプロジェクト/セッション単位で集約管理する。
type StateManager struct {
	watcher           *Watcher
	incrementalReader *IncrementalReader
	projects          map[string]*projectRecord
	mu                sync.RWMutex
}

// NewStateManager は StateManager を初期化して返す。
func NewStateManager(watcher *Watcher) *StateManager {
	return &StateManager{
		watcher:           watcher,
		incrementalReader: NewIncrementalReader(),
		projects:          make(map[string]*projectRecord),
	}
}

// InitialScan はフルスキャンを実行し、集約状態を再構築する。
func (s *StateManager) InitialScan() error {
	return s.initialScan()
}

func (s *StateManager) initialScan() error {
	if s.watcher == nil {
		return errors.New("watcher is required")
	}

	// 第1段階: I/O が重い探索処理はロック外で実行する。
	projectPaths, err := s.watcher.DiscoverProjects()
	if err != nil {
		return err
	}

	type sessionInfo struct {
		projectPath string
		sessionID   string
		sessionPath string
	}

	var sessions []sessionInfo
	for _, projectPath := range projectPaths {
		sessionIDs, err := s.watcher.DiscoverSessions(projectPath)
		if err != nil {
			return err
		}

		for _, sessionID := range sessionIDs {
			sessionPath, err := resolveSessionFilePath(projectPath, sessionID)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			sessions = append(sessions, sessionInfo{
				projectPath: projectPath,
				sessionID:   sessionID,
				sessionPath: sessionPath,
			})
		}
	}

	// 第2段階: 収集結果をもとにロック下で状態を再構築する。
	s.mu.Lock()
	defer s.mu.Unlock()

	s.projects = make(map[string]*projectRecord)
	s.incrementalReader = NewIncrementalReader()

	for _, info := range sessions {
		if err := s.upsertFromFileLocked(info.projectPath, info.sessionID, info.sessionPath, false); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}

	return nil
}

// HandleEvent は単一イベントを集約状態へ反映する。
func (s *StateManager) HandleEvent(event WatchEvent) error {
	return s.handleEvent(event)
}

func (s *StateManager) handleEvent(event WatchEvent) error {
	switch event.Type {
	case Removed:
		sessionPath := event.Path
		if sessionPath != "" {
			sessionPath = filepath.Clean(sessionPath)
		}
		s.removeSession(event.ProjectPath, event.SessionID, sessionPath)
		return nil
	case Created, Modified:
		sessionPath := filepath.Clean(event.Path)
		if event.Path == "" {
			resolvedPath, err := resolveSessionFilePath(event.ProjectPath, event.SessionID)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			sessionPath = resolvedPath
		}

		// 新規作成イベントは既存オフセットを捨てて先頭から読む。
		reset := event.Type == Created
		if err := s.upsertFromFile(event.ProjectPath, event.SessionID, sessionPath, reset); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				s.removeSession(event.ProjectPath, event.SessionID, sessionPath)
				return nil
			}
			return err
		}
		return nil
	default:
		return nil
	}
}

// GetProjects は全プロジェクトのスナップショット（コピー）を返す。
func (s *StateManager) GetProjects() []Project {
	return s.Projects()
}

// Projects は全プロジェクトのスナップショット（コピー）を返す。
func (s *StateManager) Projects() []Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	projects := make([]Project, 0, len(s.projects))
	for _, rec := range s.projects {
		sessions := make([]*Session, 0, len(rec.sessions))
		for _, sr := range rec.sessions {
			if sr == nil {
				continue
			}
			s := &Session{
				ID:           sr.id,
				ProjectPath:  sr.projectPath,
				FilePath:     sr.filePath,
				State:        sr.state,
				LastActivity: sr.lastActivity,
			}
			sessions = append(sessions, s)
		}
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].ID < sessions[j].ID
		})
		proj := Project{
			Path:        rec.path,
			Name:        rec.displayName,
			DisplayName: rec.displayName,
			ActiveCount: rec.activeCount,
			Sessions:    sessions,
		}
		projects = append(projects, proj)
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Path < projects[j].Path
	})

	return projects
}

// Summary は集計情報を返す（StateReader 実装）。
func (s *StateManager) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total, active, waiting := 0, 0, 0
	for _, rec := range s.projects {
		for _, sr := range rec.sessions {
			if sr == nil {
				continue
			}
			total++
			switch sr.state {
			case Thinking, ToolUse:
				active++
			case Waiting:
				waiting++
			}
		}
	}
	return Summary{
		TotalSessions: total,
		Active:        active,
		Waiting:       waiting,
		ByTool:        map[string]int{},
	}
}

// Panes は管理ペイン一覧を返す（StateReader 実装）。
// v1 watcher ベースでは未使用のため空スライスを返す。
func (s *StateManager) Panes() []terminal.Pane {
	return nil
}

// GetStatus は現在時刻付きのステータス出力を生成する。
func (s *StateManager) GetStatus() StatusOutput {
	return StatusOutput{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *StateManager) upsertFromFile(projectPath, sessionID, sessionPath string, reset bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertFromFileLocked(projectPath, sessionID, sessionPath, reset)
}

func (s *StateManager) upsertFromFileLocked(projectPath, sessionID, sessionPath string, reset bool) error {
	if s.incrementalReader == nil {
		s.incrementalReader = NewIncrementalReader()
	}
	if reset {
		s.incrementalReader.Reset(sessionPath)
	}

	entries, err := s.incrementalReader.ReadNew(sessionPath)
	if err != nil {
		return err
	}

	project := s.getOrCreateProject(projectPath)
	session := findSessionRecord(project.sessions, sessionID)
	if session == nil {
		session = &sessionRecord{
			id:          sessionID,
			projectPath: projectPath,
			state:       Idle,
		}
		project.sessions = append(project.sessions, session)
	}

	session.filePath = sessionPath
	if len(entries) > 0 {
		session.state = DetermineSessionState(entries)
		if lastCreatedAt := latestEntryCreatedAt(entries); !lastCreatedAt.IsZero() {
			session.lastActivity = lastCreatedAt
		}
	}

	if session.lastActivity.IsZero() {
		if info, statErr := os.Stat(sessionPath); statErr == nil {
			session.lastActivity = info.ModTime()
		}
	}

	refreshActiveCount(project)
	return nil
}

func (s *StateManager) removeSession(projectPath, sessionID, sessionPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, ok := s.projects[projectPath]
	if !ok {
		return
	}

	var pathsToReset []string
	if sessionPath != "" && sessionPath != "." {
		pathsToReset = append(pathsToReset, sessionPath)
	}

	filtered := make([]*sessionRecord, 0, len(project.sessions))
	for _, session := range project.sessions {
		if session == nil {
			continue
		}

		matchedByID := sessionID != "" && session.id == sessionID
		matchedByPath := sessionID == "" && sessionPath != "" && sessionPath != "." && session.filePath == sessionPath
		if matchedByID || matchedByPath {
			if session.filePath != "" && session.filePath != sessionPath {
				pathsToReset = append(pathsToReset, session.filePath)
			}
			continue
		}

		filtered = append(filtered, session)
	}

	if s.incrementalReader != nil {
		for _, p := range pathsToReset {
			s.incrementalReader.Reset(p)
		}
	}

	project.sessions = filtered
	if len(project.sessions) == 0 {
		delete(s.projects, projectPath)
		return
	}

	refreshActiveCount(project)
}

func (s *StateManager) getOrCreateProject(projectPath string) *projectRecord {
	if project, ok := s.projects[projectPath]; ok {
		return project
	}

	project := &projectRecord{
		path:        projectPath,
		displayName: filepath.Base(projectPath),
		sessions:    []*sessionRecord{},
	}
	s.projects[projectPath] = project
	return project
}

func resolveSessionFilePath(projectPath, sessionID string) (string, error) {
	candidates := []string{
		filepath.Join(projectPath, sessionID+".jsonl"),
		filepath.Join(projectPath, sessionID+".json"),
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		if info.IsDir() {
			continue
		}
		return candidate, nil
	}

	return "", os.ErrNotExist
}

func findSessionRecord(sessions []*sessionRecord, sessionID string) *sessionRecord {
	for _, session := range sessions {
		if session != nil && session.id == sessionID {
			return session
		}
	}
	return nil
}

func latestEntryCreatedAt(entries []*Entry) time.Time {
	// 末尾側が最新想定のため逆順に最初の有効時刻を返す。
	// v2 では CreatedAt が削除されたため Timestamp 文字列をパースする。
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == nil || entry.Timestamp == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			return t
		}
	}
	return time.Time{}
}

func refreshActiveCount(project *projectRecord) {
	count := 0
	for _, session := range project.sessions {
		if session == nil {
			continue
		}
		if session.state == Thinking || session.state == ToolUse {
			count++
		}
	}
	project.activeCount = count
}
