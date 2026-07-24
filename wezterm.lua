local wezterm = require 'wezterm'
local mux = wezterm.mux
local act = wezterm.action
local agent_sidebar = require 'agent_sidebar'

local EWA_ROOT = '/Users/mogra/ewa-services'
local HOME = wezterm.home_dir
local AGENT_SIDEBAR_RUNNER

-- wezterm.gui is not available to the mux server, so take care to
-- do something reasonable when this config is evaluated by the mux
function get_appearance()
  if wezterm.gui then
    return wezterm.gui.get_appearance()
  end
  return 'Dark'
end

function scheme_for_appearance(appearance)
  if appearance:find 'Dark' then
    return 'CGA'
  else
    return 'GruvboxLight'
  end
end

local function join_path(...)
  local parts = { ... }
  local path = table.remove(parts, 1) or ''
  for _, part in ipairs(parts) do
    path = path:gsub('/+$', '') .. '/' .. tostring(part):gsub('^/+', '')
  end
  return path
end

AGENT_SIDEBAR_RUNNER = join_path(wezterm.config_dir, 'agent-sidebar', 'run')

local function basename(path)
  if not path or path == '' then
    return ''
  end
  return tostring(path):gsub('/+$', ''):match '([^/]+)$' or tostring(path)
end

local function path_exists(path)
  if not path or path == '' then
    return false
  end

  local ok, _, code = os.rename(path, path)
  return ok or code == 13
end

local function first_existing_path(...)
  for _, path in ipairs { ... } do
    if path_exists(path) then
      return path
    end
  end
  return HOME
end

local function zsh_args(command)
  return { '/bin/zsh', '-lc', command }
end

local function url_decode(path)
  return path:gsub('%%(%x%x)', function(hex)
    return string.char(tonumber(hex, 16))
  end)
end

local function pane_current_dir(pane)
  local cwd_uri = pane:get_current_working_dir()
  if not cwd_uri then
    return nil
  end

  local ok, file_path = pcall(function()
    return cwd_uri.file_path
  end)
  if ok and file_path then
    return file_path
  end

  local cwd = tostring(cwd_uri)
  if cwd:find '^file://' then
    return url_decode(cwd:gsub('^file://[^/]*', ''))
  end

  return cwd
end

local function tab_spec(root, tab)
  local cwd = first_existing_path(tab.cwd, root)
  local spec = { cwd = cwd }
  if tab.command then
    spec.args = zsh_args(tab.command)
  end
  return spec
end

local function set_tab_title(tab, title)
  if not title or title == '' then
    return
  end
  pcall(function()
    tab:set_title(title)
  end)
end

local function finn_tabs(root)
  return {
    { title = 'editor', cwd = root, command = 'nvim .' },
    { title = 'web', cwd = root, command = 'npm start' },
    { title = 'test', cwd = root, command = 'npm run test:watch' },
    { title = 'mobile', cwd = root },
    { title = 'functions', cwd = join_path(root, 'functions') },
    { title = 'shell', cwd = root },
  }
end

local layout_tabs = {
  finn = finn_tabs,

  infra = function(root)
    return {
      { title = 'editor', cwd = root, command = 'nvim .' },
      { title = 'terraform', cwd = first_existing_path(join_path(root, 'terraform'), root) },
      { title = 'test', cwd = root },
    }
  end,

  growthbook_mcp = function(root)
    return {
      { title = 'editor', cwd = root, command = 'nvim .' },
      { title = 'dev', cwd = root, command = 'npm run dev' },
      { title = 'test', cwd = root, command = 'npm run test:watch' },
      { title = 'shell', cwd = root },
    }
  end,

  pipeshub = function(root)
    local frontend = first_existing_path(join_path(root, 'frontend'), root)
    return {
      { title = 'editor', cwd = frontend, command = 'nvim .' },
      { title = 'frontend', cwd = frontend, command = 'npm run dev' },
      { title = 'backend', cwd = first_existing_path(join_path(root, 'backend'), root) },
      { title = 'docker', cwd = root },
      { title = 'shell', cwd = root },
    }
  end,

  greenhouse_triage = function(root)
    return {
      { title = 'editor', cwd = root, command = 'nvim .' },
      { title = 'test', cwd = root, command = 'go test ./...' },
      { title = 'shell', cwd = root },
    }
  end,

  generic = function(root)
    return {
      { title = 'editor', cwd = root, command = 'nvim .' },
      { title = 'shell', cwd = root },
      { title = 'test', cwd = root },
    }
  end,
}

local workspace_profiles = {}
local workspace_profile_order = {}
local explicit_roots = {}

local function register_workspace(profile)
  if workspace_profiles[profile.name] then
    return
  end

  workspace_profiles[profile.name] = profile
  table.insert(workspace_profile_order, profile.name)
  explicit_roots[profile.root] = true
