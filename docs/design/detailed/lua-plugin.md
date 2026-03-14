# WezTerm Lua プラグイン v2 対応詳細設計

## 概要

v2 では baton（Go 側）が `formatted_status` を生成し、Lua プラグインはそれをそのまま表示する。
v1 で Lua 側が行っていた状態カウント・集計ロジックを Go 側に移管することで、
ロジックの二重管理を排除し、Lua プラグインをシンプルな表示レイヤーに純化する。

---

## v1 との比較

### v1 の構造

```lua
-- v1: Lua 側で集計・フォーマット
local function status_chunks(data)
  local active, thinking, tool_use, error_count = 0, 0, 0, 0
  for _, project in ipairs(data.projects or {}) do
    for _, session in ipairs(project.sessions or {}) do
      local s = session.state
      if s == "active"   then active      = active      + 1
      elseif s == "thinking" then thinking = thinking  + 1
      elseif s == "tool_use" then tool_use = tool_use  + 1
      elseif s == "error"    then error_count = error_count + 1
      end
    end
  end
  -- chunks を組み立てて返す
end

local function format_status(chunks)
  return wezterm.format(chunks)
end
```

**問題点:**
- baton（Go）と Lua の両方が状態カウントを管理しており、仕様変更時に両方の修正が必要
- 新しい状態（`Waiting` 等）を追加するたびに Lua 側も修正が必要
- アイコンやフォーマット文字列が Lua にハードコードされており、ユーザーがカスタマイズしにくい

### v2 の構造

```lua
-- v2: Lua は formatted_status をそのまま表示するだけ
local function read_status()
  -- JSON 読み込み + 5 秒キャッシュ（v1 から踏襲）
  local data = read_json(STATUS_FILE)

  -- version チェック
  if data.version ~= 2 then
    return "baton: unknown format"
  end

  return data.formatted_status, data.summary
end

local function format_status(formatted_status, summary)
  -- Waiting 強調: summary.waiting > 0 でオレンジ色
  if (summary.waiting or 0) > 0 then
    return wezterm.format({
      { Foreground = { Color = "#FF8800" } },
      { Text = formatted_status },
    })
  end
  return wezterm.format({
    { Text = formatted_status },
  })
end
```

---

## baton と Lua の責務分担

| 責務 | baton (Go) | Lua プラグイン |
|------|-----------|--------------|
| プロセス一覧のスキャン | する | しない |
| JSONL の解析・状態判定 | する | しない |
| 状態カウント（active / thinking 等） | する | しない |
| `formatted_status` の文字列生成 | する（Go template） | しない |
| アイコンの解決（tool_icons マッピング） | する | しない |
| `summary.waiting` による強調色の適用 | しない | する |
| `version` フィールドの検証 | しない（出力するのみ） | する |
| JSON ファイルのキャッシュ読み込み | しない | する（5 秒） |

### 設計判断: Lua 側の集計ロジックを削除する理由

1. **二重管理の排除**: 状態の種類・カウント方法を Go 側のみで管理することで、仕様変更の影響範囲を限定する
2. **カスタマイズ性の向上**: フォーマット・アイコンを `config.yaml` の `statusbar` セクションで設定できる（Lua ファイルの編集不要）
3. **テスト容易性**: Go 側でユニットテストが書けるため、Lua のシミュレーションが不要になる

---

## JSON 出力スキーマ（v2）

baton が `/tmp/baton-status.json` に書き出す JSON の構造。

```json
{
  "version": 2,
  "timestamp": "2026-03-07T12:34:56Z",
  "formatted_status": "baton: 2 active | 1 waiting | 1 1",
  "summary": {
    "total_sessions": 2,
    "active": 2,
    "waiting": 1,
    "by_tool": { "claude": 1, "codex": 1 }
  },
  "projects": [
    {
      "name": "baton",
      "path": "/Users/yoshihiko/ghq/github.com/yoshihiko555/baton",
      "workspace": "baton",
      "sessions": [
        {
          "pane_id": "5",
          "tool": "claude",
          "state": "waiting",
          "pid": 12345,
          "working_dir": "/Users/yoshihiko/ghq/github.com/yoshihiko555/baton",
          "branch": "feat-auth",
          "current_tool": "Bash",
          "input_tokens": 12500,
          "output_tokens": 8300
        },
        {
          "pane_id": "7",
          "tool": "codex",
          "state": "thinking",
          "pid": 12400,
          "working_dir": "/Users/yoshihiko/ghq/github.com/yoshihiko555/baton"
        }
      ]
    }
  ]
}
```

