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

// DetermineSessionState は末尾エントリから セッション状態を判定する。
// Claude Code の JSONL ではトップレベル type は "assistant"/"user" 等であり、
// セッション状態は message.content[].type（"thinking", "tool_use", "text" 等）から判定する。
func DetermineSessionState(entries []*Entry) SessionState {
	if len(entries) == 0 {
		return Idle
	}

	// 末尾から assistant エントリを探し、content の型で判定する。
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == nil {
			continue
		}

		entryType := strings.ToLower(strings.TrimSpace(entry.Type))

		// user エントリが先に見つかった場合、AI の応答待ち（thinking 相当）。
		if entryType == "user" {
			return Thinking
		}

		if entryType != "assistant" {
			continue
		}

		// assistant エントリの message.content を解析する。
		if entry.Message == nil || len(entry.Message.Content) == 0 {
			return Idle
		}

		// content の末尾ブロックの type で状態を判定する。
		lastContent := entry.Message.Content[len(entry.Message.Content)-1]
		contentType := strings.ToLower(strings.TrimSpace(lastContent.Type))
		contentType = strings.ReplaceAll(contentType, "-", "_")

		switch contentType {
		case "thinking":
			return Thinking
		case "tool_use":
			return ToolUse
		case "tool_result":
			return ToolUse
		case "error":
			return Error
		default:
			// "text" など → 応答完了
			return Idle
		}
	}

	return Idle
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
