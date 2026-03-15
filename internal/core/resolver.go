package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cwdToSlug は CWD パスを Claude Code が使用するスラグ形式に変換する。
// "/" と "." を "-" に置換する（例: /Users/foo/github.com/bar → -Users-foo-github-com-bar）。
func cwdToSlug(cwd string) string {
	s := strings.ReplaceAll(cwd, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

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

// jsonlFile は ModTime 順ソート用の JSONL ファイル情報。
type jsonlFile struct {
	name string
	path string
	mod  time.Time
}

// ResolveMultiple は同一 CWD に対して、ModTime が新しい順に最大 count 個の JSONL から
// 状態を解決して返す。返却スライスは重要度順（Waiting > Error > Thinking > ToolUse > Idle）。
// PID との1対1対応はできないが、プロジェクト内の状態分布として利用する。
func (r *StateResolver) ResolveMultiple(cwd string, count int) ([]ResolvedSession, error) {
	if r == nil || r.reader == nil || count <= 0 {
		return nil, nil
	}

	slug := cwdToSlug(cwd)
	if slug == "" {
		return nil, nil
	}

	files, err := r.listJSONL(slug)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil
	}

	// ModTime 降順でソートし、上位 count 個を取得する
	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})
	if len(files) > count {
		files = files[:count]
	}

	results := make([]ResolvedSession, 0, len(files))
	for _, f := range files {
		resolved := r.resolveOneJSONL(f)
		results = append(results, resolved)
	}

	// 重要度順にソートする（Waiting/Error が先頭に来る）
	sort.Slice(results, func(i, j int) bool {
		return statePriority[results[i].State] < statePriority[results[j].State]
	})

	return results, nil
}

// ResolveState は単一プロセス向けの状態解決（ResolveMultiple(cwd, 1) の簡易版）。
func (r *StateResolver) ResolveState(proc DetectedProcess) (ResolvedSession, error) {
	results, err := r.ResolveMultiple(proc.CWD, 1)
	if err != nil {
		return ResolvedSession{State: Thinking}, err
	}
	if len(results) == 0 {
		return ResolvedSession{State: Thinking}, nil
	}
	return results[0], nil
}

// listJSONL は指定スラグディレクトリ内の JSONL ファイル一覧を返す。
func (r *StateResolver) listJSONL(slug string) ([]jsonlFile, error) {
	dirPath := filepath.Join(r.projectDir, slug)
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []jsonlFile
	for _, de := range dirEntries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".jsonl" {
			continue
		}
		info, err := de.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, jsonlFile{
			name: de.Name(),
			path: filepath.Join(dirPath, de.Name()),
			mod:  info.ModTime(),
		})
	}
	return files, nil
}

// resolveOneJSONL は1つの JSONL ファイルから状態を解決する。
func (r *StateResolver) resolveOneJSONL(f jsonlFile) ResolvedSession {
	resolved := ResolvedSession{State: Thinking}

	entry, err := r.reader.ReadLastEntry(f.path)
	if err != nil {
		return resolved
	}
	if entry != nil {
		resolved.State = classifyEntry(entry)
	}

	// 増分読み取りで Branch / CurrentTool を抽出する
	entries, err := r.reader.ReadNew(f.path)
	if err != nil {
		return resolved
	}

	for _, e := range entries {
		if e == nil {
			continue
		}
		if e.GitBranch != "" {
			resolved.Branch = e.GitBranch
		}
		if e.Type != "assistant" {
			continue
		}
		for _, block := range e.Message.Content {
			if block.Type == "tool_use" && block.Name != "" {
				resolved.CurrentTool = block.Name
			}
		}
	}

	uuid := strings.TrimSuffix(f.name, filepath.Ext(f.name))
	if uuid == "" {
		return resolved
	}

	slug := filepath.Base(filepath.Dir(f.path))
	metaPath := filepath.Join(r.metaDir, slug, uuid+".json")
	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		return resolved
	}

	var meta sessionMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return resolved
	}

	resolved.FirstPrompt = meta.Title
	resolved.InputTokens = meta.InputTokens
	resolved.OutputTokens = meta.OutputTokens

	return resolved
}
