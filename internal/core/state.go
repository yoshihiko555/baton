package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// StateManager aggregates watcher events into in-memory project/session state.
type StateManager struct {
	watcher           *Watcher
	incrementalReader *IncrementalReader
	projects          map[string]*Project
	mu                sync.RWMutex
}

// NewStateManager creates a new StateManager.
func NewStateManager(watcher *Watcher) *StateManager {
	return &StateManager{
		watcher:           watcher,
		incrementalReader: NewIncrementalReader(),
		projects:          make(map[string]*Project),
	}
}

// InitialScan performs a full scan and rebuilds in-memory aggregated state.
func (s *StateManager) InitialScan() error {
	return s.initialScan()
}

func (s *StateManager) initialScan() error {
	if s.watcher == nil {
		return errors.New("watcher is required")
	}

	// Phase 1: discover projects and sessions outside the lock (I/O heavy).
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

	// Phase 2: acquire lock and build state from discovered data.
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

// HandleEvent applies a single watcher event to in-memory state.
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

// GetProjects returns a snapshot copy of all projects.
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

		sort.Slice(projectCopy.Sessions, func(i, j int) bool {
			return projectCopy.Sessions[i].ID < projectCopy.Sessions[j].ID
		})

		projects = append(projects, projectCopy)
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Path < projects[j].Path
	})

	return projects
}

// GetStatus returns the aggregated status payload.
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
		session.State = DetermineSessionState(entries)
		if lastCreatedAt := latestEntryCreatedAt(entries); !lastCreatedAt.IsZero() {
			session.LastActivity = lastCreatedAt
		}
	}

	if session.LastActivity.IsZero() {
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

	// Collect file paths to reset from the incremental reader.
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
		Path:        projectPath,
		DisplayName: filepath.Base(projectPath),
		Sessions:    []*Session{},
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

func findSession(sessions []*Session, sessionID string) *Session {
	for _, session := range sessions {
		if session != nil && session.ID == sessionID {
			return session
		}
	}
	return nil
}

func latestEntryCreatedAt(entries []*Entry) time.Time {
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
