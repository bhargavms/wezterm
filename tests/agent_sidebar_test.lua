local callbacks = {}

local action = {}
function action.CloseCurrentPane(options)
  return { kind = 'close', confirm = options.confirm }
end

package.preload.wezterm = function()
  return {
    action = action,
    action_callback = function(callback)
      table.insert(callbacks, callback)
      return callback
    end,
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
  local sidebar = {}
  function sidebar:get_user_vars()
    return { CODEX_AGENT_SIDEBAR = '1' }
  end

  local tab = {}
  function tab:panes_with_info()
    return { { pane = sidebar } }
  end

  local pane = {}
  function pane:tab()
    return tab
  end

  local window = make_window()
  toggle(window, pane)

  assert_equal(#window.calls, 1, 'close action count')
  assert_equal(window.calls[1].action.kind, 'close', 'close action kind')
  assert_equal(window.calls[1].action.confirm, false, 'close confirmation')
  assert_equal(window.calls[1].target, sidebar, 'closed pane')
end

do
  local injected
  local activated = false
  local split_spec

  local sidebar = {}
  function sidebar:inject_output(value)
    injected = value
  end

  local tab = {}
  function tab:panes_with_info()
    return {}
  end
  function tab:get_size()
    return { cols = 120 }
  end

  local pane = {}
  function pane:tab()
    return tab
  end
  function pane:split(spec)
    split_spec = spec
    return sidebar
  end
  function pane:activate()
    activated = true
  end

  toggle(make_window(), pane)

  assert_equal(split_spec.direction, 'Right', 'split direction')
  assert_equal(split_spec.top_level, true, 'top-level split')
  assert_equal(split_spec.size, 36, 'split width')
  assert_equal(split_spec.cwd, '/tmp/project', 'split cwd')
  assert_equal(split_spec.args[1], runner, 'runner command')
  assert_equal(split_spec.args[2], '--cwd', 'cwd flag')
  assert_equal(split_spec.args[3], '/tmp/project', 'cwd argument')
  assert_equal(split_spec.args[4], '--width', 'width flag')
  assert_equal(split_spec.args[5], '34', 'width argument')
  assert(injected:find('CODEX_AGENT_SIDEBAR', 1, true), 'sidebar marker was not injected')
  assert(injected:find('Agents', 1, true), 'sidebar title was not injected')
  assert_equal(activated, true, 'original pane activation')
end

print 'agent_sidebar_test: ok'
