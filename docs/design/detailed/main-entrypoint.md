# main.go ワイヤリング・ライフサイクル詳細設計（v2）

## 概要

v2 では、ファイルウォッチング方式から定期スキャン方式へと駆動モデルを転換する。
`Watcher` を廃止し、`Scanner` / `StateManager` / `StateResolver` の三層構造に置き換えることで、
モード間のコード重複を排除し、テスト容易性を高める。

---

## v1 との比較

### v1 の構造（簡略）

```
run()
  ├─ config.Load()
  ├─ initTerminal(cfg.Terminal)
  ├─ core.NewWatcher(cfg.WatchPath)
  ├─ core.NewStateManager(watcher)
  ├─ signal handling
  ├─ stateManager.InitialScan()
  ├─ [--once] writeStatus(); return
  ├─ watcher.Start(ctx)
  └─ [--no-tui] runNoTUI(ctx, watcher, stateManager, ...)
       └─ watcher.Events() + ticker → HandleEvent + writeStatus
         [TUI]   tui.NewModel(...) → tea.Run()
```

**問題点:**
- `runNoTUI()` と TUI モードで状態更新ロジックが分岐している
- `Watcher`（fsnotify ベース）は JSONL の変更検知であり、プロセス一覧の動的取得には対応していない
- `HandleEvent` のテストに fsnotify のモックが必要になる

### v2 の構造

```
run()
  ├─ config.Load()
  ├─ initTerminal(cfg.Terminal)
  ├─ processScanner = core.NewProcessScanner()
  ├─ scanner = core.NewDefaultScanner(term, processScanner, cfg)
  ├─ resolver = core.NewStateResolver(parser, cfg)
  ├─ stateManager = core.NewStateManager(resolver)
  ├─ exporter = core.NewExporter(cfg.ExportPath, cfg.Statusbar)
  ├─ signal handling
  ├─ [--once] doScan(); exporter.Write(); return
  ├─ [--no-tui] goroutine{ ticker → doScan() → exporter.Write() }
  └─ [TUI]      tui.NewModel(scanner, stateManager, term, cfg, exitOnJump) → tea.Run()
                  └─ TickMsg → doScan() → ScanResultMsg → Update()
```

---

## コンポーネント初期化順序

依存関係を明示するため、以下の順序で初期化する。

```
1. cfg            ← config.Load()
2. term           ← initTerminal(cfg.Terminal)
3. processScanner ← core.NewProcessScanner()
4. scanner        ← core.NewDefaultScanner(term, processScanner, cfg)
5. parser         ← core.NewParser()
6. resolver       ← core.NewStateResolver(parser, cfg)
7. stateManager   ← core.NewStateManager(resolver)
8. stateReader    ← stateManager  // StateManager は StateReader も実装する
9. exporter       ← core.NewExporter(cfg.ExportPath, cfg.Statusbar)
```

`stateManager` は `StateUpdater` と `StateReader` の両方を実装する。
`doScan()` には `StateUpdater` として、`exporter.Write()` と TUI には `StateReader` として渡す。

各コンポーネントはインターフェース経由で依存するため、テスト時にモックに差し替えられる。

---

## doScan() 関数

モード非依存のスキャン関数として定義する。

```go
// doScan はスキャンを実行し、StateManager を更新する。
// TUI / ヘッドレス / ワンショット の全モードで共有される。
// Scanner は常に ScanResult を返す（エラーは ScanResult.Err に格納）。
// StateManager.UpdateFromScan が Err の有無を判定し、
// エラー時は前回スナップショットを保持する。
func doScan(ctx context.Context, scanner core.Scanner, sm core.StateUpdater) error {
    result := scanner.Scan(ctx)
    return sm.UpdateFromScan(result)
}
```

### 設計判断: doScan() をモード非依存にする理由

| 観点 | 理由 |
|------|------|
| DRY | TUI / ヘッドレス / ワンショットで同一ロジックを重複させない |
| テスト容易性 | `Scanner` と `StateManager` をモックにすれば、モードに依存しない単体テストが書ける |
| 一貫性 | モード間で状態更新の動作が diverge しない |

---

## モード別の駆動方式

### ワンショットモード（`--once`）

```go
if err := doScan(ctx, scanner, stateManager); err != nil {
    return err
}
return exporter.Write(stateReader) // StateReader から Projects/Summary を読み取り JSON 出力
```

