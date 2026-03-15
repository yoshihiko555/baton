package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
	"github.com/yoshihiko555/baton/internal/tui"
)

const version = "0.2.0"

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// フラグ解析
	configPath := flag.String("config", "", "path to config file")
	noTUI := flag.Bool("no-tui", false, "run without TUI")
	once := flag.Bool("once", false, "write status JSON once and exit")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

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

	// doScan は TUI / ヘッドレス / ワンショット の全モードで共有するスキャン関数。
	doScan := func() error {
		result := scanner.Scan(ctx)
		if err := stateManager.UpdateFromScan(result); err != nil {
			return err
		}
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
		return writeStatus()
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
		return runNoTUI(ctx, scanner, stateManager, cfg.ScanInterval, writeStatus)
	}

	// TUI モード: stateManager は StateUpdater と StateReader を両方実装する。
	model := tui.NewModel(scanner, stateManager, stateManager, term, cfg)
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

// runNoTUI はヘッドレスモードのイベントループ。
// ticker ごとにスキャンと JSON エクスポートを実行する。
// スキャンエラー・エクスポートエラーはログ出力して継続する。
func runNoTUI(
	ctx context.Context,
	scanner core.Scanner,
	sm core.StateUpdater,
	interval time.Duration,
	writeStatus func() error,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result := scanner.Scan(ctx)
			if err := sm.UpdateFromScan(result); err != nil {
				log.Printf("scan error: %v", err)
				continue
			}
			if err := writeStatus(); err != nil {
				log.Printf("export error: %v", err)
			}
		}
	}
}

func initTerminal(name string) (terminal.Terminal, error) {
	switch name {
	case "", "wezterm":
		return terminal.NewWezTerminal(), nil
	default:
		return nil, fmt.Errorf("unsupported terminal %q", name)
	}
}
