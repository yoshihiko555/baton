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

// ParseRecord parses a JSONL record into an Entry and stores the raw payload.
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

// DetermineSessionState determines the session state from the last entry type.
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

// IncrementalReader incrementally reads appended JSONL records from files.
type IncrementalReader struct {
	offsets map[string]int64
}

// NewIncrementalReader creates a new IncrementalReader.
func NewIncrementalReader() *IncrementalReader {
	return &IncrementalReader{
		offsets: make(map[string]int64),
	}
}

// ReadNew reads only newly appended complete lines from filepath.
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
			// Complete line (ends with '\n') — always advance offset.
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
			// Trailing data without '\n'. If it parses as valid JSON,
			// treat it as a final entry and advance offset. Otherwise
			// leave offset unchanged so the data is re-read once the
			// writer appends more bytes.
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

// Reset clears the stored offset for filepath.
func (r *IncrementalReader) Reset(filepath string) {
	if r.offsets == nil {
		return
	}
	delete(r.offsets, filepath)
}
