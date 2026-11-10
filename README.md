# herdr linear title

A [herdr](https://herdr.dev) plugin that renames worktree workspaces after the
Linear issue in their branch name.

```text
branch homeco-2290                        →  workspace "HOMECO-2290 testflight, google play console 빌드제출"
branch sungjunyoung/homeco-2291-login     →  workspace "HOMECO-2291 홈코데스크 로그인 페이지 개선"
```

- Runs automatically when herdr creates a worktree or reopens a closed one.
- Keeps labels you chose yourself (e.g. `herdr worktree create --label ...`).
  The manual actions overwrite them.
- Branches without a configured team key (`develop`, `main`) are ignored.
- On failure (issue not found, auth expired) the label is left alone and a
  herdr notification explains why.

## Requirements

- herdr 0.9.3 or newer, macOS or Linux (including WSL2).
- [Nix](https://nixos.org/download) with flakes. The plugin build runs
  `nix develop --command make build`, so no other toolchain is needed.
- A Linear workspace where you can either create an OAuth application (admins
  only) or a personal API key (admins, or members when allowed by an admin).

## Install

```sh
herdr plugin install sungjunyoung/herdr-linear-title
```

Then open the setup popup. herdr has no menu for plugin actions, so run this
from any pane inside herdr (or bind a key, see [Actions](#actions)):

```sh
herdr plugin action invoke sungjunyoung.linear-title.setup
```

The action opens the popup pane directly, which you can also do yourself:

```sh
herdr plugin pane open --plugin sungjunyoung.linear-title --entrypoint setup --focus
```

A herdr TUI client must be attached. Without one the command still answers
`ok`, but no popup appears.

The popup asks for:

1. **Team keys**: the issue prefix of your Linear teams, e.g. `HOMECO` for
   `HOMECO-2290`. Comma separated for several teams.
2. **Authentication**: OAuth (recommended) or a personal API key.

Setup verifies the credentials and writes `config.toml` to the plugin config
directory (`herdr plugin config-dir sungjunyoung.linear-title`).

### OAuth

Create an OAuth application once in Linear: *Settings → Administration → API →
OAuth applications → New*.

| Field | Value |
| --- | --- |
| Name | anything without the word "Linear", e.g. `herdr-linear-title` |
| Callback URLs | `http://localhost:53682/callback` |

Only the **client ID** is needed; the plugin uses PKCE, so never enter the
client secret. During setup a browser window opens for authorization. Tokens
are stored in the plugin state directory with `0600` permissions and refreshed
automatically. To use another port, register
`http://localhost:<port>/callback` and enter that port in setup.

On WSL2 the authorization page opens in the Windows browser and the callback
reaches the plugin through WSL's localhost forwarding.

### Personal API key

Create a key in *Settings → Security & access → Personal API keys* (read
access is enough) and choose "Personal API key" in setup.

## Actions

| Action | Effect |
| --- | --- |
| `sungjunyoung.linear-title.refresh` | Retitle the current workspace, overwriting its label |
| `sungjunyoung.linear-title.refresh-all` | Retitle every open worktree workspace |
| `sungjunyoung.linear-title.setup` | Open the setup / login popup |
| `sungjunyoung.linear-title.logout` | Revoke and delete the OAuth token |

herdr runs plugin actions only through keybindings or
`herdr plugin action invoke <action>`. Bind them in
`~/.config/herdr/config.toml`, then reload with `prefix+shift+r`:

```toml
[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "sungjunyoung.linear-title.refresh"
description = "refresh Linear title"
```

## Configuration

`config.toml` is written by setup and can be edited by hand:

```toml
team_keys = ["HOMECO"]
title_format = "{identifier} {title}"   # placeholders: {identifier}, {title}

[auth]
method = "oauth"                        # or "api_key" with api_key = "lin_api_..."
oauth_client_id = "..."
oauth_redirect_port = 53682
```

herdr's sidebar stops growing at `ui.sidebar_max_width` (36 columns by
default) and truncates longer labels with `…`. Raise it if you want to see
more of the title.

With saved SSH machines, the plugin runs on the machine whose herdr server
emits the event, so install and set it up on that machine.

## Herdr desktop app

The same titles work in the Herdr desktop app (`herdr-app`) through a mod in
`herdr-app/mods/linear-title`. From a checkout of this repository:

```sh
nix develop --command make install-app
```

This installs the binary to `~/.local/bin/herdr-linear-title` and copies the
mod into `~/.config/herdr-app/mods/linear-title`. Rerun it after changing the
mod.

herdr-app does not dock a new mod into the shipped `main` layout. To show it
in the footer, clone `main` (this switches to the clone):

```sh
herdr-app do layout.clone '{"name":"main","as":"linear"}'
```

Then in `~/.config/herdr-app/layouts/linear.json` replace the `status` block
with a row that holds it and the mod:

```json
{ "id": "footer", "type": "row", "size": { "px": 28 }, "resizable": false,
  "children": [
    { "id": "status", "type": "block", "content": { "mod": "workspace-status" } },
    { "id": "linear-title", "type": "block", "size": { "px": 420 },
      "content": { "mod": "linear-title" } } ] }
```

A cloned layout no longer gets Herdr's updates to `main`. To stay on `main`,
open the mod as a small kept window instead, which floats over the layout:
`herdr-app do mod.open --name linear-title`.

The footer shows the last result. When the last result is a missing config
or login, click it to run setup.

The app shares `config.toml` and the OAuth token with the TUI plugin, so one
setup serves both. Without the TUI plugin, run setup once from a terminal:

```sh
herdr-linear-title setup
```

The mod renames a space when its branch has an issue reference and the space
has no name yet, or still has the name the mod gave it. A branch switch
retitles it once herdr-app notices the new branch, which can take half a
minute. A name you set yourself is kept until you refresh.

Commands, bound in `~/.config/herdr-app/keys.json`:

```json
{ "global": { "cmd-alt-l": "linear-title.refresh" } }
```

| Command | Effect |
| --- | --- |
| `linear-title.refresh` | Retitle the focused space, overwriting its name |
| `linear-title.refresh-all` | Retitle every space on this computer, retrying failures |
| `linear-title.setup` | Open setup in a new tab |
| `linear-title.logout` | Revoke and delete the OAuth token |

Limits:

- Spaces on other machines are skipped. The mod runs the binary on this
  computer only.
- The mod runs only while the shown layout holds it (the `linear` footer or
  the kept window).

## Troubleshooting

```sh
herdr plugin log list --plugin sungjunyoung.linear-title
```

Every run logs what it did ("renamed to ...", "kept user label ...") or why it
failed.

## Development

```sh
direnv allow          # or: nix develop
make test lint
make link             # builds ./bin and links this checkout into herdr
```

`herdr plugin link` does not run `[[build]]`; run `make build` after changes.
