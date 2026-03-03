package core

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExporterWriteStatusJSONValidJSON(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "status.json")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	status := StatusOutput{
		Projects: []Project{
			{
				Path:        "/tmp/project-a",
				DisplayName: "project-a",
				ActiveCount: 1,
				Sessions: []*Session{
					{
						ID:           "session-1",
						ProjectPath:  "/tmp/project-a",
						State:        Thinking,
						LastActivity: now,
						PaneID:       "pane-1",
					},
				},
			},
		},
		UpdatedAt: now,
	}

	if err := WriteStatusJSON(status, destPath); err != nil {
		t.Fatalf("WriteStatusJSON returned error: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Contains(content, []byte("\n  \"projects\":")) {
		t.Fatalf("expected indented JSON output, got: %s", content)
	}

	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	projects, ok := payload["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("unexpected projects payload: %#v", payload["projects"])
	}
}

func TestExporterWriteStatusJSONAtomicReplace(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "status.json")
	oldContent := []byte("old-content\n")

	if err := os.WriteFile(destPath, oldContent, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	oldFile, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer oldFile.Close()

	status := StatusOutput{
		Projects: []Project{
			{
				Path:        "/tmp/project-b",
				DisplayName: "project-b",
			},
		},
		UpdatedAt: time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
	}

	if err := WriteStatusJSON(status, destPath); err != nil {
		t.Fatalf("WriteStatusJSON returned error: %v", err)
	}

	newContent, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if bytes.Equal(newContent, oldContent) {
		t.Fatalf("destination file was not replaced")
	}

	var payload map[string]any
	if err := json.Unmarshal(newContent, &payload); err != nil {
		t.Fatalf("new content is not valid JSON: %v", err)
	}

	if _, err := oldFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	staleContent, err := io.ReadAll(oldFile)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(staleContent, oldContent) {
		t.Fatalf("expected old descriptor to keep old content, got: %q", string(staleContent))
	}
}

func TestExporterWriteStatusJSONInvalidPath(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "missing", "status.json")

	err := WriteStatusJSON(StatusOutput{}, destPath)
	if err == nil {
		t.Fatalf("expected error for invalid destination path, got nil")
	}
}
