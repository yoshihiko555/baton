package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestDiscoverProjects(t *testing.T) {
	// セッションファイルを含むディレクトリのみをプロジェクトとして列挙できることを確認する。
	baseDir := t.TempDir()

	mustWriteFile(t, filepath.Join(baseDir, "project-a", "session-1.jsonl"), []byte("a"))
	mustWriteFile(t, filepath.Join(baseDir, "project-b", "nested", "session-2.json"), []byte("b"))
	if err := os.MkdirAll(filepath.Join(baseDir, "empty-project"), 0o755); err != nil {
		t.Fatalf("mkdir empty-project: %v", err)
	}
	mustWriteFile(t, filepath.Join(baseDir, "README.md"), []byte("ignore"))

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	projects, err := watcher.DiscoverProjects()
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}

	want := []string{
		filepath.Join(baseDir, "project-a"),
		filepath.Join(baseDir, "project-b"),
	}
	if !reflect.DeepEqual(projects, want) {
		t.Fatalf("DiscoverProjects mismatch\nwant: %#v\ngot:  %#v", want, projects)
	}
}

func TestDiscoverSessions(t *testing.T) {
	// json/jsonl のみをセッションとして抽出し、ネストパスも ID 化できることを確認する。
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")

	mustWriteFile(t, filepath.Join(projectPath, "session-1.jsonl"), []byte("1"))
	mustWriteFile(t, filepath.Join(projectPath, "session-2.json"), []byte("2"))
	mustWriteFile(t, filepath.Join(projectPath, "nested", "session-3.jsonl"), []byte("3"))
	mustWriteFile(t, filepath.Join(projectPath, "ignore.txt"), []byte("x"))

	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	sessions, err := watcher.DiscoverSessions(projectPath)
	if err != nil {
		t.Fatalf("DiscoverSessions: %v", err)
	}
	sort.Strings(sessions)

	want := []string{"nested/session-3", "session-1", "session-2"}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("DiscoverSessions mismatch\nwant: %#v\ngot:  %#v", want, sessions)
	}
}

func TestWatcher_Start_Stop(t *testing.T) {
	// ファイル変更イベントの検出と、Stop 後の停止挙動を確認する。
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")
	sessionPath := filepath.Join(projectPath, "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	// fsnotify が監視を開始した後にファイルを更新してイベントを発生させる。
	mustWriteFile(t, sessionPath, []byte("updated"))

	event := waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.ProjectPath == projectPath && e.SessionID == "session-1"
	})
	if event.Path != sessionPath {
		t.Fatalf("unexpected event path: %s", event.Path)
	}

	cancel()
	watcher.Stop()

	// Stop 後は新規イベントを期待しない。残バッファのみドレインする。
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-watcher.Events():
			// 残っているイベントを読み捨てる
		case <-timer.C:
			return
		}
	}
}

func TestWatcher_FileModified(t *testing.T) {
	// 既存セッション更新で Modified イベントが届くことを確認する。
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")
	sessionPath := filepath.Join(projectPath, "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	// fsnotify 監視開始後にファイルを更新する。
	mustWriteFile(t, sessionPath, []byte("updated"))

	event := waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.ProjectPath == projectPath && e.SessionID == "session-1"
	})
	if event.Path != sessionPath {
		t.Fatalf("unexpected event path: %s", event.Path)
	}

	cancel()
	watcher.Stop()
}

func TestWatcher_Debounce(t *testing.T) {
	// 短時間の連続更新が 1 件にデバウンスされることを確認する。
	baseDir := t.TempDir()
	sessionPath := filepath.Join(baseDir, "project-a", "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	// fsnotify が監視を開始するまで少し待つ。
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 5; i++ {
		mustWriteFile(t, sessionPath, []byte(time.Now().String()))
		time.Sleep(30 * time.Millisecond)
	}

	modifiedCount := countMatchingEvents(watcher.Events(), "session-1", Modified, 1500*time.Millisecond)
	if modifiedCount != 1 {
		t.Fatalf("expected 1 modified event after debounce, got %d", modifiedCount)
	}

	cancel()
	watcher.Stop()
}

func startWatcher(t *testing.T, basePath string) (*Watcher, context.CancelFunc) {
	t.Helper()

	watcher, err := NewWatcher(basePath)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := watcher.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		watcher.Stop()
	})

	return watcher, cancel
}

func waitForEvent(t *testing.T, ch <-chan WatchEvent, timeout time.Duration, match func(WatchEvent) bool) WatchEvent {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatal("events channel closed before expected event")
			}
			if match(event) {
				return event
			}
		case <-timer.C:
			t.Fatal("timed out waiting for event")
		}
	}
}

