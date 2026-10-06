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

Then, inside the herdr TUI, run the **Linear: setup / login** action. It opens
a popup that asks for:

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

Bind them in `~/.config/herdr/config.toml`:

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
