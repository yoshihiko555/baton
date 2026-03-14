package core

import (
	"context"
	"log"
	"time"

	"github.com/yoshihiko555/baton/internal/terminal"
)

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
		procs, err := s.processScanner.FindAIProcesses(ctx, pane.TTYName)
		if err != nil {
			log.Printf("warn: skip pane %d (tty=%s): %v", pane.ID, pane.TTYName, err)
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