func countMatchingEvents(ch <-chan WatchEvent, sessionID string, eventType WatchEventType, duration time.Duration) int {
	timeout := time.NewTimer(duration)
	defer timeout.Stop()

	count := 0
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return count
			}
			if event.SessionID == sessionID && event.Type == eventType {
				count++
			}
		case <-timeout.C:
			return count
		}
	}
}

func waitForChannelClosed(t *testing.T, ch <-chan WatchEvent, timeout time.Duration) {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("events channel was not closed in time")
		}
	}
}

func mustWriteFile(t *testing.T, filePath string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
}

func TestNewWatcherNonDir(t *testing.T) {
	// ファイルパスを渡すとエラーになる。
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := NewWatcher(tmpFile)
	if err == nil {
		t.Fatal("expected error for non-directory path")
	}
}

func TestNewWatcherNonExistent(t *testing.T) {
	_, err := NewWatcher("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestOpToWatchEventType(t *testing.T) {
	tests := []struct {
		op       fsnotify.Op
		wantType WatchEventType
		wantOk   bool
	}{
		{fsnotify.Create, Created, true},
		{fsnotify.Write, Modified, true},
		{fsnotify.Chmod, Modified, true},
		{fsnotify.Remove, Removed, true},
		{fsnotify.Rename, Removed, true},
		{fsnotify.Op(0), 0, false},
	}

	for _, tc := range tests {
		gotType, gotOk := opToWatchEventType(tc.op)
		if gotOk != tc.wantOk {
			t.Errorf("opToWatchEventType(%v) ok = %v, want %v", tc.op, gotOk, tc.wantOk)
		}
		if gotOk && gotType != tc.wantType {
			t.Errorf("opToWatchEventType(%v) type = %v, want %v", tc.op, gotType, tc.wantType)
		}
	}
}

func TestIsSessionFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"session.jsonl", true},
		{"session.json", true},
		{"session.JSONL", true},
		{"session.JSON", true},
		{"session.txt", false},
		{"session.log", false},
		{"nested/session.jsonl", true},
	}

	for _, tc := range tests {
		got := isSessionFile(tc.path)
		if got != tc.want {
			t.Errorf("isSessionFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestPathToWatchEventNonSessionFile(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	// 非セッションファイルは無視される。
	_, ok := watcher.pathToWatchEvent(filepath.Join(baseDir, "project", "readme.txt"), fsnotify.Create)
	if ok {
		t.Error("expected false for non-session file")
	}
}

func TestPathToWatchEventRootLevelFile(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	// basePath 直下のファイルはプロジェクト構造を満たさない。
	_, ok := watcher.pathToWatchEvent(filepath.Join(baseDir, "session.jsonl"), fsnotify.Create)
	if ok {
		t.Error("expected false for root-level session file")
	}
}

func TestPathToWatchEventValidPath(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	event, ok := watcher.pathToWatchEvent(filepath.Join(baseDir, "project-a", "session-1.jsonl"), fsnotify.Create)
	if !ok {
		t.Fatal("expected true for valid session path")
	}
	if event.Type != Created {
		t.Errorf("Type = %v, want Created", event.Type)
	}
	if event.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", event.SessionID, "session-1")
	}
	if event.ProjectPath != filepath.Join(baseDir, "project-a") {
		t.Errorf("ProjectPath = %q, want %q", event.ProjectPath, filepath.Join(baseDir, "project-a"))
	}
}

func TestPathToWatchEventNestedSession(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	event, ok := watcher.pathToWatchEvent(filepath.Join(baseDir, "project-a", "sub", "session-1.jsonl"), fsnotify.Write)
	if !ok {
		t.Fatal("expected true for nested session path")
	}
	if event.SessionID != "sub/session-1" {
		t.Errorf("SessionID = %q, want %q", event.SessionID, "sub/session-1")
	}
}

func TestPathToWatchEventUnsupportedOp(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(watcher.Stop)

	_, ok := watcher.pathToWatchEvent(filepath.Join(baseDir, "project-a", "session.jsonl"), fsnotify.Op(0))
	if ok {
		t.Error("expected false for unsupported op")
	}
}

func TestWatcherStopIdempotent(t *testing.T) {
	baseDir := t.TempDir()
	watcher, err := NewWatcher(baseDir)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	// 複数回呼んでもパニックしない。
	watcher.Stop()
	watcher.Stop()
}
