package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResolvedSession は解決済みのセッション状態と補助情報を保持する。
type ResolvedSession struct {
	State        SessionState
	Branch       string
	CurrentTool  string
	FirstPrompt  string
	InputTokens  int
	OutputTokens int
}

// StateResolver は CWD から JSONL と session-meta を解決して状態を返す。
type StateResolver struct {
	reader       *IncrementalReader
	projectDir   string
	metaDir      string
	scanInterval time.Duration
}

// NewStateResolver は StateResolver を生成する。
func NewStateResolver(reader *IncrementalReader, projectDir, metaDir string, scanInterval time.Duration) *StateResolver {
	return &StateResolver{
		reader:       reader,
		projectDir:   projectDir,
		metaDir:      metaDir,
		scanInterval: scanInterval,
	}
}

// sessionMeta は session-meta JSON の読み取り用構造体。
type sessionMeta struct {
	Title        string `json:"title"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
}

// ResolveState は対象プロセスに対応する最新セッション状態を解決する。
func (r *StateResolver) ResolveState(proc DetectedProcess) (ResolvedSession, error) {
	resolved := ResolvedSession{State: Thinking}

	if r == nil || r.reader == nil {
		return resolved, nil
	}

	const (
		slash = "/"
		dash  = "-"
	)

	slug := strings.ReplaceAll(proc.CWD, slash, dash)
	if slug == "" {
		return resolved, nil
	}

	dirPath := filepath.Join(r.projectDir, slug)
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return resolved, nil
		}
		return ResolvedSession{}, err
	}

	threshold := time.Now().Add(-r.scanInterval * 2)
	type activeFile struct {
		name string
		path string
	}

	activeFiles := make([]activeFile, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".jsonl" {
			continue
		}

		info, err := de.Info()
		if err != nil {
			return ResolvedSession{}, err
		}
		if info.ModTime().Before(threshold) {
			continue
		}

		activeFiles = append(activeFiles, activeFile{
			name: de.Name(),
			path: filepath.Join(dirPath, de.Name()),
		})
	}

	if len(activeFiles) == 0 {
		return resolved, nil
	}

	for _, f := range activeFiles {
		entry, err := r.reader.ReadLastEntry(f.path)
		if err != nil {
			return ResolvedSession{}, err
		}
		if entry == nil {
			continue
		}
		resolved.State = classifyEntry(entry)
	}

	for _, f := range activeFiles {
		entries, err := r.reader.ReadNew(f.path)
		if err != nil {
			return ResolvedSession{}, err
		}

		for _, entry := range entries {
			if entry == nil {
				continue
			}

			if entry.GitBranch != "" {
				resolved.Branch = entry.GitBranch
			}

			if entry.Type != "assistant" {
				continue
			}

			for _, block := range entry.Message.Content {
				if block.Type == "tool_use" && block.Name != "" {
					resolved.CurrentTool = block.Name
				}
			}
		}
	}

	lastActive := activeFiles[len(activeFiles)-1]
	uuid := strings.TrimSuffix(lastActive.name, filepath.Ext(lastActive.name))
	if uuid == "" {
		return resolved, nil
	}

	metaPath := filepath.Join(r.metaDir, slug, uuid+".json")
	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		// ファイル不在を含む読み取り失敗はゼロ値のまま継続（error なし）。
		return resolved, nil
	}

	var meta sessionMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		// パース失敗もゼロ値のまま継続（error なし）。
		return resolved, nil
	}

	resolved.FirstPrompt = meta.Title
	resolved.InputTokens = meta.InputTokens
	resolved.OutputTokens = meta.OutputTokens

	return resolved, nil
}
