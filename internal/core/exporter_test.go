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
