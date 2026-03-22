# baton

[日本語](docs/README.ja.md)

AI coding session monitor for tmux. Track Claude Code, Codex, and Gemini sessions in real-time with a TUI dashboard.

![baton TUI](docs/assets/preview.png)

## Overview

baton discovers AI coding sessions running in tmux panes and displays their status in a grouped dashboard. It works as a **pane indexer + status tracker + switcher** — it doesn't launch sessions, it finds and manages them.

Key design decisions:

- **Pane-centric**: Primary key is `TMUX_PANE`, not tmux session name. Multiple AI sessions in the same tmux session are tracked individually.
- **Non-intrusive**: Sessions are started manually; baton discovers them via `ps` + JSONL log parsing.
- **Hook-free status**: State is derived from JSONL logs, child process detection, and screen scraping — no Claude Code hooks required.

## Features

- Real-time status monitoring: `Thinking` / `ToolUse` / `Waiting` / `Idle` / `Error`
- State-grouped session list with terminal preview pane
- Pane jump: select a session and switch to its tmux pane
- Multi-tool support: Claude Code, Codex CLI, Gemini CLI
- Approval prompt detection via `tmux capture-pane` screen scraping
- Codex idle/working detection via child process inspection
- Status bar JSON export for tmux status line integration
- Headless mode for background monitoring

## Requirements

- Go 1.22+
- tmux (default terminal backend)

## Install

```bash
go install github.com/yoshihiko555/baton@latest
```

Install a fixed release version:

```bash
go install github.com/yoshihiko555/baton@v0.1.0
```

Or download prebuilt binaries from GitHub Releases (`baton_<tag>_<os>_<arch>.tar.gz` / `.zip`).

Or build from source:

```bash
git clone https://github.com/yoshihiko555/baton.git
cd baton
go build -o baton .

# macOS: codesign is required after copying the binary
cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

## Usage

```bash
# TUI dashboard (stays open after pane jump)
baton

# TUI dashboard (exit after pane jump, useful for tmux popup)
baton --exit

# Headless mode (JSON export only, for background monitoring)
baton --no-tui

# One-shot (scan once, write status JSON, exit)
baton --once

# Specify config file
baton --config ~/.config/baton/config.yaml

# Version
baton --version
```

### tmux popup integration

```bash
# Add to tmux.conf for quick access
bind b display-popup -E -w 80% -h 80% "baton --exit"

# Or without --exit to keep browsing after jump
bind b display-popup -E -w 80% -h 80% "baton"
```

### TUI keybindings

| Key | Action |
|-----|--------|
| `j` / `Down` | Move cursor down |
| `k` / `Up` | Move cursor up |
| `Enter` | Jump to selected pane |
| `Tab` | Switch focus between session list and preview |
| `Esc` | Close submenu (for ambiguous sessions) |
| `q` / `Ctrl+C` | Quit |

### State groups

Sessions are grouped by status in the following order:

| Group | Icon | Description |
|-------|------|-------------|
| WAITING | `!` | Approval prompt detected, needs user action |
| ERROR | `x` | Error state |
| WORKING | `*` | Thinking or executing tools |
| IDLE | `~` | Waiting for user input |

## Configuration

Optional config file: `~/.config/baton/config.yaml`

```yaml
# Scan interval (default: 2s)
scan_interval: "2s"

# Claude Code projects directory
claude_projects_dir: "~/.claude/projects"

# Status JSON output path
status_output_path: "/tmp/baton-status.json"

# Terminal backend: "tmux" (default) or "wezterm" (legacy)
terminal: "tmux"

# Status bar format (Go template)
statusbar:
  format: "{{.Active}} active / {{.TotalSessions}} total{{if .Waiting}} | {{.Waiting}} waiting{{end}}"
  tool_icons:
    claude: ""
    codex: ""
    gemini: ""
    default: "●"
```

## How it works

```
Ticker (2s)
  └── Scanner.Scan()
        ├── tmux list-panes -a          # discover all panes
        ├── ps + pgrep                  # find AI processes per pane
        └── JSONL log parsing           # determine session state
  └── StateManager.UpdateFromScan()
        ├── ResolveMultiple()           # match processes to JSONL logs
        └── RefineToolUseState()        # screen scrape for approval prompts
  └── ScanResultMsg → TUI Update()
  └── Exporter.Write()                  # /tmp/baton-status.json
```

### State detection by tool

| Tool | Working | Idle | Waiting |
|------|---------|------|---------|
| Claude Code | JSONL `assistant` entries | JSONL `end_turn` | Screen: approval prompt patterns |
| Codex CLI | `pgrep -P`: child process exists | No child processes | Screen: approval prompt patterns |
| Gemini CLI | Process running | — | — |

## Project Structure

```
.
├── main.go                          # Entry point (--no-tui / --once / --exit / --config)
├── internal/
│   ├── core/
│   │   ├── model.go                 # Domain types (SessionState, Session, Project)
│   │   ├── parser.go                # JSONL parser + IncrementalReader
│   │   ├── process.go               # Process detection (ps/pgrep)
│   │   ├── scanner.go               # DefaultScanner (pane scan + CurrentCommand filter)
│   │   ├── watcher.go               # fsnotify file watcher + debounce
│   │   ├── state.go                 # State aggregation manager
│   │   └── exporter.go              # Atomic JSON export
│   ├── terminal/
│   │   ├── terminal.go              # Terminal interface
│   │   ├── tmux.go                  # tmux implementation (default)
│   │   └── wezterm.go               # WezTerm implementation (legacy)
│   ├── config/
│   │   └── config.go                # YAML config loader
│   └── tui/
│       ├── model.go                 # bubbletea Model + Init
│       ├── update.go                # Key input, event handling, pane jump
│       └── view.go                  # Session list + preview pane rendering
└── wezterm/
    └── baton-status.lua             # WezTerm status bar plugin (legacy)
```

## Development

```bash
# Run tests
go test ./... -v

# Static analysis
go vet ./...

# Build and install locally (macOS)
go build -o baton . && cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

## License

MIT
