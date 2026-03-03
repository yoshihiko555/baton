package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRecordValid(t *testing.T) {
	line := []byte(`{"type":"thinking","role":"assistant","created_at":"2026-03-01T10:00:00Z"}` + "\n")

	entry, err := ParseRecord(line)
	if err != nil {
		t.Fatalf("ParseRecord returned error: %v", err)
	}

	if entry.Type != "thinking" {
		t.Fatalf("unexpected Type: got %q", entry.Type)
	}
	if entry.Role != "assistant" {
		t.Fatalf("unexpected Role: got %q", entry.Role)
	}
	if entry.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt should be parsed")
	}

	wantRaw := `{"type":"thinking","role":"assistant","created_at":"2026-03-01T10:00:00Z"}`
	if string(entry.Raw) != wantRaw {
		t.Fatalf("unexpected Raw: got %q, want %q", string(entry.Raw), wantRaw)
	}

	// Verify Raw is copied and not tied to the original input buffer.
	line[2] = 'X'
	if string(entry.Raw) != wantRaw {
		t.Fatalf("Raw should not change after source mutation: got %q", string(entry.Raw))
	}
}

func TestParseRecordInvalidJSON(t *testing.T) {
	_, err := ParseRecord([]byte(`{"type":"thinking"` + "\n"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseRecordEmptyLine(t *testing.T) {
	_, err := ParseRecord([]byte(" \t \n"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDetermineSessionState(t *testing.T) {
	tests := []struct {
		name    string
		entries []*Entry
		want    SessionState
	}{
		{
			name:    "nil entries",
			entries: nil,
			want:    Idle,
		},
		{
			name:    "empty entries",
			entries: []*Entry{},
			want:    Idle,
		},
		{
			name: "last entry thinking",
			entries: []*Entry{
				{Type: "tool_use"},
				{Type: "thinking"},
			},
			want: Thinking,
		},
		{
			name: "tool use normalizes hyphen and case",
			entries: []*Entry{
				{Type: "Tool-Use"},
			},
			want: ToolUse,
		},
		{
			name: "error with spaces",
			entries: []*Entry{
				{Type: "  ERROR  "},
			},
			want: Error,
		},
		{
			name: "unknown type falls back to idle",
			entries: []*Entry{
				{Type: "assistant_message"},
			},
			want: Idle,
		},
		{
			name: "last entry nil falls back to idle",
			entries: []*Entry{
				{Type: "thinking"},
				nil,
			},
			want: Idle,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := DetermineSessionState(tc.entries)
			if got != tc.want {
				t.Fatalf("unexpected state: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewIncrementalReader(t *testing.T) {
	r := NewIncrementalReader()
	if r == nil {
		t.Fatalf("NewIncrementalReader returned nil")
	}
	if r.offsets == nil {
		t.Fatalf("offsets map should be initialized")
	}
	if len(r.offsets) != 0 {
		t.Fatalf("offsets should be empty on creation")
	}
}

func TestIncrementalReaderReadNewBasicAndOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"thinking","role":"assistant"}` + "\n" +
		`{"type":"tool_use","role":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entries, err := r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected entries length: got %d, want 2", len(entries))
	}
	if entries[0].Type != "thinking" || entries[1].Type != "tool_use" {
		t.Fatalf("unexpected entry types: got %q, %q", entries[0].Type, entries[1].Type)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if r.offsets[path] != info.Size() {
		t.Fatalf("offset should match file size: got %d, want %d", r.offsets[path], info.Size())
	}

	entries, err = r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew second call failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no new entries, got %d", len(entries))
	}
	if r.offsets[path] != info.Size() {
		t.Fatalf("offset should stay the same: got %d, want %d", r.offsets[path], info.Size())
	}
}

func TestIncrementalReaderReadNewSkipsIncompleteLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	first := `{"type":"thinking","role":"assistant"}` + "\n"
	second := `{"type":"tool_use","role":"assistant"}` + "\n"
	thirdIncomplete := `{"type":"error","role":"assistant"}`

	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entries, err := r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Type != "thinking" {
		t.Fatalf("unexpected initial entries")
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if _, err := file.WriteString(second + thirdIncomplete); err != nil {
		file.Close()
		t.Fatalf("WriteString failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// The trailing entry (no newline) is valid JSON, so it is parsed
	// and the offset advances past it.
	entries, err = r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew for appended content failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two entries (tool_use + valid trailing error), got %d", len(entries))
	}
	if entries[0].Type != "tool_use" || entries[1].Type != "error" {
		t.Fatalf("unexpected types: got %q, %q", entries[0].Type, entries[1].Type)
	}

	wantOffset := int64(len(first + second + thirdIncomplete))
	if r.offsets[path] != wantOffset {
		t.Fatalf("offset should include valid trailing entry: got %d, want %d", r.offsets[path], wantOffset)
	}

	// Appending a newline should not produce duplicate entries.
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		file.Close()
		t.Fatalf("WriteString failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	entries, err = r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew after appending newline failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no new entries after appending newline, got %d", len(entries))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if r.offsets[path] != info.Size() {
		t.Fatalf("offset should match full file size: got %d, want %d", r.offsets[path], info.Size())
	}
}

func TestIncrementalReaderReadNewDetectsRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	initial := `{"type":"thinking","role":"assistant"}` + "\n" +
		`{"type":"tool_use","role":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	if _, err := r.ReadNew(path); err != nil {
		t.Fatalf("ReadNew failed: %v", err)
	}

	rotated := `{"type":"error","role":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(rotated), 0o644); err != nil {
		t.Fatalf("WriteFile rotation failed: %v", err)
	}

	entries, err := r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew after rotation failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Type != "error" {
		t.Fatalf("expected one entry from rotated file, got %d", len(entries))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if r.offsets[path] != info.Size() {
		t.Fatalf("offset should be updated to rotated file size: got %d, want %d", r.offsets[path], info.Size())
	}
}

func TestIncrementalReaderReadNewInvalidLineSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"thinking","role":"assistant"}` + "\n" +
		`{"type":"tool_use"` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entries, err := r.ReadNew(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 valid entry (invalid line skipped), got %d", len(entries))
	}
	if entries[0].Type != "thinking" {
		t.Fatalf("expected type thinking, got %s", entries[0].Type)
	}
}

func TestIncrementalReaderReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"thinking","role":"assistant"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entries, err := r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if r.offsets[path] == 0 {
		t.Fatalf("offset should be advanced")
	}

	r.Reset(path)
	if _, ok := r.offsets[path]; ok {
		t.Fatalf("offset should be removed after reset")
	}

	entries, err = r.ReadNew(path)
	if err != nil {
		t.Fatalf("ReadNew after reset failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Type != "thinking" {
		t.Fatalf("expected to read from beginning after reset")
	}
}
