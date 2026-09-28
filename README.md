# ws

Open tmux workspaces from reusable YAML layouts.

A layout describes windows as nested columns and rows sized in percentages,
so it fits whatever terminal you open it in. Any column, row or window can
repeat once per git worktree, which makes "one column per checkout" a single
template you apply to any repo. Extra worktrees can come from a
treehouse pool, leased when the
session starts and returned when it stops.

```
ws                      picker: running sessions and projects
ws start [target]       start (or top up) a session and attach
ws stop [target]        kill it, run on_stop, return its worktrees
ws ls                   projects, state and leased worktrees
ws capture --as NAME    save the current session as a layout
ws import FILE          convert a tmuxinator project
ws new NAME / ws edit NAME
ws check                validate every project and layout
```

`target` is a project name, a directory, a running session or a zoxide query,
tried in that order. With no target, `start` uses the current directory and
`stop` the current session.

## Install

The repo is private, so installs go through an authenticated `gh`. Every
push to `main` publishes binaries to the rolling `latest` release:

```bash
gh release download latest -R AugustDG/ws -p "ws-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" \
  -O ~/.local/bin/ws --clobber && chmod +x ~/.local/bin/ws
```

The `ws` module in the dotfiles repo runs the same thing on install. From a
checkout, `go install ./cmd/ws` works too.

Open the picker in a popup with a tmux binding:

```tmux
bind f display-popup -E -w 70% -h 60% "ws"
```

## Config

Everything lives in `~/.config/ws` (or `$WS_CONFIG_DIR`):

```
config.yaml           settings, optional
projects/NAME.yaml    one per project
layouts/NAME.yaml     reusable layouts
```

A repo can also carry its own `.ws.yaml`. When it exists it's used instead of
`projects/NAME.yaml` for that root.

### Layouts

```yaml
# layouts/grid.yaml
windows:
  - name: code
    columns:
      - for_each: worktree   # one column per worktree
        rows:
          - size: 70%
            cmd: nvim
          - {}                # takes the remaining 30%
  - name: misc
    dir: "~"
    columns:
      - size: 60%
      - {}
```

A window or node is a pane unless it has `columns` or `rows`. Fields:

| field      | meaning |
|------------|---------|
| `name`     | window name (windows only) |
| `columns` / `rows` | split side by side / stacked; nest freely |
| `size`     | share of the parent split, like `70%`. Unset siblings split what's left |
| `dir`      | start dir. Relative to the parent's dir; `~` and absolute paths work |
| `cmd`      | a command or list of commands typed into the pane |
| `focus`    | select this pane (or window) after starting |
| `for_each` | `worktree`: repeat once per worktree. Inside it, relative dirs are relative to that worktree |
| `layout` + `panes` | instead of splits, a flat pane list arranged by a tmux preset (`tiled`, `main-vertical`, ...) |

Names, dirs and commands can use `{project}`, `{root}`, `{path}` (the current
worktree), `{name}` (the worktree's name, `main` for the root) and `{index}`
(0 for the root). A `for_each` window without a placeholder in its name gets
`-{index}` appended.

The `default` layout is used for directories without a project and for
projects that don't name one. Without a `layouts/default.yaml` it's a single
pane.

### Projects

```yaml
# projects/viber.yaml
root: ~/projects/viber
layout: grid            # or inline `windows:` in the layout format
worktrees:
  source: treehouse     # or git
  count: 2
env:
  NODE_ENV: development
on_start: docker compose up -d
on_stop: docker compose down
```

The worktree list always starts with the project root. Sources:

- `treehouse` leases `count` worktrees with `treehouse get --lease`. Leases are
  recorded in `~/.local/state/ws` and reused on the next start while
  treehouse still shows them as ours. `ws stop` returns them, and
  `ws stop --keep-worktrees` keeps them for next time.
- `git` uses the repo's existing linked worktrees (`git worktree list`),
  up to `count` if set. It doesn't create or remove anything.

`on_start` runs in the root before a new session is built. `on_stop` runs
after a running session is killed.

A running session never gives up worktrees: lowering `count` takes effect
after the next `ws stop`.

### Settings

```yaml
# config.yaml
default_layout: default   # layout for directories and projects that name none
```

## Behavior worth knowing

- `start` on a running session only adds windows it's missing, matched by
  name. It never rearranges existing ones.
- A new session is created at the current terminal size so percentage splits
  land where they should before you attach.
- A directory without a project file is named after its basename. If that
  name is already taken by another directory's session or project, it
  becomes `parent-basename`.
- `ws stop` from inside the session it's stopping hands the work to the tmux
  server (`run-shell -b`), since killing the session would kill `ws` first.
  Output goes to `~/.local/state/ws/stop.log`.
- `capture` and `import` turn columns or rows that repeat across worktrees of
  one repo into `for_each: worktree`, set the project root to the main
  checkout and infer the source (`treehouse` for pool paths, else `git`).
  `--no-generalize` keeps them concrete. A later window that repeats over
  different worktrees than the first stays concrete too.
- `$WS_TMUX_SOCKET` points ws at a named tmux server (`tmux -L`), and
  `$WS_STATE_DIR` overrides the state directory.

## Development

```bash
go test ./...
```

The engine tests start a private tmux server and skip when tmux isn't
installed.

| package | job |
|---------|-----|
| `internal/layout` | YAML model, sizing, expansion into a pane tree |
| `internal/engine` | builds a pane tree in tmux |
| `internal/tmux` | tmux CLI wrapper and layout-string parser |
| `internal/project` | config dir: settings, projects, layouts |
| `internal/worktree` | worktree sources (`Source` interface, registered by name) |
| `internal/workspace` | start/stop: ties config, worktrees, engine and state together |
| `internal/state` | per-session lease records |
| `internal/capture` | concrete windows to layout YAML, worktree detection |
| `internal/tmuxinator` | tmuxinator file parser |
| `internal/discover` | picker sources (`Source` funcs) and merging |
| `internal/picker` | Bubble Tea picker |
| `cmd/ws` | one file per command |

To add a worktree source, implement `worktree.Source` and add it to the
`sources` map in `internal/worktree/worktree.go`. To add a picker source,
write a `discover.Source` and include it in `items` in `cmd/ws/pick.go`.