end

register_workspace { name = 'finn', root = join_path(EWA_ROOT, 'FINN-Web-App'), layout = 'finn' }
register_workspace { name = 'finn-2637', root = join_path(EWA_ROOT, 'FINN-Web-App-FE-2637'), layout = 'finn' }
register_workspace { name = 'finn-2638', root = join_path(EWA_ROOT, 'FINN-Web-App-FE-2638'), layout = 'finn' }
register_workspace { name = 'finn-wv', root = join_path(EWA_ROOT, 'FINN-Web-App-FE-2638-WV'), layout = 'finn' }
register_workspace { name = 'infra', root = join_path(EWA_ROOT, 'github-infrastructure'), layout = 'infra' }
register_workspace { name = 'growthbook-mcp', root = join_path(EWA_ROOT, 'growthbook-mcp'), layout = 'growthbook_mcp' }
register_workspace { name = 'pipeshub', root = join_path(EWA_ROOT, 'pipeshub-ai'), layout = 'pipeshub' }
register_workspace { name = 'greenhouse-triage', root = join_path(EWA_ROOT, 'greenhouse-triage'), layout = 'greenhouse_triage' }

local function discover_ewa_repos()
  local ok, success, stdout = pcall(function()
    return wezterm.run_child_process {
      '/usr/bin/find',
      EWA_ROOT,
      '-mindepth',
      '1',
      '-maxdepth',
      '1',
      '-type',
      'd',
    }
  end)

  if not ok or not success or not stdout then
    return
  end

  local repos = {}
  for path in stdout:gmatch '[^\r\n]+' do
    if not explicit_roots[path] then
      table.insert(repos, path)
    end
  end
  table.sort(repos)

  for _, path in ipairs(repos) do
    register_workspace {
      name = basename(path),
      root = path,
      layout = 'generic',
    }
  end
end

discover_ewa_repos()

local shortcut_workspaces = {
  { key = '1', name = 'finn' },
  { key = '2', name = 'finn-2637' },
  { key = '3', name = 'finn-2638' },
  { key = '4', name = 'finn-wv' },
  { key = '5', name = 'infra' },
  { key = '6', name = 'growthbook-mcp' },
  { key = '7', name = 'pipeshub' },
  { key = '8', name = 'greenhouse-triage' },
}

local function workspace_exists(name)
  for _, workspace_name in ipairs(mux.get_workspace_names()) do
    if workspace_name == name then
      return true
    end
  end
  return false
end

local function tabs_for_profile(profile)
  local layout = layout_tabs[profile.layout] or layout_tabs.generic
  return layout(profile.root)
end

local function create_workspace(profile)
  local tabs = tabs_for_profile(profile)
  if #tabs == 0 then
    tabs = layout_tabs.generic(profile.root)
  end

  local first_spec = tab_spec(profile.root, tabs[1])
  first_spec.workspace = profile.name
  local first_tab, _, mux_window = mux.spawn_window(first_spec)
  set_tab_title(first_tab, tabs[1].title)

  for index = 2, #tabs do
    local tab = mux_window:spawn_tab(tab_spec(profile.root, tabs[index]))
    set_tab_title(tab, tabs[index].title)
  end
end

local function switch_or_create_workspace(profile, window, pane)
  if not workspace_exists(profile.name) then
    create_workspace(profile)
  end

  window:perform_action(act.SwitchToWorkspace { name = profile.name }, pane)
end

local function switch_workspace_action(name)
  return wezterm.action_callback(function(window, pane)
    local profile = workspace_profiles[name]
    if profile then
      switch_or_create_workspace(profile, window, pane)
    end
  end)
end

local function workspace_choices()
  local seen = {}
  local choices = {}

  for _, name in ipairs(workspace_profile_order) do
    local profile = workspace_profiles[name]
    table.insert(choices, {
      id = name,
      label = string.format('%s  %s', name, basename(profile.root)),
    })
    seen[name] = true
  end

  local active = mux.get_workspace_names()
  table.sort(active)
  for _, name in ipairs(active) do
    if not seen[name] then
      table.insert(choices, {
        id = name,
        label = name .. '  active',
      })
    end
  end

  return choices
end

local function choose_workspace_action()
  return wezterm.action_callback(function(window, pane)
    window:perform_action(
      act.InputSelector {
        title = 'Workspaces',
        fuzzy = true,
        choices = workspace_choices(),
        action = wezterm.action_callback(function(inner_window, inner_pane, id)
          if not id then
            return
          end

          local profile = workspace_profiles[id]
          if profile then
            switch_or_create_workspace(profile, inner_window, inner_pane)
          else
            inner_window:perform_action(act.SwitchToWorkspace { name = id }, inner_pane)
          end
        end),
      },
      pane
    )
  end)
