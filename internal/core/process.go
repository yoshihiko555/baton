package core

import (
	"context"
	"errors"
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

// backgroundCommands は Codex/Claude が常駐させる子プロセス名。
// これらは「作業中」の判定から除外する。
var backgroundCommands = map[string]bool{
	"node":         true, // MCP サーバー
	"npm":          true, // MCP サーバー起動
	"caffeinate":   true, // スリープ防止
	"gopls":        true, // LSP サーバー
	"claude-tmux":  true, // tmux 統合プロセス
}

// backgroundPrefixes は常駐子プロセスのベース名プレフィックス。
// MCP サーバー等のバイナリ名が環境によって異なるため、プレフィックスで除外する。
var backgroundPrefixes = []string{
	"mcp-server",  // MCP サーバー各種
	"mcp-proxy",   // MCP プロキシ
}

// isBackgroundProcess はプロセス名が常駐プロセスかを判定する。
func isBackgroundProcess(base string) bool {
	if backgroundCommands[base] {
		return true
	}
	for _, prefix := range backgroundPrefixes {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

// HasChildProcesses は指定 PID に作業用の子プロセスがあるかを返す。
// MCP サーバー等の常駐プロセスは除外し、sandbox 実行等の実作業プロセスのみをカウントする。
//
// 手順:
//  1. pgrep -P で子プロセスの PID 一覧を取得
//  2. 子プロセスがある場合、ps -o comm= で COMM 名を取得
//  3. backgroundCommands に含まれないプロセスがあれば true を返す
func (s *ProcessScanner) HasChildProcesses(ctx context.Context, pid int) (bool, error) {
	// Step 1: 子プロセス PID 一覧を取得
	pgrepOut, err := s.execFn(ctx, "pgrep", "-P", strconv.Itoa(pid))
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}

	// 子 PID を収集
	var childPIDs []string
	for _, line := range strings.Split(string(pgrepOut), "\n") {
		p := strings.TrimSpace(line)
		if p != "" {
			childPIDs = append(childPIDs, p)
		}
	}
	if len(childPIDs) == 0 {
		return false, nil
	}

	// Step 2: 各子プロセスの COMM 名を取得（macOS 互換: -p pid1,pid2）
	psOut, err := s.execFn(ctx, "ps", "-p", strings.Join(childPIDs, ","), "-o", "comm=")
	if err != nil {
		// ps 失敗時はフォールバック: 子プロセスがある = Thinking
		return true, nil
	}

	// Step 3: backgroundCommands 以外のプロセスがあるか判定
	for _, line := range strings.Split(string(psOut), "\n") {
		comm := strings.TrimSpace(line)
		if comm == "" {
			continue
		}
		// パス付き COMM（例: /Applications/Pencil.app/...）はベース名で判定
		base := comm
		if idx := strings.LastIndex(comm, "/"); idx >= 0 {
			base = comm[idx+1:]
		}
		if !isBackgroundProcess(base) {
			return true, nil
		}
	}
	return false, nil
}
