-- 使い方:
--   local baton_status = require 'wezterm.baton-status'
--   baton_status.setup({
--     path = '/tmp/baton-status.json', -- 省略可
--     interval = 5, -- 省略可（秒）
--   })

local M = {}
local wezterm = require 'wezterm'

-- 状態ファイル読み取り結果の短期キャッシュ
local cache = { data = nil, last_read = 0 }

local CACHE_TTL_SECONDS = 5
local DEFAULT_STATUS_PATH = '/tmp/baton-status.json'

local function now_seconds()
  return os.time()
end

local function values(tbl)
  -- table 以外が来た場合も for-in が安全に回せるよう、空イテレータを返す
  if type(tbl) ~= 'table' then
    return function()
      return nil
    end
  end

  local key
  return function()
    key = next(tbl, key)
    if key ~= nil then
      return tbl[key]
    end
  end
end

function M.read_status(path)
  path = path or DEFAULT_STATUS_PATH

  -- 直近読み取りから TTL 以内ならファイル再読込を避ける
  local now = now_seconds()
  if cache.data ~= nil and (now - cache.last_read) < CACHE_TTL_SECONDS then
    return cache.data
  end

  local file = io.open(path, 'r')
  if not file then
    return nil
  end

  local content = file:read('*a')
  file:close()

  if not content or content == '' then
    return nil
  end

  -- JSON の破損で落ちないように pcall で保護
  local ok, parsed = pcall(wezterm.json_parse, content)
  if not ok or type(parsed) ~= 'table' then
    return nil
  end

  -- 正常時のみキャッシュ更新
  cache.data = parsed
  cache.last_read = now
  return parsed
end

local function count_session_state(session)
  -- セッション state を集計用の論理値に正規化する
  local active = false
  local thinking = false
  local tool_use = false
  local errored = false

  if type(session) ~= 'table' then
    return active, thinking, tool_use, errored
  end

  local state = session.state

  if state == 'thinking' or state == 'tool_use' then
    active = true
  end
  if state == 'thinking' then
    thinking = true
  end
  if state == 'tool_use' then
    tool_use = true
  end
  if state == 'error' then
    errored = true
  end

  return active, thinking, tool_use, errored
end

function M.status_chunks(data)
  -- wezterm.format に渡せる chunks テーブルを返す（外部から統合しやすい形式）
  if type(data) ~= 'table' then
    return nil
  end

  local project_count = 0
  local active_count = 0
  local thinking_count = 0
  local tool_use_count = 0
  local error_count = 0

  local projects = data.projects
  -- projects[].sessions[] 形式のデータを集計
  if type(projects) == 'table' then
    for project in values(projects) do
      project_count = project_count + 1
      local sessions = type(project) == 'table' and project.sessions or nil
      if type(sessions) == 'table' then
        for session in values(sessions) do
          local active, thinking, tool_use, errored = count_session_state(session)
          if active then
            active_count = active_count + 1
          end
          if thinking then
            thinking_count = thinking_count + 1
          end
          if tool_use then
            tool_use_count = tool_use_count + 1
          end
          if errored then
            error_count = error_count + 1
          end
        end
      end
    end
  end

  local standalone_sessions = data.sessions
  -- projects が無く sessions だけある旧/簡易形式にも対応
  if type(standalone_sessions) == 'table' then
    if project_count == 0 then
      -- standalone のみでも表示上は 1 プロジェクトとして扱う
      project_count = 1
    end
    for session in values(standalone_sessions) do
      local active, thinking, tool_use, errored = count_session_state(session)
      if active then
        active_count = active_count + 1
      end
      if thinking then
        thinking_count = thinking_count + 1
      end
      if tool_use then
        tool_use_count = tool_use_count + 1
      end
      if errored then
        error_count = error_count + 1
      end
    end
  end

  local chunks = {
    { Foreground = { Color = '#A0A0A0' } },
    { Text = 'baton: ' },
    { Foreground = { Color = '#7DCFFF' } },
    { Text = tostring(project_count) },
    { Foreground = { Color = '#A0A0A0' } },
    { Text = ' proj | ' },
    { Foreground = { Color = '#9ECE6A' } },
    { Text = tostring(active_count) },
    { Foreground = { Color = '#A0A0A0' } },
    { Text = ' active | ' },
    { Foreground = { Color = '#E0AF68' } },
    { Text = tostring(thinking_count) },
    { Foreground = { Color = '#A0A0A0' } },
    { Text = ' thinking' },
  }

  -- 0 件の指標は省略してステータスを短く保つ
  if tool_use_count > 0 then
    table.insert(chunks, { Foreground = { Color = '#A0A0A0' } })
    table.insert(chunks, { Text = ' | ' })
    table.insert(chunks, { Foreground = { Color = '#73DACA' } })
    table.insert(chunks, { Text = tostring(tool_use_count) })
    table.insert(chunks, { Foreground = { Color = '#A0A0A0' } })
    table.insert(chunks, { Text = ' tool_use' })
  end

  if error_count > 0 then
    table.insert(chunks, { Foreground = { Color = '#A0A0A0' } })
    table.insert(chunks, { Text = ' | ' })
    table.insert(chunks, { Foreground = { Color = '#F7768E' } })
    table.insert(chunks, { Text = tostring(error_count) })
    table.insert(chunks, { Foreground = { Color = '#A0A0A0' } })
    table.insert(chunks, { Text = ' error' })
  end

  return chunks
end

function M.format_status(data)
  local chunks = M.status_chunks(data)
  if not chunks then
    return wezterm.format({
      { Foreground = { Color = '#808080' } },
      { Text = 'baton: no data' },
    })
  end
  return wezterm.format(chunks)
end

function M.setup(config)
  config = config or {}
  local path = config.path or DEFAULT_STATUS_PATH
  local interval = config.interval or 5
  local last_update = 0

  -- WezTerm の update-right-status は高頻度で呼ばれるため、更新間隔で間引く
  wezterm.on('update-right-status', function(window, _)
    local now = now_seconds()
    if (now - last_update) < interval then
      return
    end
    last_update = now

    local data = M.read_status(path)
    local status = M.format_status(data)
    window:set_right_status(status)
  end)
end

return M
