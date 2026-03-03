package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// ParseRecord は JSONL の1行を Entry に変換し、元の JSON も保持する。
func ParseRecord(line []byte) (*Entry, error) {
	record := bytes.TrimSpace(line)
	if len(record) == 0 {
		return nil, errors.New("empty record")
	}

	entry := &Entry{}
	if err := json.Unmarshal(record, entry); err != nil {
		return nil, err
	}

	entry.Raw = append(json.RawMessage(nil), record...)
	return entry, nil
}

// DetermineSessionState は末尾エントリの type からセッション状態を判定する。
func DetermineSessionState(entries []*Entry) SessionState {
	if len(entries) == 0 || entries[len(entries)-1] == nil {
		return Idle
	}

	lastType := strings.ToLower(strings.TrimSpace(entries[len(entries)-1].Type))
	lastType = strings.ReplaceAll(lastType, "-", "_")

	switch lastType {
	case "thinking":
		return Thinking
	case "tool_use":
		return ToolUse
	case "error":
		return Error
	default:
		return Idle
	}
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

// Reset は指定ファイルの読み取りオフセットを破棄する。
func (r *IncrementalReader) Reset(filepath string) {
	if r.offsets == nil {
		return
	}
	delete(r.offsets, filepath)
}
