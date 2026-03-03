-- Usage:
--   local baton_status = require 'wezterm.baton-status'
--   baton_status.setup({
--     path = '/tmp/baton-status.json', -- optional
--     interval = 5, -- optional (seconds)
--   })

local M = {}
local wezterm = require 'wezterm'

-- Cache
local cache = { data = nil, last_read = 0 }

local CACHE_TTL_SECONDS = 5
local DEFAULT_STATUS_PATH = '/tmp/baton-status.json'

local function now_seconds()
  return os.time()
end

local function values(tbl)
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

  local ok, parsed = pcall(wezterm.json_parse, content)
  if not ok or type(parsed) ~= 'table' then
    return nil
  end

  cache.data = parsed
  cache.last_read = now
  return parsed
end

local function count_session_state(session)
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

function M.format_status(data)
  if type(data) ~= 'table' then
    return wezterm.format({
      { Foreground = { Color = '#808080' } },
      { Text = 'baton: no data' },
    })
  end

  local project_count = 0
  local active_count = 0
  local thinking_count = 0
  local tool_use_count = 0
  local error_count = 0

  local projects = data.projects
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
  if type(standalone_sessions) == 'table' then
    if project_count == 0 then
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

  return wezterm.format(chunks)
end

function M.setup(config)
  config = config or {}
  local path = config.path or DEFAULT_STATUS_PATH
  local interval = config.interval or 5
  local last_update = 0

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
