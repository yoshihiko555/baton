package core

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// toolTypeMap は COMM 名から ToolType へのマッピング。完全一致のみ有効。
var toolTypeMap = map[string]ToolType{
	"claude": ToolClaude,
	"codex":  ToolCodex,
	"gemini": ToolGemini,
}

// ProcessScanner は特定の TTY に紐づく AI プロセスを検出する。
type ProcessScanner struct {
	execFn func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewProcessScanner はデフォルトの ProcessScanner を返す。
func NewProcessScanner() *ProcessScanner {
	return &ProcessScanner{
		execFn: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
	}
}

// NewProcessScannerWithExec はテスト用に execFn を差し替えたインスタンスを返す。
func NewProcessScannerWithExec(execFn func(ctx context.Context, name string, args ...string) ([]byte, error)) *ProcessScanner {
	return &ProcessScanner{execFn: execFn}
}

// normalizeTTY は WezTerm が返す TTY 名を ps コマンド向けに正規化する。
func normalizeTTY(tty string) string {
	return strings.TrimPrefix(tty, "/dev/")
}

// parse は ps コマンドの出力を解析して DetectedProcess の一覧に変換する。
func (s *ProcessScanner) parse(output []byte) []DetectedProcess {
	lines := strings.Split(string(output), "\n")
	var results []DetectedProcess

	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		toolType, ok := toolTypeMap[fields[2]]
		if !ok {
			continue
		}
		results = append(results, DetectedProcess{
			PID:      pid,
			Name:     fields[2],
			ToolType: toolType,
		})
	}
	return results
}

// FindAIProcesses は指定 TTY で動作中の AI プロセス一覧を返す。
func (s *ProcessScanner) FindAIProcesses(ctx context.Context, tty string) ([]DetectedProcess, error) {
	normalizedTTY := normalizeTTY(tty)
	output, err := s.execFn(ctx, "ps", "-t", normalizedTTY, "-o", "pid,ppid,comm")
	if err != nil {
		return nil, err
	}
	return s.parse(output), nil
}
