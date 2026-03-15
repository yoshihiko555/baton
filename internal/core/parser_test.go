package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRecordValid(t *testing.T) {
	// 正常な JSONL 1行を Entry に変換できることを確認する。
	line := []byte(`{"type":"assistant","subtype":"","message":{"role":"assistant","content":[],"stop_reason":"end_turn"},"timestamp":"2026-03-01T10:00:00Z"}` + "\n")

	entry, err := ParseRecord(line)
	if err != nil {
		t.Fatalf("ParseRecord returned error: %v", err)
	}

	if entry.Type != "assistant" {
		t.Fatalf("unexpected Type: got %q", entry.Type)
	}
	if entry.Message.Role != "assistant" {
		t.Fatalf("unexpected Message.Role: got %q", entry.Message.Role)
	}
	if entry.Timestamp != "2026-03-01T10:00:00Z" {
		t.Fatalf("unexpected Timestamp: got %q", entry.Timestamp)
	}
}

func TestParseRecordInvalidJSON(t *testing.T) {
	// 不正 JSON はエラーになることを確認する。
	_, err := ParseRecord([]byte(`{"type":"assistant"` + "\n"))
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
				{Type: "assistant", Message: Message{Content: []ContentBlock{{Type: "tool_use"}}}},
				{Type: "assistant", Message: Message{Content: []ContentBlock{{Type: "thinking"}}}},
			},
			want: Thinking,
		},
		{
			name: "assistant with stop_reason tool_use means Waiting",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "tool_use", Content: []ContentBlock{{Type: "tool_use", Name: "Bash"}}}},
			},
			want: Waiting,
		},
		{
			name: "assistant with stop_reason tool_use and Agent tool means Thinking",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "tool_use", Content: []ContentBlock{{Type: "text"}, {Type: "tool_use", Name: "Agent"}}}},
			},
			want: Thinking,
		},
		{
			name: "assistant with stop_reason tool_use and Task tool means Thinking",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "tool_use", Content: []ContentBlock{{Type: "tool_use", Name: "Task"}}}},
			},
			want: Thinking,
		},
		{
			name: "assistant with stop_reason tool_use and Skill tool means Thinking",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "tool_use", Content: []ContentBlock{{Type: "tool_use", Name: "Skill"}}}},
			},
			want: Thinking,
		},
		{
			name: "assistant with stop_reason tool_use but no content means Waiting",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "tool_use"}},
			},
			want: Waiting,
		},
		{
			name: "assistant with stop_reason end_turn means Idle",
			entries: []*Entry{
				{Type: "assistant", Message: Message{StopReason: "end_turn"}},
			},
			want: Idle,
		},
		{
			name: "error content",
			entries: []*Entry{
				{Type: "assistant", Message: Message{Content: []ContentBlock{{Type: "error"}}}},
			},
			want: Error,
		},
		{
			name: "assistant with text content falls back to idle",
			entries: []*Entry{
				{Type: "assistant", Message: Message{Content: []ContentBlock{{Type: "text"}}}},
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
			name: "user entry with tool_result content means thinking",
			entries: []*Entry{
				{Type: "user", Message: Message{Content: []ContentBlock{{Type: "tool_result"}}}},
			},
			want: Thinking,
		},
		{
			name: "progress entry means ToolUse",
			entries: []*Entry{
				{Type: "progress"},
			},
			want: ToolUse,
		},
		{
			name: "system entry with turn_duration means Idle",
			entries: []*Entry{
				{Type: "system", SubType: "turn_duration"},
			},
			want: Idle,
		},
		{
			name: "last entry nil means Idle",
			entries: []*Entry{
				{Type: "assistant", Message: Message{Content: []ContentBlock{{Type: "thinking"}}}},
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
	content := `{"type":"thinking"}` + "\n" +
		`{"type":"tool_use"}` + "\n"
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

	first := `{"type":"thinking"}` + "\n"
	second := `{"type":"tool_use"}` + "\n"
	thirdIncomplete := `{"type":"error"}`

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

	initial := `{"type":"thinking"}` + "\n" +
		`{"type":"tool_use"}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	if _, err := r.ReadNew(path); err != nil {
		t.Fatalf("ReadNew failed: %v", err)
	}

	rotated := `{"type":"error"}` + "\n"
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
	content := `{"type":"thinking"}` + "\n" +
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
	content := `{"type":"thinking"}` + "\n"
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

func TestReadLastEntry(t *testing.T) {
	// ReadLastEntry がオフセットを更新せず末尾エントリを返すことを確認する。
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"thinking"}` + "\n" +
		`{"type":"assistant","message":{"stop_reason":"end_turn"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entry, err := r.ReadLastEntry(path)
	if err != nil {
		t.Fatalf("ReadLastEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatalf("expected non-nil entry")
	}
	if entry.Type != "assistant" {
		t.Fatalf("unexpected Type: got %q, want %q", entry.Type, "assistant")
	}
	if entry.Message.StopReason != "end_turn" {
		t.Fatalf("unexpected StopReason: got %q, want %q", entry.Message.StopReason, "end_turn")
	}

	// ReadLastEntry はオフセットを更新しないことを確認する。
	if r.offsets[path] != 0 {
		t.Fatalf("ReadLastEntry should not update offset, got %d", r.offsets[path])
	}
}

func TestReadLastEntryEmptyFile(t *testing.T) {
	// 空ファイルでは nil, nil を返すことを確認する。
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	r := NewIncrementalReader()
	entry, err := r.ReadLastEntry(path)
	if err != nil {
		t.Fatalf("ReadLastEntry failed: %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil entry for empty file, got %+v", entry)
	}
}
