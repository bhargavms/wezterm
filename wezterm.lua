local wezterm = require 'wezterm'

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

local config = wezterm.config_builder()
config.default_prog = { '/bin/zsh' }
config.selection_word_boundary = ' \t\n{}[]()"\'`,;:@│┃*…$'
config.debug_key_events = true
config.color_scheme = scheme_for_appearance(get_appearance())
config.font = wezterm.font '0xProto Nerd Font'
config.audible_bell = 'Disabled'
config.hide_tab_bar_if_only_one_tab = true
config.font_size = 18.0
config.send_composed_key_when_left_alt_is_pressed = false
config.send_composed_key_when_right_alt_is_pressed = false

return config