- スキャンを 1 回だけ実行して終了する
- CI / スクリプト組み込みを想定
- Exporter は `StateReader` から最新状態を読み取る

### ヘッドレスモード（`--no-tui`）

```go
go func() {
    ticker := time.NewTicker(cfg.ScanInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := doScan(ctx, scanner, stateManager); err != nil {
                log.Printf("scan error: %v", err)
                continue
            }
            if err := exporter.Write(stateReader); err != nil {
                log.Printf("export error: %v", err)
            }
        }
    }
}()
<-ctx.Done()
```

**設計判断: ヘッドレスモードの goroutine 内で Exporter を呼ぶ理由**

- TUI 不使用時は `bubbletea` の Update ループが存在しない
- goroutine + ticker が「TUI の TickMsg」に相当するイベントループを代替する
- `exporter.Write()` はアトミック書き込みであり、goroutine から安全に呼び出せる
- Exporter は `StateReader` インターフェース経由で最新状態を読み取る

### TUI モード

```go
model := tui.NewModel(scanner, stateManager, term, cfg, *exitOnJump)
p := tea.NewProgram(model, tea.WithAltScreen())
_, err = p.Run()
return err
```

TUI 内部の駆動は `doScanCmd` で行う（`tui.md` 参照）。
`doScan()` → `StateReader` から読み取り → `ScanResultMsg` を生成。

**設計判断: TUI モードで doScan を Update() 内で同期実行する理由**

| 観点 | 理由 |
|------|------|
| 単純性 | `tea.Cmd` による非同期実行は goroutine + channel の管理が複雑になる |
| スキャン速度 | プロセス一覧取得（ps）+ JSONL 読み取りは数十 ms 以内に完了する見込み |
| UI の応答性 | TickMsg 間隔（デフォルト 1〜2 秒）が十分長いため、ブロッキングの影響は無視できる |
| 将来の選択肢 | スキャンが重くなった場合は `tea.Cmd` 非同期化に切り替え可能。現時点では過剰設計を避ける |

---

## Config 拡張

v2 で追加・変更する設定項目。

```yaml
# ~/.config/baton/config.yaml（v2）

# スキャン間隔（旧: refresh_interval）
scan_interval: 2s

# Claude プロジェクトデータディレクトリ
claude_projects_dir: ~/.claude/projects

# Claude セッションメタデータディレクトリ（将来利用）
session_meta_dir: ~/.claude/projects

# ステータスバー設定（WezTerm Lua プラグイン向け）
statusbar:
  # テンプレート文字列（Go template 構文）
  format: "{{.Active}} active / {{.TotalSessions}} total{{if .Waiting}} | {{.Waiting}} waiting{{end}}"
  # ツールアイコンマッピング（AI クライアント種別）
  tool_icons:
    claude: ""
    codex: ""
    gemini: ""
    default: "●"

# WezTerm 統合設定
terminal:
  type: wezterm

# JSON エクスポート先
export_path: /tmp/baton-status.json
```

### 変更点まとめ

| キー | v1 | v2 | 理由 |
|------|----|----|------|
| `refresh_interval` | あり | `scan_interval` に改名 | ウォッチングからスキャンへの概念変更 |
| `watch_path` | あり | 削除 | Watcher 廃止に伴い不要 |
| `claude_projects_dir` | なし | 追加 | Scanner が JSONL を探す起点 |
| `session_meta_dir` | なし | 追加 | 将来のセッションメタデータ参照用 |
| `statusbar` | なし | 追加 | formatted_status 生成設定 |

---

## シグナルハンドリング

v1 と同様に `context.WithCancel` + `os.Signal` を使用する。変更なし。

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
go func() {
    <-sigCh
    cancel()
}()
```

---

## エラーハンドリング方針

- `doScan()` のエラーはログに記録し、前回の状態を保持する（パニックしない）
- `exporter.Write()` のエラーはログに記録し、次回スキャンを継続する
- `log.Fatal` は `main()` の最上位のみで使用する（v1 から踏襲）

---

## 関連ファイル

- `main.go` — エントリポイント（本文書の対象）
- `internal/core/scanner.go` — Scanner インターフェース + DefaultScanner 実装
- `internal/core/state.go` — StateManager（UpdateFromScan メソッド）
- `internal/core/exporter.go` — Exporter（formatted_status 生成）
- `internal/tui/model.go` — TUI モデル初期化
- `internal/tui/update.go` — TickMsg 処理・doScan 呼び出し
