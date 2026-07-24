local wezterm = require 'wezterm'
local act = wezterm.action
local mux = wezterm.mux

local M = {}

local SIDEBAR_USER_VAR = 'CODEX_AGENT_SIDEBAR'
local SIDEBAR_MARKER = '\x1b]1337;SetUserVar=' .. SIDEBAR_USER_VAR .. '=MQ==\x07'
local SIDEBAR_WINDOW_TITLE = 'Codex Agent Sidebar'
local SIDEBAR_COLUMNS = 42
local SIDEBAR_SCREEN_MARGIN = 40

local function find_sidebar()
  for _, mux_window in ipairs(mux.all_windows()) do
    local titled_pane
    if
      mux_window.get_title
      and mux_window:get_title() == SIDEBAR_WINDOW_TITLE
      and mux_window.active_pane
    then
      titled_pane = mux_window:active_pane()
    end
    for _, tab in ipairs(mux_window:tabs()) do
      for _, info in ipairs(tab:panes_with_info()) do
        if info.pane:get_user_vars()[SIDEBAR_USER_VAR] == '1' then
          return info.pane
        end
      end
    end
    if titled_pane then
      return titled_pane
    end
  end

  return nil
end

local function sidebar_geometry()
  if not wezterm.gui then
    return nil
  end
  local screens = wezterm.gui.screens()
  local screen = screens and screens.active
  if not screen then
    return nil
  end

  local available_width = math.max(1, screen.width - 48)
  local width = math.min(
    available_width,
    math.max(420, math.min(640, math.floor(screen.width * 0.22)))
  )
  local height = math.max(1, screen.height - SIDEBAR_SCREEN_MARGIN * 2)
  local top_margin = math.min(
    SIDEBAR_SCREEN_MARGIN,
    math.max(0, math.floor((screen.height - height) / 2))
  )
  return {
    width = width,
    height = height,
    x = screen.x + screen.width - width - 24,
    y = screen.y + top_margin,
  }
end

local function configure_sidebar_window(mux_window, pane, geometry, attempt)
  local gui_window = mux_window:gui_window()
  if not gui_window then
    if attempt < 20 then
      wezterm.time.call_after(0.05, function()
        configure_sidebar_window(mux_window, pane, geometry, attempt + 1)
      end)
    else
      wezterm.log_error 'failed to attach GUI agent sidebar window'
    end
    return
  end

  gui_window:set_config_overrides {
    enable_tab_bar = false,
    window_close_confirmation = 'NeverPrompt',
    window_decorations = 'RESIZE',
    window_padding = {
      left = 10,
      right = 10,
      top = 10,
      bottom = 10,
    },
  }
  if geometry then
    gui_window:set_inner_size(geometry.width, geometry.height)
    gui_window:set_position(geometry.x, geometry.y)
  end
  gui_window:perform_action(act.ToggleAlwaysOnTop, pane)
  gui_window:focus()
end

function M.toggle_action(options)
  assert(options and options.runner, 'agent sidebar runner is required')
  assert(options.cwd_for_pane, 'agent sidebar cwd resolver is required')

  return wezterm.action_callback(function(window, pane)
    local existing = find_sidebar()
    if existing then
      existing:send_text 'q'
      return
    end

    local cwd = options.cwd_for_pane(pane) or wezterm.home_dir
    local geometry = sidebar_geometry()
    local pane_dimensions = pane:get_dimensions()
    local source_window_id = window:mux_window():window_id()
    local ok, tab_or_error, sidebar, sidebar_window = pcall(function()
      return mux.spawn_window {
        cwd = cwd,
        args = {
          options.runner,
          '--window-id',
          tostring(source_window_id),
          '--source-pane-id',
          tostring(pane:pane_id()),
          '--width',
          tostring(SIDEBAR_COLUMNS - 2),
        },
        width = SIDEBAR_COLUMNS,
        height = math.max(20, pane_dimensions.viewport_rows or 30),
        position = geometry and {
          x = geometry.x,
          y = geometry.y,
          origin = 'ScreenCoordinateSystem',
        } or nil,
      }
    end)

    if not ok then
      wezterm.log_error('failed to open floating agent sidebar: ' .. tostring(tab_or_error))
      return
    end

    sidebar_window:set_title(SIDEBAR_WINDOW_TITLE)
    sidebar:inject_output(SIDEBAR_MARKER)
    configure_sidebar_window(sidebar_window, sidebar, geometry, 1)
  end)
end

return M
