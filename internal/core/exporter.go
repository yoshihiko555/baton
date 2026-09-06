package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

// Exporter は StatusOutput をアトミックに JSON ファイルへ書き出す。
type Exporter struct {
	destPath     string
	cfg          ExporterConfig
	hookListener bool
}

// ExporterConfig は Exporter の動作を制御する設定。
type ExporterConfig struct {
	// Format は Go template 文字列。空の場合はデフォルト "{{.Active}}/{{.TotalSessions}}" を使用する。
	Format string
	// ToolIcons はツール名からアイコン文字列へのマッピング（将来拡張用）。
	ToolIcons map[string]string
}

// NewExporter は destPath への書き出しを行う Exporter を生成する。
func NewExporter(destPath string, cfg ExporterConfig) *Exporter {
	return &Exporter{destPath: destPath, cfg: cfg}
}

// SetHookListener marks whether this instance is actively listening on the hook socket.
// Only the resident instance that successfully bound the hook socket should set this to true.
func (e *Exporter) SetHookListener(v bool) {
	e.hookListener = v
}

// Write は StateReader から状態を読み取り、DTO に変換してアトミックに書き出す。
func (e *Exporter) Write(sr StateReader) error {
	status := BuildStatusOutput(sr, e.hookListener)
	// FormattedStatus は Exporter に設定された template で上書きする
	// （BuildStatusOutput のデフォルト書式ではなく、Statusbar.Format 設定を反映するため）。
	status.FormattedStatus = FormatStatus(e.cfg.Format, status.Summary)

	return writeAtomicJSON(status, e.destPath)
}

// BuildStatusOutput は StateReader から StatusOutput DTO を組み立てる。
// FormattedStatus はデフォルト書式（"{{.Active}}/{{.TotalSessions}}" 相当）で設定される。
// Exporter.Write はこれを呼び出した後、自身に設定された template で FormattedStatus を上書きする。
func BuildStatusOutput(sr StateReader, hookListener bool) StatusOutput {
	return BuildStatusOutputFromProjects(sr.Projects(), hookListener)
}

// BuildStatusOutputFromProjects は []Project から直接 StatusOutput DTO を組み立てる。
// Summary は calcSummary(projects) で算出するため、呼び出し側が list --waiting のように
// フィルタ済みの []Project を渡せば、フィルタ後の集計を calcSummary と同じルールで
// 再現できる（文字列化した DTO の再集計を避けるための export）。
// FormattedStatus はデフォルト書式（"{{.Active}}/{{.TotalSessions}}" 相当）で設定される。
func BuildStatusOutputFromProjects(projects []Project, hookListener bool) StatusOutput {
	outputs := make([]ProjectOutput, 0, len(projects))
	for _, p := range projects {
		outputs = append(outputs, toProjectOutput(p))
	}

	summary := toSummaryOutput(calcSummary(projects))

	return StatusOutput{
		Version:         2,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		HookListener:    hookListener,
		Projects:        outputs,
		Summary:         summary,
		FormattedStatus: fmt.Sprintf("%d/%d", summary.Active, summary.TotalSessions),
	}
}

// ReadStatus reads and decodes a status JSON file previously written by Exporter.Write.
// If the file does not exist, the returned error wraps os.ErrNotExist (use errors.Is to check).
func ReadStatus(path string) (StatusOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StatusOutput{}, fmt.Errorf("status file %q does not exist: %w", path, err)
		}
		return StatusOutput{}, fmt.Errorf("read status %q: %w", path, err)
	}

	var status StatusOutput
	if err := json.Unmarshal(data, &status); err != nil {
		return StatusOutput{}, fmt.Errorf("decode status %q: %w", path, err)
	}
	return status, nil
}

// toProjectOutput は Project ドメイン型を ProjectOutput DTO に変換する。
func toProjectOutput(p Project) ProjectOutput {
	sessions := make([]SessionOutput, 0, len(p.Sessions))
	for _, s := range p.Sessions {
		if s != nil {
			sessions = append(sessions, toSessionOutput(*s))
		}
	}
	return ProjectOutput{
		Name:      p.Name,
		Path:      p.Path,
		Workspace: p.Workspace,
		Sessions:  sessions,
	}
}

// toSessionOutput は Session ドメイン型を SessionOutput DTO に変換する。
// ゼロ値フィールドは出力 DTO に含めない。
func toSessionOutput(s Session) SessionOutput {
	out := SessionOutput{
		PID:        s.PID,
		Tool:       s.Tool.String(),
		State:      s.State.String(),
		WorkingDir: s.WorkingDir,
	}
	// PaneID は string 型。空でなく、かつ曖昧でない場合のみ出力する。
	if s.PaneID != "" && !s.Ambiguous {
		out.PaneID = s.PaneID
	}
	if s.Branch != "" {
		out.Branch = s.Branch
	}
	if s.CurrentTool != "" {
		out.CurrentTool = s.CurrentTool
	}
	if s.FirstPrompt != "" {
		out.FirstPrompt = s.FirstPrompt
	}
	if s.InputTokens != 0 {
		out.InputTokens = s.InputTokens
	}
	if s.OutputTokens != 0 {
		out.OutputTokens = s.OutputTokens
	}
	if s.SessionID != "" {
		out.SessionID = s.SessionID
	}
	if s.TranscriptPath != "" {
		out.TranscriptPath = s.TranscriptPath
	}
	if s.StateSource != "" {
		out.StateSource = s.StateSource
	}
	if s.Via != "" {
		out.Via = s.Via
	}
	return out
}

// toSummaryOutput は Summary ドメイン型を SummaryOutput DTO に変換する。
func toSummaryOutput(s Summary) SummaryOutput {
	return SummaryOutput{
		TotalSessions: s.TotalSessions,
		Active:        s.Active,
		Waiting:       s.Waiting,
		ByTool:        s.ByTool,
	}
}

// FormatStatus は Go template（tmplStr）を使って summary から FormattedStatus 文字列を
// 生成する。tmplStr が空、またはパース・実行に失敗した場合はデフォルト書式
// "{{.Active}}/{{.TotalSessions}}" 相当のフォールバック文字列を返す。
// Exporter.Write（Statusbar.Format 反映）と CLI サブコマンド（list --format json の
// formatted_status 反映）の両方から共有される。
func FormatStatus(tmplStr string, summary SummaryOutput) string {
	if tmplStr == "" {
		tmplStr = "{{.Active}}/{{.TotalSessions}}"
	}
	tmpl, err := template.New("status").Parse(tmplStr)
	if err != nil {
		return fmt.Sprintf("%d/%d", summary.Active, summary.TotalSessions)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, summary); err != nil {
		return fmt.Sprintf("%d/%d", summary.Active, summary.TotalSessions)
	}
	return buf.String()
}

// writeAtomicJSON は status を整形 JSON で destPath にアトミックに書き出す。
// 一時ファイルへ書いてから rename で置換し、破損中間状態を避ける。
func writeAtomicJSON(status StatusOutput, destPath string) error {
	dir := filepath.Dir(destPath)
	pattern := filepath.Base(destPath) + ".tmp-*"

	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(tmpFile)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(status); err != nil {
		_ = tmpFile.Close()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	removeTmp = false

	// 出力ファイルは読み書き権限を所有者のみに限定する。
	if err := os.Chmod(destPath, 0600); err != nil {
		return err
	}

	return nil
}
