local wezterm = require 'wezterm'
local act = wezterm.action

local M = {}

local SIDEBAR_USER_VAR = 'CODEX_AGENT_SIDEBAR'
local SIDEBAR_MARKER = '\x1b]1337;SetUserVar=' .. SIDEBAR_USER_VAR .. '=MQ==\x07'
local SIDEBAR_TITLE = '\x1b]2;Agents\x07'

local function find_sidebar(tab)
  if not tab then
    return nil
  end

  for _, info in ipairs(tab:panes_with_info()) do
    if info.pane:get_user_vars()[SIDEBAR_USER_VAR] == '1' then
      return info.pane
    end
  end

  return nil
end

local function sidebar_width(tab)
  local size = tab:get_size()
  local width = math.floor(size.cols * 0.30)

  if size.cols < 90 then
    return math.max(24, math.floor(size.cols * 0.40))
  end

  return math.max(36, math.min(52, width))
end

function M.toggle_action(options)
  assert(options and options.runner, 'agent sidebar runner is required')
  assert(options.cwd_for_pane, 'agent sidebar cwd resolver is required')

  return wezterm.action_callback(function(window, pane)
    local tab = pane:tab()
    if not tab then
      return
    end

    local existing = find_sidebar(tab)
    if existing then
      window:perform_action(act.CloseCurrentPane { confirm = false }, existing)
      return
    end

    local cwd = options.cwd_for_pane(pane) or wezterm.home_dir
    local width = sidebar_width(tab)
    local ok, sidebar = pcall(function()
      return pane:split {
        direction = 'Right',
        top_level = true,
        size = width,
        cwd = cwd,
        args = {
          options.runner,
          '--cwd',
          cwd,
          '--width',
          tostring(math.max(20, width - 2)),
        },
      }
    end)

    if not ok then
      wezterm.log_error('failed to open agent sidebar: ' .. tostring(sidebar))
      return
    end

    sidebar:inject_output(SIDEBAR_MARKER .. SIDEBAR_TITLE)
    pane:activate()
  end)
end

return M
