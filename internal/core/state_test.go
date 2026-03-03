package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStateManagerInitialScan(t *testing.T) {
	// 初期スキャンで複数プロジェクト/ネストセッションを正しく集約できることを確認する。
	baseDir := t.TempDir()
	projectAPath := filepath.Join(baseDir, "project-a")
	projectBPath := filepath.Join(baseDir, "project-b")

	writeJSONL(t, filepath.Join(projectAPath, "session-1.jsonl"),
		`{"type":"thinking","role":"assistant","created_at":"2026-03-01T09:00:00Z"}`,
		`{"type":"tool_use","role":"assistant","created_at":"2026-03-01T09:01:00Z"}`,
	)
	writeJSONL(t, filepath.Join(projectAPath, "session-2.jsonl"),
		`{"type":"error","role":"assistant","created_at":"2026-03-01T09:02:00Z"}`,
	)
	writeJSONL(t, filepath.Join(projectBPath, "nested", "session-3.jsonl"),
		`{"type":"assistant_message","role":"assistant","created_at":"2026-03-01T09:03:00Z"}`,
	)

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	manager := NewStateManager(watcher)
	if err := manager.InitialScan(); err != nil {
		t.Fatalf("InitialScan: %v", err)
	}

	projects := manager.GetProjects()
	if len(projects) != 2 {
		t.Fatalf("unexpected project count: got %d, want 2", len(projects))
	}

	projectA := mustProject(t, projects, projectAPath)
	if projectA.DisplayName != "project-a" {
		t.Fatalf("unexpected display name: got %q", projectA.DisplayName)
	}
	if projectA.ActiveCount != 1 {
		t.Fatalf("unexpected active count for project-a: got %d, want 1", projectA.ActiveCount)
	}
	if len(projectA.Sessions) != 2 {
		t.Fatalf("unexpected session count for project-a: got %d, want 2", len(projectA.Sessions))
	}

	session1 := mustSession(t, projectA, "session-1")
	if session1.State != ToolUse {
		t.Fatalf("unexpected state for session-1: got %v, want %v", session1.State, ToolUse)
	}
	wantLastActivity := mustRFC3339(t, "2026-03-01T09:01:00Z")
	if !session1.LastActivity.Equal(wantLastActivity) {
		t.Fatalf("unexpected last activity: got %s, want %s", session1.LastActivity, wantLastActivity)
	}

	session2 := mustSession(t, projectA, "session-2")
	if session2.State != Error {
		t.Fatalf("unexpected state for session-2: got %v, want %v", session2.State, Error)
	}

	projectB := mustProject(t, projects, projectBPath)
	if projectB.ActiveCount != 0 {
		t.Fatalf("unexpected active count for project-b: got %d, want 0", projectB.ActiveCount)
	}
	if len(projectB.Sessions) != 1 {
		t.Fatalf("unexpected session count for project-b: got %d, want 1", len(projectB.Sessions))
	}

	session3 := mustSession(t, projectB, "nested/session-3")
	if session3.State != Idle {
		t.Fatalf("unexpected state for nested/session-3: got %v, want %v", session3.State, Idle)
	}
}

func TestStateManagerHandleEvent(t *testing.T) {
	// Modified/Created/Removed の各イベントが状態へ反映されることを確認する。
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")
	session1Path := filepath.Join(projectPath, "session-1.jsonl")
	session2Path := filepath.Join(projectPath, "session-2.jsonl")

	writeJSONL(t, session1Path,
		`{"type":"thinking","role":"assistant","created_at":"2026-03-01T10:00:00Z"}`,
	)

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	manager := NewStateManager(watcher)
	if err := manager.InitialScan(); err != nil {
		t.Fatalf("InitialScan: %v", err)
	}

	appendJSONL(t, session1Path,
		`{"type":"tool_use","role":"assistant","created_at":"2026-03-01T10:01:00Z"}`,
	)
	_ = manager.HandleEvent(WatchEvent{
		Type:        Modified,
		Path:        session1Path,
		ProjectPath: projectPath,
		SessionID:   "session-1",
	})

	project := mustProject(t, manager.GetProjects(), projectPath)
	session1 := mustSession(t, project, "session-1")
	if session1.State != ToolUse {
		t.Fatalf("unexpected state for session-1 after modify: got %v, want %v", session1.State, ToolUse)
	}
	if !session1.LastActivity.Equal(mustRFC3339(t, "2026-03-01T10:01:00Z")) {
		t.Fatalf("unexpected last activity after modify: %s", session1.LastActivity)
	}

	writeJSONL(t, session2Path,
		`{"type":"error","role":"assistant","created_at":"2026-03-01T10:02:00Z"}`,
	)
	_ = manager.HandleEvent(WatchEvent{
		Type:        Created,
		Path:        session2Path,
		ProjectPath: projectPath,
		SessionID:   "session-2",
	})

	project = mustProject(t, manager.GetProjects(), projectPath)
	if len(project.Sessions) != 2 {
		t.Fatalf("unexpected session count after create: got %d, want 2", len(project.Sessions))
	}
	if project.ActiveCount != 1 {
		t.Fatalf("unexpected active count after create: got %d, want 1", project.ActiveCount)
	}
	session2 := mustSession(t, project, "session-2")
	if session2.State != Error {
		t.Fatalf("unexpected state for session-2: got %v, want %v", session2.State, Error)
	}

	if err := os.Remove(session1Path); err != nil {
		t.Fatalf("Remove session-1 file: %v", err)
	}
	_ = manager.HandleEvent(WatchEvent{
		Type:        Removed,
		Path:        session1Path,
		ProjectPath: projectPath,
		SessionID:   "session-1",
	})

	project = mustProject(t, manager.GetProjects(), projectPath)
	if len(project.Sessions) != 1 {
		t.Fatalf("unexpected session count after remove: got %d, want 1", len(project.Sessions))
	}
	if mustSession(t, project, "session-2").ID != "session-2" {
		t.Fatalf("session-2 should remain after remove")
	}
	if project.ActiveCount != 0 {
		t.Fatalf("unexpected active count after remove: got %d, want 0", project.ActiveCount)
	}
}

