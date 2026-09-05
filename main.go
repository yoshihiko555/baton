package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/hook"
	"github.com/yoshihiko555/baton/internal/terminal"
	"github.com/yoshihiko555/baton/internal/tui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		os.Exit(hook.RunHookCommand(os.Args[2:], os.Stdin, os.Getenv))
	}

	if err := run(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// フラグ解析
	configPath := flag.String("config", "", "path to config file")
	noTUI := flag.Bool("no-tui", false, "run without TUI")
	once := flag.Bool("once", false, "write status once and exit")
	outputFormat := flag.String("format", "json", "output format for --once: json or tmux")
	exitOnJump := flag.Bool("exit", false, "exit after pane jump")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(currentVersion())
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
		log.Printf("warning: could not create log directory for %q: %v (falling back to stderr)", cfg.LogFile, err)
	} else if file, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err != nil {
		log.Printf("warning: could not open log file %q: %v (falling back to stderr)", cfg.LogFile, err)
	} else {
		previousLogOutput := log.Writer()
		if !*noTUI && !*once {
			log.SetOutput(file)
		} else {
			log.SetOutput(io.MultiWriter(os.Stderr, file))
		}
		defer func() {
			log.SetOutput(previousLogOutput)
			_ = file.Close()
		}()
	}
	core.SetDebugLogging(cfg.LogLevel == "debug")

	term, err := initTerminal(cfg.Terminal)
	if err != nil {
		return fmt.Errorf("init terminal: %w", err)
	}
	if !term.IsAvailable() {
		log.Printf("terminal %q is not available", term.Name())
	}

	// v2 コンポーネント初期化
	processScanner := core.NewProcessScanner()
	scanner := core.NewDefaultScanner(term, processScanner)
	reader := core.NewIncrementalReader()
	resolver := core.NewStateResolver(reader, cfg.ClaudeProjectsDir, cfg.SessionMetaDir, cfg.ScanInterval)
	stateManager := core.NewStateManager(resolver)
	stateManager.SetProcessScanner(processScanner)
	// rescan は hook サーバーの PermissionRequest 受信時に即時スキャンをトリガーするチャネル。
	// hook が無効/未起動でも安全に select できるよう常に生成する（誰も送信しなければ単に発火しない）。
	rescan := make(chan struct{}, 1)
	exporter := core.NewExporter(cfg.StatusOutputPath, core.ExporterConfig{
		Format:    cfg.Statusbar.Format,
		ToolIcons: cfg.Statusbar.ToolIcons,
	})

	// シグナルハンドリング
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case <-ctx.Done():
			return
		case sig := <-sigCh:
			log.Printf("received signal: %s", sig)
			cancel()
		}
	}()

	// hook サーバー起動: 常駐起動（--once / --exit ではない）かつ hook.enabled のときのみ listen する。
	// listen に失敗しても baton の動作は継続する（従来の画面判定にフォールバック）。
	if cfg.Hook.Enabled && !*once && !*exitOnJump {
		hookStore := hook.NewStore(cfg.Hook.IdleCancelScans)
		hookServer, err := hook.Listen(hook.ServerOptions{
			SocketPath: cfg.Hook.SocketPath,
			Store:      hookStore,
			OnPermissionRequest: func() {
				select {
				case rescan <- struct{}{}:
				default:
				}
			},
		})
		switch {
		case err == nil:
			stateManager.SetHookStore(hookStore)
			defer func() {
				if closeErr := hookServer.Close(); closeErr != nil {
					log.Printf("hook server: close: %v", closeErr)
				}
			}()
		case errors.Is(err, hook.ErrAlreadyListening):
			log.Printf("hook server: %v (continuing without hook integration)", err)
		default:
			log.Printf("hook server: listen failed: %v (continuing without hook integration)", err)
		}
	}

	// doScan は TUI / ヘッドレス / ワンショット の全モードで共有するスキャン関数。
	doScan := func() error {
		result := scanner.Scan(ctx)
		if err := stateManager.UpdateFromScan(result); err != nil {
			return err
		}
		stateManager.ApplyHookStates()
		stateManager.RefineToolUseState(term)
		return nil
	}

	writeStatus := func() error {
		return exporter.Write(stateManager)
	}

	// ワンショットモード: 1 回だけスキャンして JSON を書き出して終了。
	if *once {
		if err := doScan(); err != nil {
			return err
		}

		switch strings.ToLower(strings.TrimSpace(*outputFormat)) {
		case "", "json":
			return writeStatus()
		case "tmux":
			fmt.Print(core.BuildTMUXStatusWithIcons(stateManager.Projects(), cfg.Statusbar.StateIcons))
			return nil
		default:
			return fmt.Errorf("unsupported --format %q: expected json or tmux", *outputFormat)
		}
	}

	// ヘッドレスモード: TUI なしで定期スキャン。
	if *noTUI {
		fmt.Printf("baton: headless mode (interval=%s, output=%s)\n", cfg.ScanInterval, cfg.StatusOutputPath)
		// 初回スキャンで起動確認メッセージを表示する
		if err := doScan(); err != nil {
			return err
		}
		if err := writeStatus(); err != nil {
			return err
		}
		summary := stateManager.Summary()
		fmt.Printf("baton: found %d sessions across %d projects\n", summary.TotalSessions, len(stateManager.Projects()))
		return runNoTUI(ctx, scanner, stateManager, term, cfg.ScanInterval, writeStatus, rescan)
	}

	// TUI モード: stateManager は StateUpdater と StateReader を両方実装する。
	model := tui.NewModel(scanner, stateManager, stateManager, term, cfg, *exitOnJump, rescan)
	program := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		<-ctx.Done()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	return nil
}

func currentVersion() string {
	return effectiveVersion(version, buildInfoVersion())
}

func buildInfoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

func effectiveVersion(embeddedVersion, moduleVersion string) string {
	if normalized := normalizeVersion(embeddedVersion); normalized != "" && normalized != "dev" {
		return normalized
	}
	if normalized := normalizeVersion(moduleVersion); normalized != "" {
		return normalized
	}
	if normalized := normalizeVersion(embeddedVersion); normalized != "" {
		return normalized
	}
	return "dev"
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(value, "v")
}

// runNoTUI はヘッドレスモードのイベントループ。
// ticker または hook rescan チャネルのいずれかでスキャンと JSON エクスポートを実行する。
// スキャンエラー・エクスポートエラーはログ出力して継続する。
func runNoTUI(
	ctx context.Context,
	scanner core.Scanner,
	sm core.StateUpdater,
	term terminal.Terminal,
	interval time.Duration,
	writeStatus func() error,
	rescan <-chan struct{},
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runScan := func() {
		result := scanner.Scan(ctx)
		if err := sm.UpdateFromScan(result); err != nil {
			log.Printf("scan error: %v", err)
			return
		}
		sm.ApplyHookStates()
		sm.RefineToolUseState(term)
		if err := writeStatus(); err != nil {
			log.Printf("export error: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runScan()
		case <-rescan:
			runScan()
		}
	}
}

func initTerminal(name string) (terminal.Terminal, error) {
	switch name {
	case "wezterm":
		return terminal.NewWezTerminal(), nil
	case "", "tmux":
		return terminal.NewTmuxTerminal(), nil
	default:
		return nil, fmt.Errorf("unsupported terminal %q", name)
	}
}
