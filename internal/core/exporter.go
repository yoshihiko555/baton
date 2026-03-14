package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

// Exporter は StatusOutput をアトミックに JSON ファイルへ書き出す。
type Exporter struct {
	destPath string
	cfg      ExporterConfig
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

// Write は StateReader から状態を読み取り、DTO に変換してアトミックに書き出す。
func (e *Exporter) Write(sr StateReader) error {
	// プロジェクト一覧を変換する。
	rawProjects := sr.Projects()
	projects := make([]ProjectOutput, 0, len(rawProjects))
	for _, p := range rawProjects {
		projects = append(projects, toProjectOutput(p))
	}

	// サマリーを変換する。
	summary := toSummaryOutput(sr.Summary())

	// FormattedStatus を生成する。
	formatted := e.formatStatus(summary)

	// StatusOutput を組み立てる。
	status := StatusOutput{
		Version:         2,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Projects:        projects,
		Summary:         summary,
		FormattedStatus: formatted,
	}

	return writeAtomicJSON(status, e.destPath)
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

// formatStatus は Go template を使って FormattedStatus 文字列を生成する。
// template パースまたは実行に失敗した場合はフォールバック文字列を返す。
func (e *Exporter) formatStatus(summary SummaryOutput) string {
	tmplStr := e.cfg.Format
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