func TestStateManagerGetProjects(t *testing.T) {
	// GetProjects がソート済みかつ防御的コピーを返すことを確認する。
	baseDir := t.TempDir()
	projectAPath := filepath.Join(baseDir, "project-a")
	projectBPath := filepath.Join(baseDir, "project-b")

	writeJSONL(t, filepath.Join(projectAPath, "session-2.jsonl"),
		`{"type":"thinking","role":"assistant","created_at":"2026-03-01T11:01:00Z"}`,
	)
	writeJSONL(t, filepath.Join(projectAPath, "session-1.jsonl"),
		`{"type":"tool_use","role":"assistant","created_at":"2026-03-01T11:02:00Z"}`,
	)
	writeJSONL(t, filepath.Join(projectBPath, "session-1.jsonl"),
		`{"type":"error","role":"assistant","created_at":"2026-03-01T11:03:00Z"}`,
	)

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	manager := NewStateManager(watcher)
	if err := manager.InitialScan(); err != nil {
		t.Fatalf("InitialScan: %v", err)
	}

	projects := manager.GetProjects()
	if len(projects) != 2 {
		t.Fatalf("unexpected project count: got %d, want 2", len(projects))
	}
	if projects[0].Path != projectAPath || projects[1].Path != projectBPath {
		t.Fatalf("projects should be sorted by path")
	}

	projectA := mustProject(t, projects, projectAPath)
	if len(projectA.Sessions) != 2 {
		t.Fatalf("unexpected session count: got %d, want 2", len(projectA.Sessions))
	}
	if projectA.Sessions[0].ID != "session-1" || projectA.Sessions[1].ID != "session-2" {
		t.Fatalf("sessions should be sorted by id")
	}

	projects[0].DisplayName = "mutated"
	projects[0].Sessions[0].State = Idle

	fresh := manager.GetProjects()
	projectAFresh := mustProject(t, fresh, projectAPath)
	if projectAFresh.DisplayName == "mutated" {
		t.Fatalf("GetProjects should return copied project data")
	}
	if mustSession(t, projectAFresh, "session-1").State != ToolUse {
		t.Fatalf("GetProjects should return copied session data")
	}
}

func TestStateManagerGetStatus(t *testing.T) {
	// GetStatus が現在時刻と最新プロジェクト一覧を返すことを確認する。
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")

	writeJSONL(t, filepath.Join(projectPath, "session-1.jsonl"),
		`{"type":"thinking","role":"assistant","created_at":"2026-03-01T12:00:00Z"}`,
	)

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	manager := NewStateManager(watcher)
	if err := manager.InitialScan(); err != nil {
		t.Fatalf("InitialScan: %v", err)
	}

	before := time.Now().UTC()
	status := manager.GetStatus()

	if status.UpdatedAt.IsZero() {
		t.Fatalf("UpdatedAt should not be zero")
	}
	if status.UpdatedAt.Before(before) {
		t.Fatalf("UpdatedAt should be greater than scan timestamp")
	}

	wantProjects := manager.GetProjects()
	if !reflect.DeepEqual(status.Projects, wantProjects) {
		t.Fatalf("status projects mismatch")
	}
}

func writeJSONL(t *testing.T, filePath string, records ...string) {
	t.Helper()

	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, strings.TrimSpace(record))
	}
	payload := strings.Join(lines, "\n") + "\n"

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
}

func appendJSONL(t *testing.T, filePath string, records ...string) {
	t.Helper()

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", filePath, err)
	}
	defer file.Close()

	for _, record := range records {
		if _, err := file.WriteString(strings.TrimSpace(record) + "\n"); err != nil {
			t.Fatalf("append %s: %v", filePath, err)
		}
	}
}

func mustProject(t *testing.T, projects []Project, path string) Project {
	t.Helper()

	for _, project := range projects {
		if project.Path == path {
			return project
		}
	}
	t.Fatalf("project not found: %s", path)
	return Project{}
}

func mustSession(t *testing.T, project Project, sessionID string) *Session {
	t.Helper()

	for _, session := range project.Sessions {
		if session != nil && session.ID == sessionID {
			return session
		}
	}
	t.Fatalf("session not found: %s", sessionID)
	return nil
}

func mustRFC3339(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}
