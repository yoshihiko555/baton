# Architecture Overview

## 概要

baton は Claude Code のセッション状態をリアルタイム監視し、TUI ダッシュボードおよび WezTerm ステータスバーに表示する Go アプリケーションである。

## システム構成

```
┌─────────────────────────────────────────────────────────┐
│  Claude Code Sessions                                   │
│  ~/.claude/projects/*/sessions/*/*.jsonl                 │
└──────────────────────┬──────────────────────────────────┘
                       │ fsnotify
                       ▼
┌──────────────────────────────────────────────────────────┐
│  internal/core                                           │
│                                                          │
│  ┌──────────┐    ┌────────────┐    ┌──────────────────┐  │
│  │ Watcher  │───▶│ Channel    │───▶│ StateManager     │  │
│  │(fsnotify)│    │ (Events()) │    │(状態集約)         │  │
│  └──────────┘    └────────────┘    └────────┬─────────┘  │
│                                             │            │
│  ┌──────────────┐    ┌──────────┐           │            │
│  │ Parser       │    │ Exporter │◀──────────┘            │
│  │(JSONL解析)   │    │(JSON出力)│                        │
│  └──────────────┘    └──────────┘                        │
└──────────────────────────┬───────────────────────────────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
    ┌──────────────┐ ┌──────────┐ ┌──────────────────┐
    │ TUI Mode     │ │ No-TUI   │ │ Once Mode        │
    │ (bubbletea)  │ │ (daemon) │ │ (one-shot)       │
    └──────┬───────┘ └────┬─────┘ └──────────────────┘
           │              │
           ▼              ▼
    ┌──────────────┐ ┌──────────────────────────────┐
    │ Terminal     │ │ /tmp/baton-status.json        │
    │ (WezTerm)   │ │                               │
    └──────────────┘ └──────────────┬───────────────┘
                                    │
                                    ▼
                          ┌──────────────────┐
                          │ WezTerm Plugin   │
                          │ (baton-status.lua)│
                          └──────────────────┘
```

## コンポーネント

### core パッケージ (`internal/core/`)

| コンポーネント | ファイル | 責務 |
|--------------|---------|------|
| **Watcher** | `watcher.go` | fsnotify でセッションディレクトリを再帰監視。デバウンス付き |
| **Parser** | `parser.go` | JSONL パース + `IncrementalReader` による差分読み取り |
| **StateManager** | `state.go` | プロジェクト・セッション単位の状態集約 |
| **Exporter** | `exporter.go` | ステータス JSON のアトミック書き出し（temp + rename） |
| **Model** | `model.go` | ドメイン型定義（`SessionState`, `Session`, `Project`, `StatusOutput`） |

### TUI パッケージ (`internal/tui/`)

Elm Architecture（bubbletea）に基づく TUI ダッシュボード。

| ファイル | 責務 |
|---------|------|
| `model.go` | bubbletea Model 定義、`Init()`、`tea.Sub` による channel 購読 |
| `update.go` | キー入力・ウィンドウリサイズ・ウォッチイベントのハンドリング |
| `view.go` | 左右ペイン + ステータスバーの描画 |

### Terminal パッケージ (`internal/terminal/`)

ターミナルエミュレータとの連携を抽象化。

- `Terminal` インターフェース: `Name()`, `IsAvailable()`, `FocusPane()`
- `WezTerminal`: WezTerm CLI を使用した実装

### Config パッケージ (`internal/config/`)

YAML 設定ファイルの読み込みとデフォルト値の解決。

## データフロー

### TUI モード

```
fsnotify event
  → Watcher.Events() channel
  → tea.Sub で購読
  → WatchEventMsg として Update() に配信
  → StateManager.HandleEvent() で状態更新
  → View() で再描画
```

### ヘッドレスモード（`--no-tui`）

```
fsnotify event
  → Watcher.Events() channel
  → select で直接受信
  → StateManager.HandleEvent() で状態更新
  → ticker で定期的に WriteStatusJSON() 実行
  → /tmp/baton-status.json に書き出し
  → WezTerm プラグインが読み取り・表示
```

## セッション状態

| State | 意味 |
|-------|------|
| `idle` | 入力待ち |
| `thinking` | LLM が応答生成中 |
| `tool_use` | ツール実行中 |
| `error` | エラー発生 |

## 設計原則

- **関心の分離**: 集約（state）と出力（exporter）を分離
- **インターフェース抽象化**: Terminal はインターフェース経由でアクセス（テスト容易性）
- **Elm Architecture**: TUI は bubbletea の Model-Update-View パターンに従う
- **差分処理**: IncrementalReader で変更部分のみパース（パフォーマンス）
- **アトミック書き込み**: temp + rename パターンで JSON 破損を防止
