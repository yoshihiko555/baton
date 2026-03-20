package core

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// aiCommands は AI ツールの CurrentCommand 名。
var aiCommands = []string{"claude", "codex", "gemini"}

// isAICommand は CurrentCommand が AI ツールかを判定する（大文字小文字無視）。
// "node" は nodeBasedAI ツール（Gemini 等）の可能性があるため通過させる。
func isAICommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, ai := range aiCommands {
		if strings.Contains(lower, ai) {
			return true
		}
	}
	// node ランタイムで動作する AI ツールの可能性がある
	return lower == "node"
}

// DefaultScanner は Terminal と ProcessScanner を組み合わせてプロセス検出を行う。
type DefaultScanner struct {
	terminal       terminal.Terminal
	processScanner *ProcessScanner
}

// NewDefaultScanner は DefaultScanner を生成する。
func NewDefaultScanner(term terminal.Terminal, ps *ProcessScanner) *DefaultScanner {
	return &DefaultScanner{
		terminal:       term,
		processScanner: ps,
	}
}

// Scan は全ペインをスキャンし、AI プロセスを検出して ScanResult を返す。
func (s *DefaultScanner) Scan(ctx context.Context) ScanResult {
	panes, err := s.terminal.ListPanes()
	if err != nil {
		return ScanResult{
			Err:       err,
			Timestamp: time.Now(),
		}
	}

	var allProcesses []DetectedProcess
	for _, pane := range panes {
		// CurrentCommand が AI ツールでなければ ps をスキップ（tmux 最適化）
		// WezTerm は CurrentCommand が空なのでフィルタされない
		if pane.CurrentCommand != "" && !isAICommand(pane.CurrentCommand) {
			continue
		}
		procs, err := s.processScanner.FindAIProcesses(ctx, pane.TTYName)
		if err != nil {
			log.Printf("warn: skip pane %s (tty=%s): %v", pane.ID, pane.TTYName, err)
			continue
		}
		for i := range procs {
			procs[i].PaneID = pane.ID
			procs[i].CWD = pane.WorkingDir
		}
		allProcesses = append(allProcesses, procs...)
	}

	return ScanResult{
		Processes: allProcesses,
		Panes:     panes,
		Timestamp: time.Now(),
	}
}
