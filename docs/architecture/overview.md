# Architecture Overview

**バージョン**: v2
**最終更新**: 2026-03-07
**要件定義書**: `docs/requirements/requirements-v2.md`

> **v1 からの変更**: セッション発見の source of truth を「JSONL ファイル存在」から「OS プロセス存在」に変更。
> v1 の設計は `git log` で参照可能。

## 概要

baton は AI コーディングエージェント（Claude Code, Codex CLI, Gemini CLI）のセッション状態をリアルタイム監視し、TUI ダッシュボードおよび WezTerm ステータスバーに表示する Go アプリケーションである。

## システム構成

```
┌─────────────────────────────────────────────────────────────┐
│  WezTerm Panes (AI セッション実行環境)                        │
│  各ペインで claude / codex / gemini が直接起動される           │
└──────────────────────┬──────────────────────────────────────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
┌──────────────────┐  ┌──────────────────┐
│ wezterm cli list │  │ ps -t <tty>      │
│ (ペイン情報)      │  │ (プロセス検出)    │
│ TTY, CWD, PaneID │  │ PID, COMM        │
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
│  │(ps パース)   │    │              │                        │
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
    │ (WezTerm)   │ │ (version: 2 スキーマ)         │
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

| コンポーネント | 責務 |
|--------------|------|
| **Scanner** | WezTerm ペイン一覧取得 + ペインごとのプロセス検出を統合。1 回のスキャン = 1 `wezterm cli list` + N `ps` |
| **ProcessScanner** | `ps -t <tty> -o pid,ppid,comm` の出力をパースし、COMM 名で AI プロセスを同定 |
| **StateResolver** | Claude Code セッションの JSONL を解析し、詳細状態（Idle/Thinking/ToolUse/Waiting/Error）を判定 |
| **StateManager** | スキャン結果からスナップショット照合。前回→今回の差分でセッション追加・削除・状態更新 |
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

- `Terminal` インターフェース: `Name()`, `IsAvailable()`, `ListPanes()`, `FocusPane()`
- `WezTerminal`: WezTerm CLI を使用した実装
- `Pane` 構造体: `ID`, `Title`, `TabID`, `WorkingDir`, `TTYName`, `IsActive`

### Config パッケージ (`internal/config/`)

YAML 設定ファイルの読み込みとデフォルト値の解決。

## データフロー

### スキャンサイクル（全モード共通）

```
ticker (2-3秒間隔)
  → Scanner.Scan()
    → wezterm cli list --format json  → ペイン一覧 (TTY, CWD, PaneID)
    → per pane: ps -t <tty>           → AI プロセス検出 (PID, COMM)
    → Claude セッション: JSONL tail   → 詳細状態判定
  → StateManager.UpdateFromScan()     → スナップショット照合・状態更新
  → Exporter / TUI に反映
```

### TUI モード

```
ticker
  → ScanResultMsg として Update() に配信
  → StateManager.UpdateFromScan() で状態更新
  → View() で再描画
```

### ヘッドレスモード（`--no-tui`）

```
ticker
  → Scanner.Scan()
  → StateManager.UpdateFromScan() で状態更新
  → Exporter.WriteStatusJSON() で /tmp/baton-status.json に書き出し
  → WezTerm プラグインが読み取り・表示
```

## セッション状態

| State | 意味 | 対象ツール |
|-------|------|-----------|
| `idle` | ターン完了、ユーザー入力待ち | Claude Code |
| `thinking` | AI が推論中 / プロセスが動作中 | 全ツール |
| `tool_use` | ツール実行中（承認済み） | Claude Code |
| `waiting` | ユーザーの操作を待機中 | Claude Code |
| `error` | エラー発生 | Claude Code |

- Claude Code: JSONL の最終エントリから 5 段階を判定（`progress` エントリで Waiting/ToolUse を区別）
- Codex/Gemini: プロセス存在 = `thinking`、プロセス消失 = セッション除外

## 設計原則

- **プロセス監視ファースト**: セッション生死の source of truth は OS プロセス。JSONL は補助データ
- **スナップショット照合**: 毎回の Scan で全状態を再構築し、前回との差分を取る（イベント駆動ではない）
- **関心の分離**: 検出（Scanner）・状態判定（StateResolver）・集約（StateManager）・出力（Exporter）を分離
- **インターフェース抽象化**: Terminal, Scanner は DI でモック差し替え可能（テスト容易性）
- **Elm Architecture**: TUI は bubbletea の Model-Update-View パターンに従う
- **アトミック書き込み**: temp + rename パターンで JSON 破損を防止
- **ベストエフォート補完**: 必須データ（プロセス, ペイン）で骨格を作り、任意データ（JSONL, session-meta）で補完
