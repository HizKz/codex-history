---
name: maintain-codex-history
description: Maintain the codex-history Go TUI and its release definitions. Use when changing Codex app-server history ingestion, transcript normalization, SQLite search, TOML configuration or keybindings, Bubble Tea UI behavior, resume handling, Nix/Homebrew packaging, release automation, or compatibility with a new Codex CLI version.
---

# Maintain Codex History

Follow the root `AGENTS.md` throughout the task. Read only the relevant package,
tests, and documentation before editing.

## Classify the change

- App-server lifecycle or protocol: inspect `internal/appserver` and the installed Codex schema.
- Transcript items or privacy: inspect `internal/history` and `docs/architecture.md`.
- Search behavior or cache schema: inspect `internal/index` and its tests.
- Configuration or keys: inspect `internal/config`, both TOML files, and key-conflict tests.
- Interaction or layout: inspect `internal/tui`; test compact and wide layouts and full-width text.
- CLI behavior: inspect `cmd/codex-history` and preserve exit/error behavior.
- Packaging or publishing: read `docs/releasing.md` before changing Nix, GoReleaser, or workflows.

## Work safely

1. Inspect `git status` and isolate the requested scope.
2. Preserve app-server, local-only data, indexing, and emergency-exit invariants from `AGENTS.md`.
3. Make the smallest coherent implementation and update tests in the same change.
4. Update configuration examples or user documentation when behavior is visible.
5. Run the verification ladder below and inspect the final diff.

## Verify protocol changes

Generate the current stable schema into a temporary directory:

```sh
schema_dir="$(mktemp -d)"
codex app-server generate-json-schema --out "$schema_dir"
```

Compare exact request, response, enum, and item fields before editing Go types.
Keep the fake app-server lifecycle test deterministic. When useful and
authorized, run `doctor --json --no-cache` against the installed Codex CLI, but
never capture real thread content.

## Update configuration atomically

Change all of these together:

1. The typed config structure.
2. `internal/config/default.toml`.
3. `examples/config.toml`.
4. Strict validation and conflict detection.
5. Merge, rejection, and behavior tests.

Retain old behavior for omitted fields. Treat `config_version` changes as an
explicit migration decision rather than an incidental edit.

## Preserve search privacy

Index user/assistant text and safe descriptive metadata. Do not add command
output, MCP payloads, tool results, or raw expandable details to
`history.Transcript.Body`. Keep query wildcard escaping and cache permissions
covered by tests.

## Verification ladder

Start with formatting and focused package tests, then run:

```sh
go mod tidy -diff
go test ./...
go vet ./...
```

Run `go test -race ./...` for concurrency or process-lifecycle changes. Run
`staticcheck ./...` when available. For packaging or release changes, also run:

```sh
nix build --no-link 'path:.#codex-history'
nix develop 'path:.' -c goreleaser check
```

Do not commit, push, tag, publish, or modify the Homebrew tap unless the user
explicitly asks for that external action.
