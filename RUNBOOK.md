# WezTerm Agent Sidebar Runbook

This runbook covers installing and using the agent sidebar feature in this repo
on a new machine.

## 1) Prerequisites

- Install WezTerm.
- Install Go (for the first-run build of the sidebar watcher).
- Ensure terminal can read this repo from your WezTerm config path:
  `~/.config/wezterm`.

## 2) Install this repository as WezTerm config

```bash
git clone https://github.com/bhargavms/wezterm.git ~/.config/wezterm
cd ~/.config/wezterm
```

Optional: if you already have a custom config, back it up before replacing it.

## 3) Verify launch script permissions

```bash
chmod +x ~/.config/wezterm/agent-sidebar/run
```

The sidebar launcher (`agent-sidebar/run`) builds the Go binary when needed.

## 4) Start / reload WezTerm

- Close and reopen WezTerm, or run config reload if supported by your shell:

```bash
wezterm cli reload
```

## 5) Use the sidebar on your first run

In WezTerm, use WezTerm leader key + `a`:

- `Ctrl+Space`, then `a`

This does three things:

1. Builds `agent-sidebar/run` output to:
   `~/.cache/wezterm-agent-sidebar/codex-agent-sidebar` (first run only).
2. Spawns a floating sidebar window.
3. Shows running agents with active work info.

Use the same binding again to dismiss the sidebar.

## 6) Machine-specific path settings

`wezterm.lua` currently sets:

- `EWA_ROOT = '/Users/mogra/ewa-services'`

On a non-mogra machine, edit this value to match your local project root.

You can also simplify the file to keep only the sidebar-relevant bits if you
don’t want the workspace auto-layout logic.

## 7) Sync/update checklist

```bash
cd ~/.config/wezterm
git pull
```

Then restart WezTerm (or reload config) so updated Lua and sidebar binaries are used.