### フィールド説明

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `version` | number | スキーマバージョン。v2 では必ず `2` |
| `timestamp` | string | スキャン実行時刻（RFC3339） |
| `formatted_status` | string | WezTerm 表示用のフォーマット済み文字列 |
| `summary.total_sessions` | number | 全セッション数 |
| `summary.active` | number | active 状態のセッション数 (Thinking + ToolUse + Waiting) |
| `summary.waiting` | number | waiting 状態のセッション数 |
| `summary.by_tool` | object | ツール別セッション数 `{"claude": N, "codex": M}` |
| `projects` | array | プロジェクト別の詳細情報 |
| `projects[].workspace` | string | WezTerm ワークスペース名（`omitempty`: 未設定時はキー省略） |

---

## Lua プラグインの実装詳細

### read_status()

```lua
local CACHE_TTL = 5  -- 秒
local _cache = { data = nil, time = 0 }

local function read_status()
  local now = os.time()
  if _cache.data and (now - _cache.time) < CACHE_TTL then
    return _cache.data
  end

  local f = io.open(STATUS_FILE, "r")
  if not f then return nil end
  local content = f:read("*a")
  f:close()

  local ok, data = pcall(wezterm.json_parse, content)
  if not ok or not data then return nil end

  _cache.data = data
  _cache.time = now
  return data
end
```

### format_status()（v2 簡素化版）

```lua
local function format_status(data)
  -- version チェック
  if not data or data.version ~= 2 then
    return wezterm.format({
      { Foreground = { Color = "#888888" } },
      { Text = "baton: unknown format" },
    })
  end

  local text = data.formatted_status or ""
  local waiting = (data.summary or {}).waiting or 0

  -- Waiting 強調: オレンジ色で全体を表示
  if waiting > 0 then
    return wezterm.format({
      { Foreground = { Color = "#FF8800" } },
      { Text = text },
      "ResetAttributes",
    })
  end

  return wezterm.format({
    { Text = text },
    "ResetAttributes",
  })
end
```

### 設計判断: 強調判定に summary.waiting を使い formatted_status を文字列解析しない理由

| 観点 | 理由 |
|------|------|
| 堅牢性 | `formatted_status` の文字列フォーマットはユーザーが `config.yaml` で変更できるため、文字列解析は壊れやすい |
| 意図の明確化 | `summary.waiting` は「Waiting 状態のセッションが存在する」という意図を直接表す構造化データ |
| パフォーマンス | 文字列マッチングよりも数値比較の方が軽量 |

### setup()（v2 版）

```lua
local M = {}

function M.setup(config)
  config = config or {}
  local status_file = config.status_file or STATUS_FILE

  wezterm.on("update-right-status", function(window, _pane)
    local data = read_status()
    local status = format_status(data)
    window:set_right_status(status)
  end)
end

return M
```

---

## 後方互換性（v1 形式の JSON）

v1 形式（`version` フィールドなし）を受け取った場合のフォールバック。

```lua
local function format_status(data)
  -- v1 フォールバック: version フィールドがない場合
  if not data then
    return ""
  end

  if data.version ~= 2 then
    -- v1 形式の場合、最低限の情報を表示して警告する
    -- （v1 の projects 構造を参照しようとはしない）
    return wezterm.format({
      { Foreground = { Color = "#888888" } },
      { Text = "baton: unknown format" },
    })
  end

  -- v2 フォーマット処理（前述）
end
```

### 設計判断: version チェックのフォールバック戦略

- **表示を止める（空文字）** ではなく **"baton: unknown format"** を表示する理由:
  - ユーザーが「baton が動いているがフォーマットが古い」と気づける
  - サイレントな表示消失はデバッグが困難
- **v1 形式を解析してフォールバック表示しない** 理由:
  - v1 の Lua ロジックを v2 プラグインに残すと保守コストが倍になる
  - v1→v2 の移行期間は短いと想定しており、複雑な互換レイヤーは不要

---

## formatted_status の生成（Go 側の設定）

Lua が受け取る `formatted_status` は `config.yaml` の `statusbar` セクションで制御する。

```yaml
statusbar:
  format: "{{.Active}} active / {{.TotalSessions}} total{{if .Waiting}} | {{.Waiting}} waiting{{end}}"
  tool_icons:
    claude:  ""
    codex:   ""
    gemini:  ""
    default: "●"
```

Go template で使用できる変数:

| 変数 | 型 | 説明 |
|------|-----|------|
| `.TotalSessions` | int | 全セッション数 |
| `.Active` | int | アクティブセッション数 (Thinking + ToolUse + Waiting) |
| `.Waiting` | int | Waiting セッション数 |
| `.ByTool` | map[string]int | ツール別セッション数 |

---

## 関連ファイル

- `wezterm/baton-status.lua` — Lua プラグイン本体（本文書の対象）
- `internal/core/exporter.go` — `formatted_status` 生成・JSON 書き出し
- `internal/core/model.go` — `SessionState`（Waiting 含む）の定義
- `~/.config/baton/config.yaml` — `statusbar.format` / `statusbar.tool_icons` の設定
