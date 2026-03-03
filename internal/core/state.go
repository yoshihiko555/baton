package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// StateManager は watcher イベントをプロジェクト/セッション単位で集約管理する。
type StateManager struct {
	watcher           *Watcher
	incrementalReader *IncrementalReader
	projects          map[string]*Project
	mu                sync.RWMutex
}

// NewStateManager は StateManager を初期化して返す。
func NewStateManager(watcher *Watcher) *StateManager {
	return &StateManager{
		watcher:           watcher,
		incrementalReader: NewIncrementalReader(),
		projects:          make(map[string]*Project),
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

	s.projects = make(map[string]*Project)
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	projects := make([]Project, 0, len(s.projects))
	for _, project := range s.projects {
		projectCopy := Project{
			Path:        project.Path,
			DisplayName: project.DisplayName,
			ActiveCount: project.ActiveCount,
			Sessions:    make([]*Session, 0, len(project.Sessions)),
		}

		for _, session := range project.Sessions {
			if session == nil {
				continue
			}
			sessionCopy := *session
			projectCopy.Sessions = append(projectCopy.Sessions, &sessionCopy)
		}

		// 呼び出し側で扱いやすいようセッション順を安定化する。
		sort.Slice(projectCopy.Sessions, func(i, j int) bool {
			return projectCopy.Sessions[i].ID < projectCopy.Sessions[j].ID
		})

		projects = append(projects, projectCopy)
	}

	// プロジェクト順も安定化する。
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Path < projects[j].Path
	})

	return projects
}

// GetStatus は現在時刻付きのステータス出力を生成する。
func (s *StateManager) GetStatus() StatusOutput {
	return StatusOutput{
		Projects:  s.GetProjects(),
		UpdatedAt: time.Now().UTC(),
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
		// Created などで再読込したい場合はオフセットを初期化する。
		s.incrementalReader.Reset(sessionPath)
	}

	entries, err := s.incrementalReader.ReadNew(sessionPath)
	if err != nil {
		return err
	}

	project := s.getOrCreateProject(projectPath)
	session := findSession(project.Sessions, sessionID)
	if session == nil {
		session = &Session{
			ID:          sessionID,
			ProjectPath: projectPath,
			State:       Idle,
		}
		project.Sessions = append(project.Sessions, session)
	}

	session.FilePath = sessionPath
	if len(entries) > 0 {
		// 新規エントリがある場合のみ状態と最終活動時刻を更新する。
		session.State = DetermineSessionState(entries)
		if lastCreatedAt := latestEntryCreatedAt(entries); !lastCreatedAt.IsZero() {
			session.LastActivity = lastCreatedAt
		}
	}

	if session.LastActivity.IsZero() {
		// created_at が無い場合はファイル更新時刻をフォールバックに使う。
		if info, statErr := os.Stat(sessionPath); statErr == nil {
			session.LastActivity = info.ModTime()
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

	// 削除対象に紐づくファイルのオフセットをリセット対象として収集する。
	var pathsToReset []string
	if sessionPath != "" && sessionPath != "." {
		pathsToReset = append(pathsToReset, sessionPath)
	}

	filtered := make([]*Session, 0, len(project.Sessions))
	for _, session := range project.Sessions {
		if session == nil {
			continue
		}

		matchedByID := sessionID != "" && session.ID == sessionID
		matchedByPath := sessionID == "" && sessionPath != "" && sessionPath != "." && session.FilePath == sessionPath
		if matchedByID || matchedByPath {
			// ID で一致した場合でも FilePath が異なれば併せてリセットする。
			if session.FilePath != "" && session.FilePath != sessionPath {
				pathsToReset = append(pathsToReset, session.FilePath)
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

	project.Sessions = filtered
	if len(project.Sessions) == 0 {
		delete(s.projects, projectPath)
		return
	}

	refreshActiveCount(project)
}

func (s *StateManager) getOrCreateProject(projectPath string) *Project {
	if project, ok := s.projects[projectPath]; ok {
		return project
	}

	project := &Project{
		Path: projectPath,
		// 表示名は最後のディレクトリ名を利用する。
		DisplayName: filepath.Base(projectPath),
		Sessions:    []*Session{},
	}
	s.projects[projectPath] = project
	return project
}

func resolveSessionFilePath(projectPath, sessionID string) (string, error) {
	// 互換性のため jsonl/json の両方を探索する。
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

func findSession(sessions []*Session, sessionID string) *Session {
	for _, session := range sessions {
		if session != nil && session.ID == sessionID {
			return session
		}
	}
	return nil
}

func latestEntryCreatedAt(entries []*Entry) time.Time {
	// 末尾側が最新想定のため逆順に最初の有効時刻を返す。
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == nil || entry.CreatedAt.IsZero() {
			continue
		}
		return entry.CreatedAt
	}
	return time.Time{}
}

func refreshActiveCount(project *Project) {
	// active は Thinking / ToolUse のみをカウントする。
	count := 0
	for _, session := range project.Sessions {
		if session == nil {
			continue
		}
		if session.State == Thinking || session.State == ToolUse {
			count++
		}
	}
	project.ActiveCount = count
}
