package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Message は JSONL レコード内の message フィールドを表す。
type Message struct {
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason,omitempty"`
}

// ContentBlock は message.content[] の1要素を表す。
type ContentBlock struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// ProgressData は progress エントリの data フィールドを表す。
type ProgressData struct {
	Type string `json:"type"`
}

// Entry は JSONL ストリームの1レコードを表す。
type Entry struct {
	Type      string       `json:"type"`
	SubType   string       `json:"subtype,omitempty"`
	Message   Message      `json:"message,omitempty"`
	SessionID string       `json:"sessionId,omitempty"`
	GitBranch string       `json:"gitBranch,omitempty"`
	Timestamp string       `json:"timestamp,omitempty"`
	Data      ProgressData `json:"data,omitempty"`
}

// ParseRecord は JSONL の1行を Entry に変換する。
func ParseRecord(line []byte) (*Entry, error) {
	record := bytes.TrimSpace(line)
	if len(record) == 0 {
		return nil, errors.New("empty record")
	}

	entry := &Entry{}
	if err := json.Unmarshal(record, entry); err != nil {
		return nil, err
	}

	return entry, nil
}

// DetermineSessionState は末尾エントリからセッション状態を判定する。
func DetermineSessionState(entries []*Entry) SessionState {
	if len(entries) == 0 {
		return Idle
	}

	last := entries[len(entries)-1]
	if last == nil {
		return Idle
	}

	return classifyEntry(last)
}

func classifyEntry(e *Entry) SessionState {
	switch e.Type {
	case "system":
		if e.SubType == "turn_duration" {
			return Idle
		}
		return Idle
	case "assistant":
		switch e.Message.StopReason {
		case "end_turn":
			return Idle
		case "tool_use":
			if isSubagentToolUse(e) {
				return Thinking
			}
			return Waiting
		}

		if len(e.Message.Content) == 0 {
			return Idle
		}

		last := e.Message.Content[len(e.Message.Content)-1]
		switch last.Type {
		case "thinking":
			return Thinking
		case "error":
			return Error
		default:
			return Idle
		}
	case "progress":
		return ToolUse
	case "user":
		if len(e.Message.Content) > 0 && e.Message.Content[0].Type == "tool_result" {
			return Thinking
		}
		return Thinking
	default:
		return Idle
	}
}

// subagentToolNames はサブエージェント起動を示すツール名のセット。
// これらの tool_use は承認待ちではなくサブエージェント実行中と判定する。
var subagentToolNames = map[string]bool{
	"Agent":    true,
	"Task":     true,
	"Skill":    true,
	"dispatch": true,
}

// isSubagentToolUse は assistant エントリの最後の tool_use がサブエージェント系かを判定する。
func isSubagentToolUse(e *Entry) bool {
	for i := len(e.Message.Content) - 1; i >= 0; i-- {
		block := e.Message.Content[i]
		if block.Type == "tool_use" {
			return subagentToolNames[block.Name]
		}
	}
	return false
}

// IncrementalReader はファイル追記分だけを増分読み取りする。
type IncrementalReader struct {
	offsets map[string]int64
}

// NewIncrementalReader は IncrementalReader を初期化して返す。
func NewIncrementalReader() *IncrementalReader {
	return &IncrementalReader{
		offsets: make(map[string]int64),
	}
}

// ReadNew は filepath から未読の追記分のみを読み取る。
func (r *IncrementalReader) ReadNew(filepath string) ([]*Entry, error) {
	if r.offsets == nil {
		r.offsets = make(map[string]int64)
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	offset := r.offsets[filepath]
	if info.Size() < offset {
		// ローテーション等でファイルが短くなった場合は先頭から読み直す。
		offset = 0
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(file)
	nextOffset := offset
	var entries []*Entry

	for {
		line, err := reader.ReadBytes('\n')

		if err == nil {
			// 改行終端の完全な1行は常に消費済みとしてオフセットを進める。
			nextOffset += int64(len(line))
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			if entry, parseErr := ParseRecord(line); parseErr == nil {
				entries = append(entries, entry)
			}
			continue
		}

		if errors.Is(err, io.EOF) {
			// 末尾に改行なしデータがある場合:
			// - JSON として妥当なら最終エントリとして採用しオフセットを進める
			// - 不完全ならオフセットを据え置き、次回追記時に再読する
			if len(bytes.TrimSpace(line)) > 0 {
				if entry, parseErr := ParseRecord(line); parseErr == nil {
					entries = append(entries, entry)
					nextOffset += int64(len(line))
				}
			}
			break
		}

		return nil, err
	}

	r.offsets[filepath] = nextOffset
	return entries, nil
}

// ReadLastEntry は filepath の末尾から最終エントリ1件を返す。
// このメソッドは読み取りオフセットを更新しない。
//
// Claude Code の assistant エントリはツール呼び出し内容を含むため数万バイトに
// なることがある。初回は 64KB を読み、パース失敗時は段階的に読み取り範囲を
// 拡大して最大 512KB までリトライする。
func (r *IncrementalReader) ReadLastEntry(filepath string) (*Entry, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, nil
	}

	// 段階的に読み取り範囲を拡大する
	tailSizes := []int64{64 * 1024, 256 * 1024, 512 * 1024}
	for _, tailSize := range tailSizes {
		start := info.Size() - tailSize
		if start < 0 {
			start = 0
		}

		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}

		buf := make([]byte, info.Size()-start)
		n, err := io.ReadFull(file, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
		buf = buf[:n]

		lines := bytes.Split(buf, []byte{'\n'})
		for i := len(lines) - 1; i >= 0; i-- {
			if len(bytes.TrimSpace(lines[i])) == 0 {
				continue
			}

			entry, parseErr := ParseRecord(lines[i])
			if parseErr == nil {
				return entry, nil
			}
		}

		// ファイル全体を読んでもパースできなかった場合はリトライ不要
		if start == 0 {
			break
		}
	}

	return nil, nil
}

// Reset は指定ファイルの読み取りオフセットを破棄する。
func (r *IncrementalReader) Reset(filepath string) {
	if r.offsets == nil {
		return
	}
	delete(r.offsets, filepath)
}