end

local function prompt_workspace_action()
  return act.PromptInputLine {
    description = 'Workspace name',
    action = wezterm.action_callback(function(window, pane, line)
      if not line then
        return
      end

      local name = line:gsub('^%s+', ''):gsub('%s+$', '')
      if name == '' then
        return
      end

      local profile = workspace_profiles[name]
      if profile then
        switch_or_create_workspace(profile, window, pane)
      else
        window:perform_action(
          act.SwitchToWorkspace {
            name = name,
            spawn = { cwd = pane_current_dir(pane) },
          },
          pane
        )
      end
    end),
  }
end

local function switch_relative_workspace_action(delta)
  return wezterm.action_callback(function(window, pane)
    local names = mux.get_workspace_names()
    table.sort(names)
    if #names == 0 then
      return
    end

    local current = window:active_workspace()
    local current_index = 1
    for index, name in ipairs(names) do
      if name == current then
        current_index = index
        break
      end
    end

    local next_index = ((current_index - 1 + delta) % #names) + 1
    window:perform_action(act.SwitchToWorkspace { name = names[next_index] }, pane)
  end)
end

local function spawn_tab_in_current_dir_action()
  return wezterm.action_callback(function(window, pane)
    window:perform_action(act.SpawnCommandInNewTab { cwd = pane_current_dir(pane) }, pane)
  end)
end

local function split_in_current_dir_action(direction)
  return wezterm.action_callback(function(window, pane)
    local spec = {
      domain = 'CurrentPaneDomain',
      cwd = pane_current_dir(pane),
    }

    if direction == 'horizontal' then
      window:perform_action(act.SplitHorizontal(spec), pane)
    else
      window:perform_action(act.SplitVertical(spec), pane)
    end
  end)
end

wezterm.on('update-right-status', function(window, pane)
  local workspace = window:active_workspace()
  local cwd = basename(pane_current_dir(pane))
  local label = workspace

  if cwd ~= '' then
    label = label .. ' : ' .. cwd
  end

  window:set_right_status(wezterm.format {
    { Attribute = { Intensity = 'Bold' } },
    { Text = ' ' .. label .. ' ' },
  })
end)

local config = wezterm.config_builder()
config.default_prog = { '/bin/zsh' }
config.selection_word_boundary = ' \t\n{}[]()"\'`,;:@│┃*…$'
config.debug_key_events = false
config.color_scheme = scheme_for_appearance(get_appearance())
config.font = wezterm.font '0xProto Nerd Font'
config.audible_bell = 'Disabled'
config.hide_tab_bar_if_only_one_tab = true
config.font_size = 18.0
config.send_composed_key_when_left_alt_is_pressed = false
config.send_composed_key_when_right_alt_is_pressed = false
config.leader = { key = 'Space', mods = 'CTRL', timeout_milliseconds = 1000 }

config.keys = {
  {
    key = 'Space',
    mods = 'LEADER|CTRL',
    action = act.SendKey { key = 'Space', mods = 'CTRL' },
  },
  { key = 'w', mods = 'LEADER', action = choose_workspace_action() },
  { key = 'W', mods = 'LEADER|SHIFT', action = prompt_workspace_action() },
  { key = '[', mods = 'LEADER', action = switch_relative_workspace_action(-1) },
  { key = ']', mods = 'LEADER', action = switch_relative_workspace_action(1) },
  { key = 'n', mods = 'LEADER', action = spawn_tab_in_current_dir_action() },
  {
    key = 'a',
    mods = 'LEADER',
    action = agent_sidebar.toggle_action {
      runner = AGENT_SIDEBAR_RUNNER,
      cwd_for_pane = pane_current_dir,
    },
  },
  { key = '|', mods = 'LEADER|SHIFT', action = split_in_current_dir_action 'horizontal' },
  { key = '\\', mods = 'LEADER', action = split_in_current_dir_action 'horizontal' },
  { key = '-', mods = 'LEADER', action = split_in_current_dir_action 'vertical' },
  { key = 'h', mods = 'LEADER', action = act.ActivatePaneDirection 'Left' },
  { key = 'j', mods = 'LEADER', action = act.ActivatePaneDirection 'Down' },
  { key = 'k', mods = 'LEADER', action = act.ActivatePaneDirection 'Up' },
  { key = 'l', mods = 'LEADER', action = act.ActivatePaneDirection 'Right' },
  { key = 'z', mods = 'LEADER', action = act.TogglePaneZoomState },
}

for _, shortcut in ipairs(shortcut_workspaces) do
  table.insert(config.keys, {
    key = shortcut.key,
    mods = 'LEADER',
    action = switch_workspace_action(shortcut.name),
  })
end

return config
