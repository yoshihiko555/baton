package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRecordValid(t *testing.T) {
	// 正常な JSONL 1行を Entry に変換できることを確認する。
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

	// Raw が入力バッファを参照せず、コピー保持されることを確認する。
	line[2] = 'X'
	if string(entry.Raw) != wantRaw {
		t.Fatalf("Raw should not change after source mutation: got %q", string(entry.Raw))
	}
}

func TestParseRecordInvalidJSON(t *testing.T) {
	// 不正 JSON はエラーになることを確認する。
	_, err := ParseRecord([]byte(`{"type":"thinking"` + "\n"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestParseRecordEmptyLine(t *testing.T) {
	// 空行（空白のみ含む）を拒否することを確認する。
	_, err := ParseRecord([]byte(" \t \n"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDetermineSessionState(t *testing.T) {
	// 末尾エントリの type から状態が決定されることを確認する。
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
			name: "assistant with thinking content",
			entries: []*Entry{
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "tool_use"}}}},
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "thinking"}}}},
			},
			want: Thinking,
		},
		{
			name: "tool use normalizes hyphen and case",
			entries: []*Entry{
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "Tool-Use"}}}},
			},
			want: ToolUse,
		},
		{
			name: "error content",
			entries: []*Entry{
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "  error  "}}}},
			},
			want: Error,
		},
		{
			name: "assistant with text content falls back to idle",
			entries: []*Entry{
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "text"}}}},
			},
			want: Idle,
		},
		{
			name: "user entry means thinking",
			entries: []*Entry{
				{Type: "user"},
			},
			want: Thinking,
		},
		{
			name: "last entry nil skips to previous entry",
			entries: []*Entry{
				{Type: "assistant", Message: &Message{Content: []ContentBlock{{Type: "thinking"}}}},
				nil,
			},
			want: Thinking,
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
	// コンストラクタが内部オフセットマップを初期化することを確認する。
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
	// 追記分のみ取得し、オフセットが維持されることを確認する。
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
	// 改行なし末尾データの扱い（有効 JSON は採用）を確認する。
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

	// 改行なし末尾でも JSON が妥当なら採用し、オフセットも進む。
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

	// その後に改行だけ追記しても重複エントリが出ないことを確認する。
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
	// ローテーション等でファイルが短くなっても先頭から再読できることを確認する。
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
	// 途中に不正行があっても有効行だけを返すことを確認する。
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
	// Reset 後は先頭から再読することを確認する。
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
