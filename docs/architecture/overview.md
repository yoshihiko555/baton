# Architecture Overview

**バージョン**: v2
**最終更新**: 2026-03-29
**要件定義書**: `docs/requirements/requirements-v2.md`

> **v1 からの変更**: セッション発見の source of truth を「JSONL ファイル存在」から「OS プロセス存在」に変更。
> v1 の設計は `git log` で参照可能。

## 概要

baton は AI コーディングエージェント（Claude Code, Codex CLI, Gemini CLI）のセッション状態をリアルタイム監視し、TUI ダッシュボードおよび tmux (デフォルト) / WezTerm ステータスバーに表示する Go アプリケーションである。

## システム構成

```text
┌─────────────────────────────────────────────────────────────┐
│  tmux / WezTerm Panes (AI セッション実行環境)                 │
│  各ペインで claude / codex / gemini が直接起動される           │
└──────────────────────┬──────────────────────────────────────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
┌──────────────────┐  ┌──────────────────┐
│ tmux list-panes  │  │ ps -t <tty>      │
│ (ペイン情報)      │  │ (プロセス検出)    │
│ TTY, CWD, PaneID │  │ PID, COMM, ARGS  │
└────────┬─────────┘  └────────┬─────────┘
         │                     │
         └──────────┬──────────┘
                    ▼
┌──────────────────────────────────────────────────────────────┐
│  internal/core                                               │
│                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────┐   │
│  │ Scanner      │───▶│ StateManager │───▶│ Exporter      │   │
│  │(ペイン+プロセ│    │(スナップショッ│    │(JSON出力)     │   │
│  │ ス統合)      │    │ ト照合)      │    │               │   │
│  └──────┬───────┘    └──────┬───────┘    └───────────────┘   │
│         │                   │                                │
│  ┌──────┴───────┐    ┌──────┴───────┐                        │
│  │ Process      │    │ StateResolver│                        │
│  │ Scanner      │    │(JSONL解析)   │                        │
│  │(ps パース)   │    │+ PaneText解析 │                        │
│  └──────────────┘    └──────────────┘                        │
└──────────────────────────┬───────────────────────────────────┘
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
    │ (tmux/WezTerm)│ │ (version: 2 スキーマ)         │
    └──────────────┘ └──────────────┬───────────────┘
                                    │
                                    ▼
                          ┌──────────────────┐
                          │ Status Line      │
                          │ (tmux / lua)     │
                          └──────────────────┘
```

## コンポーネント

### core パッケージ (`internal/core/`)

| コンポーネント | 責務 |
|--------------|------|
| **Scanner** | tmux (または WezTerm) ペイン一覧取得 + ペインごとのプロセス検出を統合。`CurrentCommand` フィルタによる高速化 |
| **ProcessScanner** | `ps -t <tty> -o pid,ppid,comm,args` の出力をパースし、COMM / ARGS名で AI プロセスを同定。また Codex の子プロセス有無を検査する |
| **StateResolver** | Claude Code セッションの JSONL を解析し、基礎データを構築 |
| **StateManager** | スキャン結果からスナップショット照合。プロセス、JSONL、ペインテキスト（`tmux capture-pane`）を組み合わせて最終的な状態（Idle/Thinking/ToolUse/Waiting/Error）を判定 |
| **Exporter** | ステータス JSON のアトミック書き出し（temp + rename） |
| **Model** | ドメイン型定義（`SessionState`, `ToolType`, `Session`, `Project`, `DetectedProcess`, `ScanResult`） |

### TUI パッケージ (`internal/tui/`)

Elm Architecture（bubbletea）に基づく TUI ダッシュボード。

| ファイル | 責務 |
|---------|------|
| `model.go` | bubbletea Model 定義、`Init()`、ticker による定期スキャン |
| `update.go` | キー入力・ウィンドウリサイズ・スキャン結果のハンドリング |
| `view.go` | 左右ペイン（プロジェクト一覧 + セッション詳細）+ ステータスバーの描画 |

### Terminal パッケージ (`internal/terminal/`)

ターミナルエミュレータとの連携を抽象化。

- `Terminal` インターフェース: `Name()`, `IsAvailable()`, `ListPanes()`, `FocusPane()`, `GetPaneText()`, `SendKeys()`
- `TmuxTerminal`: tmux CLI を使用した実装（デフォルト）
- `WezTerminal`: WezTerm CLI を使用した実装（レガシー）
- `Pane` 構造体: `ID`, `SessionName`, `WorkingDir`, `TTYName`, `CurrentCommand` など

### Config パッケージ (`internal/config/`)

YAML 設定ファイルの読み込みとデフォルト値の解決。

## データフロー

### スキャンサイクル（全モード共通）

```text
ticker (2-3秒間隔)
  → Scanner.Scan()
    → tmux list-panes -a              → ペイン一覧 (TTY, CWD, PaneID, Cmd)
    → per pane: ps -t <tty>           → AI プロセス検出 (PID, COMM, ARGS)
  → StateManager.UpdateFromScan()
    → Claude セッション: JSONL tail   → 基礎状態判定
    → Codex セッション: 子プロセス検査 → Idle / Thinking 判定
  → StateManager.RefineToolUseState()
    → per pane: capture-pane          → ペインテキストから詳細状態精緻化（Waiting判定など）
  → Exporter / TUI に反映
```

### TUI モード

```text
ticker
  → ScanResultMsg として Update() に配信
  → StateManager.UpdateFromScan() で状態更新
  → View() で再描画
```

### ヘッドレスモード（`--no-tui`）

```text
ticker
  → Scanner.Scan()
  → StateManager.UpdateFromScan() で状態更新
  → StateManager.RefineToolUseState() で状態精緻化
  → Exporter.Write() で /tmp/baton-status.json に書き出し
  → tmux ステータスライン / WezTerm プラグインが読み取り・表示
```

## セッション状態

| State | 意味 | 対象ツール |
|-------|------|-----------|
| `idle` | ターン完了、ユーザー入力待ち | Claude Code, Codex, Gemini |
| `thinking` | AI が推論中 / プロセスが動作中 | 全ツール |
| `tool_use` | ツール実行中（承認済み） | Claude Code |
| `waiting` | ユーザーの操作を待機中 | Claude Code, Codex, Gemini |
| `error` | エラー発生 | Claude Code |

- Claude Code: ペインテキスト（逆順スキャン）を主軸に、JSONL を補助データとして用いて詳細判定
- Codex CLI: 子プロセスの有無で `idle`/`thinking` を判定し、ペインテキストから `waiting` を判定
- Gemini CLI: プロセス存在を基本とし、ペインテキストから `idle`/`waiting` を判定

## 設計原則

- **プロセス監視ファースト**: セッション生死の source of truth は OS プロセス。JSONL は補助データ
- **ペインテキスト優先判定**: Claude Code の詳細な状態や全ツールの待機状態判定において、現在のペインの画面出力を最も権威ある情報とする
- **スナップショット照合**: 毎回の Scan で全状態を再構築し、前回との差分を取る（イベント駆動ではない）
- **関心の分離**: 検出（Scanner）・状態判定（StateResolver/StateManager）・集約・出力（Exporter）を分離
- **インターフェース抽象化**: Terminal, Scanner は DI でモック差し替え可能（テスト容易性）
- **Elm Architecture**: TUI は bubbletea の Model-Update-View パターンに従う
- **アトミック書き込み**: temp + rename パターンで JSON 破損を防止
