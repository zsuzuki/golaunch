# AGENTS.md

## Project Overview

`golaunch` is a terminal UI launcher for registered CLI commands.

- Language: Go
- UI framework: Bubble Tea
- Config format: TOML
- Config path: `~/.config/golaunch/golaunch.toml`
- Main entrypoint: `main.go`

The app shows a left pane for command selection/editing and a right pane for the rendered command line plus process I/O.

## Current UI Spec

### List Screen

Displays:

- `Current Dir: <cwd>`
- Registered command titles

Behavior:

- `up/down` or `k/j`: move selection
- `enter`: open selected command in edit screen
- `r`: run selected command as-is
- `a`: add a new empty command and open edit screen
- `c`: clone the selected command and insert it directly below
- `d`: delete selected command with confirmation
- `q`: ask for quit confirmation
- `Ctrl+C`: quit immediately

Selection styling:

- Selected row uses a light blue background
- Selected row text uses a darker foreground color

### Edit Screen

Displays:

- `Title`
- `Command`
- `Args`

Behavior:

- `up/down` or `k/j`: move selection
- `enter`: edit selected field
- `r`: save and run
- `+`: add an empty argument
- When an argument row is selected, `+` inserts below that row
- `space`: toggle argument enabled state
- `Ctrl+D` or `del`: delete selected argument
- `esc`: return to list screen
- `q`: ask for quit confirmation

Notes:

- There is no `[Run]` button anymore
- Title, command, and args are edited inline through Bubble Tea text input
- New commands start empty

### Running Mode

While a command is running, normal navigation/editing is locked.

Allowed controls while running:

- `Ctrl+C` or `i`: send `SIGINT` to the running process
- `Ctrl+\\` or `K`: send `SIGKILL` to the running process

Behavior:

- The app stores the active process handle
- The process is started asynchronously
- Completion unlocks the UI and updates the output pane

## Output Pane Spec

The right pane shows:

1. `Command: <fully rendered command line>`
2. `I/O`
3. `stdout` / `stderr`

Notes:

- The command line display includes only enabled args
- Output rendering should respect pane width to avoid visual corruption

## Config File Shape

Current TOML structure:

```toml
[[commands]]
title = "List File"
command = "ls"

  [[commands.args]]
  value = "-l"
  enabled = true

  [[commands.args]]
  value = "."
  enabled = false
```

Data model in Go:

- `Config`
- `CommandDef`
- `Arg`

## Implementation Notes

- Keep the app as a single-process TUI managed from `main.go` unless there is a clear reason to split files
- Persist config changes to `~/.config/golaunch/golaunch.toml`
- Preserve current keybindings unless the user explicitly asks to change them
- Do not reintroduce the old `[Run]` row in the edit screen
- Keep list/edit behavior aligned with the README when changing UI behavior
- Be careful with running-state transitions; avoid letting normal key handling execute while `running == true`
- If changing process execution, preserve signal delivery support and process-group handling

## Verification

After code changes, run:

```bash
gofmt -w main.go
go build ./...
```

Update `README.md` as part of the same change when user-facing behavior changes.
