# codex-history

`codex-history` is a fast, local-first TUI for browsing, searching, and resuming
[Codex CLI](https://developers.openai.com/codex/cli/) conversations.

The application talks to the supported Codex app-server protocol instead of
parsing private rollout files. Conversation content stays on your machine; the
optional full-text index is a local SQLite database.

## Features

- Two-pane conversation list and transcript viewer, with a compact single-pane layout
- Full-text search across user and assistant messages
- Display of plans, reasoning summaries, commands, file changes, MCP calls, and other tool activity
- Resume a selected conversation with `codex resume <thread-id>`
- Strict, versioned TOML configuration with fully customizable key bindings
- Source and archived-conversation filters
- macOS and Linux support, designed for Homebrew and Nix distribution

## Install from source

Requirements: Go 1.25 or newer and a working `codex` executable.

```sh
go install github.com/HizKz/codex-history/cmd/codex-history@latest
```

The Homebrew tap, nixpkgs package, and prebuilt release archives will be added
after the first tagged release.

## Usage

```sh
codex-history
codex-history --reindex
codex-history doctor
codex-history config init
codex-history config check
```

Default keys include:

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move through conversations or transcript items |
| `tab` | Switch pane focus |
| `/` | Full-text search |
| `enter` | Resume the selected conversation |
| `space` | Expand or collapse tool details |
| `r` | Refresh conversations |
| `R` | Rebuild the search index |
| `ctrl+r` | Reload configuration without restarting |
| `s` / `a` | Toggle all sources / archived conversations |
| `?` | Show configured keys |
| `q` | Quit |
| `ctrl+c` | Emergency exit (always reserved) |

## Configuration

The default path follows the operating system convention:

- macOS: `~/Library/Application Support/codex-history/config.toml`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/codex-history/config.toml`

`CODEX_HISTORY_CONFIG` or `--config` can select another file. Create a fully
commented starting point with:

```sh
codex-history config init
```

Configuration is strict: unknown fields, invalid colors, unsupported values,
and conflicting active key bindings are reported before the TUI starts. Missing
values inherit the embedded defaults. To define a keymap from scratch, set
`keys.use_defaults = false`.

See [examples/config.toml](examples/config.toml) for every option.

The local index is stored below the OS cache directory. Command output, MCP
arguments/results, and other expandable tool details are intentionally excluded
from full-text indexing. Use `--no-cache` for an in-memory index.

## Development

```sh
go test ./...
go vet ./...
staticcheck ./...
nix build --no-link
```

Release metadata is injected with Go linker flags; snapshots can be built with
GoReleaser.

## Privacy and compatibility

`codex-history` launches `codex app-server --listen stdio://` and uses the
versioned `thread/list` and `thread/read` APIs. It does not upload conversation
data. The SQLite cache contains searchable conversation text and is created with
user-only permissions where supported.

This is a community project and is not an official OpenAI product.

## License

MIT — see [LICENSE](LICENSE).
