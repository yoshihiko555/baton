# baton

[日本語](docs/README.ja.md)

AI coding session monitor for tmux. Track Claude Code, Codex, and Gemini sessions in real-time with a TUI dashboard.

![baton TUI](docs/assets/preview.png)

## Overview

baton discovers AI coding sessions running in tmux panes and displays their status in a dashboard with an `Attention` section and a stable session list. It works as a **pane indexer + status tracker + switcher** — it doesn't launch sessions, it finds and manages them.

Key design decisions:

- **Pane-centric**: Primary key is `TMUX_PANE`, not tmux session name. Multiple AI sessions in the same tmux session are tracked individually.
- **Non-intrusive**: Sessions are started manually; baton discovers them via `ps`, `tmux capture-pane`, and Claude JSONL/session-meta files.
- **Hook-free status**: State is derived from pane text, child process detection, and JSONL fallback — no Claude Code hooks required.

## Features

- Real-time status monitoring: `Thinking` / `ToolUse` / `Waiting` / `Idle` / `Error`
- `Attention` section for waiting sessions plus a stable `project / tool / PID` ordered session list with terminal preview pane
- Pane jump: select a session and switch to its tmux pane
- Multi-tool support: Claude Code, Codex CLI, Gemini CLI
- Incremental session filtering in TUI (`/`, text match, state filters like `waiting`, `!idle`)
- In-TUI approval actions for Claude Code (`a`/`d`/`A`/`D`)
- Approval prompt detection via `tmux capture-pane` screen scraping
- Codex idle/working detection via child process inspection
- Status bar JSON export for tmux status line integration
- Headless mode for background monitoring

## Requirements

- Go 1.25.5+ (as declared in `go.mod`)
- tmux (default terminal backend)

## Install

```bash
go install github.com/yoshihiko555/baton@latest
```

Install a fixed release version:

```bash
go install github.com/yoshihiko555/baton@v0.1.1
```

Or download prebuilt binaries from GitHub Releases (`baton_<tag>_<os>_<arch>.tar.gz` / `.zip`).

Release notes:

- [CHANGELOG.md](CHANGELOG.md)

If `baton` is not found after installation, ensure your Go bin directory is in `PATH`.

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
| `/` | Start session filter input |
| `w` | Jump to next Waiting session |
| `a` / `d` | Approve / deny (Waiting Claude on preview pane) |
| `A` / `D` | Approve+message / deny+message (Claude on preview pane) |
| `Esc` | Close submenu or clear active filter |
| `q` / `Ctrl+C` | Quit |

### Session filter

- Press `/` to enter filter mode and type to filter incrementally
- Match targets: session name, working directory/path, tool name
- State tokens: `waiting`, `idle`, `thinking`, `tool_use`, `working`, `error`
- Prefix `!` to exclude a state (example: `!idle`)

Examples:

- `waiting` → show Waiting sessions only
- `!idle` → show all sessions except Idle
- `codex !idle` → show non-idle Codex sessions

### TUI layout

The left pane has two layers:

| Section | Content |
|---------|---------|
| Attention | Summary counts for `Waiting / Working / Idle`, plus up to 5 `Waiting` sessions |
| Sessions | Stable list ordered by `project -> tool -> PID`, grouped by project |

State icons remain visible on each session row:

| State | Icon | Description |
|-------|------|-------------|
| WAITING | `!` | Approval prompt detected, needs user action |
| ERROR | `x` | Error state on the session row |
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
  state_icons:
    working: "🤔"
    waiting: "✋"
    idle: "~"
```

## How it works

```text
Ticker (2s)
  └── Scanner.Scan()
        ├── tmux list-panes -a          # discover panes and current commands
        ├── ps -t <tty>                 # find AI processes per pane
        └── pgrep -P <pid>              # inspect Codex child processes
  └── StateManager.UpdateFromScan()
        └── ResolveMultiple()           # build Claude base data from JSONL/session-meta
  └── StateManager.RefineToolUseState()
        └── tmux capture-pane           # refine Waiting / Idle from pane text
  └── ScanResultMsg → TUI Update()
  └── Exporter.Write()                  # /tmp/baton-status.json
```

### State detection by tool

| Tool | Working | Idle | Waiting |
|------|---------|------|---------|
| Claude Code | Pane text indicators (`✢` / `·` / `✶`) with JSONL fallback | Prompt line (`❯` + divider) with JSONL fallback | Screen: approval prompt patterns |
| Codex CLI | `pgrep -P`: child process exists | No child processes | Screen: numbered approval prompt |
| Gemini CLI | Process running (default) | Screen: `workspace (...) ... sandbox` | Screen: approval prompt patterns |

## Project Structure

```
.
├── main.go                          # Entry point (--no-tui / --once / --exit / --config)
├── internal/
│   ├── core/
│   │   ├── model.go                 # Domain types (SessionState, Session, Project)
│   │   ├── parser.go                # JSONL parser + IncrementalReader
│   │   ├── process.go               # Process detection (ps/pgrep)
│   │   ├── resolver.go              # Claude JSONL/session-meta resolver
│   │   ├── scanner.go               # DefaultScanner (pane scan + CurrentCommand filter)
│   │   ├── state.go                 # State aggregation manager
│   │   ├── exporter.go              # Atomic JSON export
│   │   ├── tmux_status.go           # tmux status-line formatter
│   │   └── watcher.go               # fsnotify file watcher (legacy/compat)
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
