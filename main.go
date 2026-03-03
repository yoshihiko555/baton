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

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run() error {
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

	watcher, err := core.NewWatcher(cfg.WatchPath)
	if err != nil {
		return fmt.Errorf("init watcher: %w", err)
	}

	stateManager := core.NewStateManager(watcher)

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

	writeStatus := func() error {
		return core.WriteStatusJSON(stateManager.GetStatus(), cfg.StatusOutputPath)
	}

	defer func() {
		watcher.Stop()
		if err := writeStatus(); err != nil {
			log.Printf("final status write failed: %v", err)
		}
	}()

	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}

	if err := stateManager.InitialScan(); err != nil {
		log.Printf("initial scan warning: %v", err)
	}

	if *once {
		return nil
	}

	if *noTUI {
		return runNoTUI(ctx, watcher, stateManager, cfg.RefreshInterval, writeStatus)
	}

	model := tui.NewModel(stateManager, stateManager, watcher, term, cfg)
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

func runNoTUI(
	ctx context.Context,
	watcher *core.Watcher,
	stateManager *core.StateManager,
	interval time.Duration,
	writeStatus func() error,
) error {
	if interval <= 0 {
		interval = time.Second
	}

	if err := writeStatus(); err != nil {
		return fmt.Errorf("write status: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events():
			if !ok {
				return nil
			}
			if err := stateManager.HandleEvent(event); err != nil {
				log.Printf("handle event: %v", err)
			}
		case <-ticker.C:
			if err := writeStatus(); err != nil {
				return fmt.Errorf("write status: %w", err)
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
