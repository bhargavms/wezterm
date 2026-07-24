local callbacks = {}
local mux_windows = {}
local spawn_window_calls = {}
local spawn_window_result
local active_screen = { x = 100, y = 50, width = 2000, height = 1200 }

local action = {
  ToggleAlwaysOnTop = { kind = 'always-on-top' },
}

package.preload.wezterm = function()
  return {
    action = action,
    action_callback = function(callback)
      table.insert(callbacks, callback)
      return callback
    end,
    mux = {
      all_windows = function()
        return mux_windows
      end,
      spawn_window = function(spec)
        table.insert(spawn_window_calls, spec)
        mux_windows = { spawn_window_result[3] }
        return table.unpack(spawn_window_result)
      end,
    },
    gui = {
      screens = function()
        return {
          active = active_screen,
        }
      end,
    },
    time = {
      call_after = function(_, callback)
        callback()
      end,
    },
    home_dir = '/Users/test',
    log_error = function(message)
      error(message)
    end,
  }
end

local source = debug.getinfo(1, 'S').source:sub(2)
local source_dir = source:match '^(.*)/[^/]+$' or '.'
local repo_root = source_dir:match '^(.*)/tests$' or '.'
package.path = repo_root .. '/?.lua;' .. package.path

local agent_sidebar = require 'agent_sidebar'

local function assert_equal(actual, expected, label)
  if actual ~= expected then
    error(string.format('%s: expected %s, got %s', label, tostring(expected), tostring(actual)))
  end
end

local function make_window()
  local window = { calls = {} }
  function window:perform_action(requested_action, target)
    table.insert(self.calls, { action = requested_action, target = target })
  end
  return window
end

local runner = '/tmp/agent-sidebar/run'
local toggle = agent_sidebar.toggle_action {
  runner = runner,
  cwd_for_pane = function()
    return '/tmp/project'
  end,
}

do
  local closed = false
  local existing_sidebar = {}
  function existing_sidebar:get_user_vars()
    return { CODEX_AGENT_SIDEBAR = '1' }
  end
  function existing_sidebar:send_text(value)
    if value == 'q' then
      closed = true
      mux_windows = {}
    end
  end

  local existing_tab = {}
  function existing_tab:panes_with_info()
    return { { pane = existing_sidebar } }
  end

  local existing_window = {}
  function existing_window:tabs()
    return { existing_tab }
  end
  mux_windows = { existing_window }

  local spawned_sidebar = {}
  function spawned_sidebar:inject_output() end
  function spawned_sidebar:activate() end

  local source_tab = {}
  function source_tab:panes_with_info()
    return {}
  end
  function source_tab:get_size()
    return { cols = 120 }
  end

  local source_pane = {}
  function source_pane:tab()
    return source_tab
  end
  function source_pane:split()
    return spawned_sidebar
  end

  toggle(make_window(), source_pane)

  assert_equal(closed, true, 'existing global sidebar closed')
  assert_equal(#mux_windows, 0, 'global sidebar count after close')
  assert_equal(#spawn_window_calls, 0, 'new sidebar window count')
end

do
  active_screen = { x = 100, y = 50, width = 400, height = 360 }
  local injected
  local sidebar_title
  local overrides
  local inner_size
  local position
  local focused = false
  local gui_actions = {}

  local sidebar = {}
  function sidebar:inject_output(value)
    injected = value
  end
  function sidebar:pane_id()
    return 900
  end

  local sidebar_tab = {}

  local sidebar_gui = {}
  function sidebar_gui:set_config_overrides(value)
    overrides = value
  end
  function sidebar_gui:set_inner_size(width, height)
    inner_size = { width = width, height = height }
  end
  function sidebar_gui:set_position(x, y)
    position = { x = x, y = y }
  end
  function sidebar_gui:perform_action(requested_action, target)
    table.insert(gui_actions, { action = requested_action, target = target })
  end
  function sidebar_gui:focus()
    focused = true
  end

  local sidebar_window = {}
  function sidebar_window:set_title(value)
    sidebar_title = value
  end
  function sidebar_window:gui_window()
    return sidebar_gui
  end

  spawn_window_result = { sidebar_tab, sidebar, sidebar_window }

  local source_mux_window = {}
  function source_mux_window:window_id()
    return 7
  end

  local pane = {}
  function pane:pane_id()
    return 41
  end
  function pane:get_dimensions()
    return { viewport_rows = 37 }
  end

  local window = make_window()
  function window:mux_window()
    return source_mux_window
  end

  toggle(window, pane)

  assert_equal(#spawn_window_calls, 1, 'floating sidebar spawn count')
  local spawn = spawn_window_calls[1]
  assert_equal(spawn.cwd, '/tmp/project', 'floating sidebar cwd')
  assert_equal(spawn.args[1], runner, 'runner command')
  assert_equal(spawn.args[2], '--window-id', 'window id flag')
  assert_equal(spawn.args[3], '7', 'window id')
  assert_equal(spawn.args[4], '--source-pane-id', 'source pane flag')
  assert_equal(spawn.args[5], '41', 'source pane id')
  assert_equal(sidebar_title, 'Codex Agent Sidebar', 'sidebar mux window title')
  assert_equal(overrides.enable_tab_bar, false, 'sidebar tab bar')
  assert_equal(overrides.window_decorations, 'RESIZE', 'sidebar decorations')
  assert_equal(inner_size.width, 352, 'sidebar width stays inside a small screen')
  assert_equal(inner_size.height, 280, 'sidebar height stays inside a small screen')
  assert_equal(position.x, 124, 'sidebar is positioned on the right')
  assert_equal(position.y, 90, 'sidebar top margin')
  assert_equal(gui_actions[1].action.kind, 'always-on-top', 'floating window level')
  assert_equal(gui_actions[1].target, sidebar, 'floating action pane')
  assert(injected:find('CODEX_AGENT_SIDEBAR', 1, true), 'sidebar marker was not injected')
  assert_equal(focused, true, 'floating sidebar focus')
end

print 'agent_sidebar_test: ok'
