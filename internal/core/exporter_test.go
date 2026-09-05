package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExporterwriteAtomicJSONValidJSON(t *testing.T) {
	// 正常系: 整形 JSON が出力され、構文としても妥当であることを確認する。
	destPath := filepath.Join(t.TempDir(), "status.json")

	status := StatusOutput{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Projects: []ProjectOutput{
			{
				Name: "project-a",
				Path: "/tmp/project-a",
				Sessions: []SessionOutput{
					{
						PID:   1234,
						Tool:  "claude",
						State: "thinking",
					},
				},
			},
		},
	}

	if err := writeAtomicJSON(status, destPath); err != nil {
		t.Fatalf("writeAtomicJSON returned error: %v", err)
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

func TestExporterwriteAtomicJSONAtomicReplace(t *testing.T) {
	// 置換時に新旧ファイル記述子が分離される（原子的置換）ことを確認する。
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
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Projects: []ProjectOutput{
			{
				Name: "project-b",
				Path: "/tmp/project-b",
			},
		},
	}

	if err := writeAtomicJSON(status, destPath); err != nil {
		t.Fatalf("writeAtomicJSON returned error: %v", err)
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

func TestExporterwriteAtomicJSONInvalidPath(t *testing.T) {
	// 異常系: 親ディレクトリが無いパスではエラーになることを確認する。
	destPath := filepath.Join(t.TempDir(), "missing", "status.json")

	err := writeAtomicJSON(StatusOutput{}, destPath)
	if err == nil {
		t.Fatalf("expected error for invalid destination path, got nil")
	}
}

func TestToSessionOutputHookFieldsJSON(t *testing.T) {
	tests := []struct {
		name           string
		session        Session
		wantHookFields bool
	}{
		{
			name: "hook fields set",
			session: Session{
				SessionID:      "sess-1",
				TranscriptPath: "/path/to/transcript.jsonl",
				StateSource:    SourceHook,
			},
			wantHookFields: true,
		},
		{
			name:           "hook fields omitted when empty",
			session:        Session{},
			wantHookFields: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, err := json.Marshal(toSessionOutput(tc.session))
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(content, &payload); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}

			if !tc.wantHookFields {
				for _, key := range []string{"session_id", "transcript_path", "state_source"} {
					if _, ok := payload[key]; ok {
						t.Errorf("unexpected key %q in JSON: %s", key, content)
					}
				}
				return
			}

			want := map[string]string{
				"session_id":      "sess-1",
				"transcript_path": "/path/to/transcript.jsonl",
				"state_source":    SourceHook,
			}
			for key, value := range want {
				if got := payload[key]; got != value {
					t.Errorf("%s = %#v, want %q", key, got, value)
				}
			}
		})
	}
}

func TestExporterWriteReadStatusRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		hookListener bool
	}{
		{name: "listener", hookListener: true},
		{name: "non-listener", hookListener: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destPath := filepath.Join(t.TempDir(), "status.json")
			manager := NewStateManager(nil)
			manager.projects = []Project{
				{
					Name: "project-a",
					Path: "/tmp/project-a",
					Sessions: []*Session{
						{
							PID:            1234,
							Tool:           ToolClaude,
							State:          Waiting,
							PaneID:         "%1",
							WorkingDir:     "/tmp/project-a",
							SessionID:      "session-1",
							TranscriptPath: "/tmp/transcript.jsonl",
							StateSource:    SourceHook,
						},
					},
				},
			}
			manager.summary = calcSummary(manager.projects)

			exporter := NewExporter(destPath, ExporterConfig{})
			if tc.hookListener {
				exporter.SetHookListener(true)
			}
			if err := exporter.Write(manager); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}

			got, err := ReadStatus(destPath)
			if err != nil {
				t.Fatalf("ReadStatus returned error: %v", err)
			}
			if got.Version != 2 {
				t.Errorf("Version = %d, want 2", got.Version)
			}
			if got.HookListener != tc.hookListener {
				t.Errorf("HookListener = %v, want %v", got.HookListener, tc.hookListener)
			}
			if _, err := time.Parse(time.RFC3339, got.Timestamp); err != nil {
				t.Errorf("Timestamp = %q, want RFC3339: %v", got.Timestamp, err)
			}
			if len(got.Projects) != 1 || len(got.Projects[0].Sessions) != 1 {
				t.Fatalf("Projects = %#v, want one project with one session", got.Projects)
			}
			session := got.Projects[0].Sessions[0]
			if session.PaneID != "%1" || session.SessionID != "session-1" || session.StateSource != SourceHook {
				t.Errorf("session = %#v, want pane/session/source hook fields", session)
			}
			if got.Summary.TotalSessions != 1 || got.Summary.Waiting != 1 {
				t.Errorf("Summary = %#v, want one waiting session", got.Summary)
			}
		})
	}
}

func TestReadStatusNotExist(t *testing.T) {
	_, err := ReadStatus(filepath.Join(t.TempDir(), "missing-status.json"))
	if err == nil {
		t.Fatal("ReadStatus returned nil error for nonexistent path")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("errors.Is(err, os.ErrNotExist) = false: %v", err)
	}
}
