package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestDiscoverProjects(t *testing.T) {
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
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")
	sessionPath := filepath.Join(projectPath, "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	event := waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.Type == Created && e.ProjectPath == projectPath && e.SessionID == "session-1"
	})
	if event.Path != sessionPath {
		t.Fatalf("unexpected event path: %s", event.Path)
	}

	cancel()
	watcher.Stop()

	// After Stop(), no new events should be emitted.
	// Drain any remaining buffered events.
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-watcher.Events():
			// drain
		case <-timer.C:
			return
		}
	}
}

func TestWatcher_FileModified(t *testing.T) {
	baseDir := t.TempDir()
	projectPath := filepath.Join(baseDir, "project-a")
	sessionPath := filepath.Join(projectPath, "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.Type == Created && e.SessionID == "session-1"
	})

	mustWriteFile(t, sessionPath, []byte("updated"))

	event := waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.Type == Modified && e.ProjectPath == projectPath && e.SessionID == "session-1"
	})
	if event.Path != sessionPath {
		t.Fatalf("unexpected event path: %s", event.Path)
	}

	cancel()
	watcher.Stop()
}

func TestWatcher_Debounce(t *testing.T) {
	baseDir := t.TempDir()
	sessionPath := filepath.Join(baseDir, "project-a", "session-1.jsonl")
	mustWriteFile(t, sessionPath, []byte("init"))

	watcher, cancel := startWatcher(t, baseDir)

	waitForEvent(t, watcher.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.Type == Created && e.SessionID == "session-1"
	})

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
