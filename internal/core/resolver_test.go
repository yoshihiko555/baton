package core

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const (
	idleJSONL    = `{"type":"assistant","message":{"role":"assistant","content":[],"stop_reason":"end_turn"}}` + "\n"
	waitingJSONL = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}],"stop_reason":"tool_use"}}` + "\n"
)

func writeTestJSONL(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
}

func TestStateResolverResolvePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeTestJSONL(t, path, idleJSONL)
	resolver := NewStateResolver(NewIncrementalReader(), dir, dir, time.Second)

	resolved, _, err := resolver.ResolvePath(path)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if resolved.State != Idle {
		t.Errorf("State = %v, want %v", resolved.State, Idle)
	}
}

func TestStateResolverResolvePathNonexistent(t *testing.T) {
	dir := t.TempDir()
	resolver := NewStateResolver(NewIncrementalReader(), dir, dir, time.Second)

	resolved, normalizedPath, err := resolver.ResolvePath(filepath.Join(dir, "missing.jsonl"))
	if err == nil {
		t.Fatal("ResolvePath returned nil error for a nonexistent file")
	}
	if normalizedPath != "" {
		t.Errorf("normalized path = %q, want empty", normalizedPath)
	}
	if resolved.State != Thinking {
		t.Errorf("fallback State = %v, want %v", resolved.State, Thinking)
	}
}

func TestStateResolverResolvePathRejectsInvalidFile(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "session.txt")
	if err := os.WriteFile(txtPath, []byte("not jsonl"), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	directoryPath := filepath.Join(dir, "directory.jsonl")
	if err := os.Mkdir(directoryPath, 0o755); err != nil {
		t.Fatalf("os.Mkdir: %v", err)
	}
	resolver := NewStateResolver(NewIncrementalReader(), dir, dir, time.Second)

	for _, path := range []string{txtPath, directoryPath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			resolved, normalizedPath, err := resolver.ResolvePath(path)
			if err == nil {
				t.Fatalf("ResolvePath(%q) returned nil error", path)
			}
			if normalizedPath != "" {
				t.Errorf("normalized path = %q, want empty", normalizedPath)
			}
			if resolved.State != Thinking {
				t.Errorf("fallback State = %v, want %v", resolved.State, Thinking)
			}
		})
	}
}

func TestStateResolverResolvePathRejectsRelativePath(t *testing.T) {
	resolver := NewStateResolver(NewIncrementalReader(), t.TempDir(), t.TempDir(), time.Second)

	resolved, normalizedPath, err := resolver.ResolvePath("relative/path.jsonl")
	if err == nil {
		t.Fatal("ResolvePath returned nil error for a relative path")
	}
	if normalizedPath != "" {
		t.Errorf("normalized path = %q, want empty", normalizedPath)
	}
	if resolved.State != Thinking {
		t.Errorf("fallback State = %v, want %v", resolved.State, Thinking)
	}
}

func TestStateResolverResolvePathSymlinkMatchesExclude(t *testing.T) {
	realProjectDir := t.TempDir()
	cwd := "/workspace/shared"
	slugDir := filepath.Join(realProjectDir, cwdToSlug(cwd))
	realPath := filepath.Join(slugDir, "session.jsonl")
	writeTestJSONL(t, realPath, idleJSONL)

	projectDir := filepath.Join(t.TempDir(), "projects")
	if err := os.Symlink(realProjectDir, projectDir); err != nil {
		t.Fatalf("os.Symlink project dir: %v", err)
	}
	linkPath := filepath.Join(t.TempDir(), "link.jsonl")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatalf("os.Symlink transcript: %v", err)
	}
	resolver := NewStateResolver(NewIncrementalReader(), projectDir, realProjectDir, time.Second)

	resolved, normalizedPath, err := resolver.ResolvePath(linkPath)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if resolved.State != Idle {
		t.Errorf("State = %v, want %v", resolved.State, Idle)
	}
	expectedPath, err := filepath.EvalSymlinks(realPath)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(%q): %v", realPath, err)
	}
	if normalizedPath != expectedPath {
		t.Errorf("normalized path = %q, want %q", normalizedPath, expectedPath)
	}

	remaining, err := resolver.ResolveMultipleExcluding(cwd, 1, map[string]bool{
		normalizedPath: true,
	})
	if err != nil {
		t.Fatalf("ResolveMultipleExcluding: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("len(remaining) = %d, want 0", len(remaining))
	}
}

func TestStateResolverResolvePathRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "session.jsonl")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("syscall.Mkfifo: %v", err)
	}
	resolver := NewStateResolver(NewIncrementalReader(), dir, dir, time.Second)

	resolved, normalizedPath, err := resolver.ResolvePath(fifoPath)
	if err == nil {
		t.Fatal("ResolvePath returned nil error for a FIFO")
	}
	if normalizedPath != "" {
		t.Errorf("normalized path = %q, want empty", normalizedPath)
	}
	if resolved.State != Thinking {
		t.Errorf("fallback State = %v, want %v", resolved.State, Thinking)
	}
}

func TestStateResolverResolveMultipleExcluding(t *testing.T) {
	projectDir := t.TempDir()
	cwd := "/workspace/shared"
	slugDir := filepath.Join(projectDir, cwdToSlug(cwd))
	excludedPath := filepath.Join(slugDir, "excluded.jsonl")
	includedPath := filepath.Join(slugDir, "included.jsonl")
	writeTestJSONL(t, excludedPath, idleJSONL)
	writeTestJSONL(t, includedPath, waitingJSONL)
	resolver := NewStateResolver(NewIncrementalReader(), projectDir, projectDir, time.Second)
	excludedPath, err := filepath.EvalSymlinks(excludedPath)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(%q): %v", excludedPath, err)
	}

	resolved, err := resolver.ResolveMultipleExcluding(cwd, 2, map[string]bool{
		excludedPath: true,
	})
	if err != nil {
		t.Fatalf("ResolveMultipleExcluding: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if resolved[0].State != Waiting {
		t.Errorf("State = %v, want %v", resolved[0].State, Waiting)
	}
}

func TestStateResolverResolveOneJSONLSiblingMeta(t *testing.T) {
	projectDir := t.TempDir()
	metaDir := t.TempDir()
	slug := "-workspace-project"
	uuid := "11111111-1111-1111-1111-111111111111"
	jsonlPath := filepath.Join(projectDir, slug, uuid+".jsonl")
	writeTestJSONL(t, jsonlPath, idleJSONL)
	metaPath := filepath.Join(filepath.Dir(jsonlPath), uuid+".json")
	if err := os.WriteFile(metaPath, []byte(`{"title":"first prompt text","inputTokens":10,"outputTokens":20}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	resolver := NewStateResolver(NewIncrementalReader(), projectDir, metaDir, time.Second)

	resolved, _, err := resolver.ResolvePath(jsonlPath)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	assertResolvedMeta(t, resolved)
}

func TestStateResolverResolveOneJSONLFallbackMeta(t *testing.T) {
	projectDir := t.TempDir()
	metaDir := t.TempDir()
	slug := "-workspace-project"
	uuid := "22222222-2222-2222-2222-222222222222"
	jsonlPath := filepath.Join(projectDir, slug, uuid+".jsonl")
	writeTestJSONL(t, jsonlPath, idleJSONL)
	metaPath := filepath.Join(metaDir, slug, uuid+".json")
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}
	if err := os.WriteFile(metaPath, []byte(`{"title":"first prompt text","inputTokens":10,"outputTokens":20}`), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	resolver := NewStateResolver(NewIncrementalReader(), projectDir, metaDir, time.Second)

	resolved, _, err := resolver.ResolvePath(jsonlPath)
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	assertResolvedMeta(t, resolved)
}

func assertResolvedMeta(t *testing.T, resolved ResolvedSession) {
	t.Helper()
	if resolved.FirstPrompt != "first prompt text" {
		t.Errorf("FirstPrompt = %q, want %q", resolved.FirstPrompt, "first prompt text")
	}
	if resolved.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resolved.InputTokens)
	}
	if resolved.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", resolved.OutputTokens)
	}
}
