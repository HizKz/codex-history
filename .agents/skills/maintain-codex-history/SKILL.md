---
name: maintain-codex-history
description: Route and maintain changes in the codex-history Go TUI repository. Use for any implementation, bug fix, refactor, compatibility update, test, documentation, packaging, or release task in this repository, especially work involving Codex app-server, transcript privacy, SQLite search, TOML configuration, keybindings, Bubble Tea UI, resume handling, Nix, Homebrew, or GoReleaser.
---

# Maintain Codex History

Treat the root `AGENTS.md` as the source of truth for project invariants, change
rules, verification, and completion. Inspect the working tree, then load only
the code, tests, and documentation relevant to the requested change.

## Route the change

- App-server protocol, lifecycle, or resume handling: inspect
  `internal/appserver`, the installed Codex schema, and fake-server tests.
- Transcript normalization, privacy, or search: inspect `internal/history`,
  `internal/index`, their tests, and `docs/architecture.md` as applicable.
- Configuration or keybindings: inspect `internal/config`, the embedded and
  example TOML files, validation, and key-conflict tests.
- TUI interaction or layout: inspect `internal/tui` and cover compact, wide,
  and full-width Unicode rendering as applicable.
- CLI behavior: inspect `cmd/codex-history` and its command tests.
- Packaging or release work: read `docs/releasing.md` before inspecting Nix,
  GoReleaser, or workflow definitions.

## Follow specialized workflows

Generate the current stable schema into a temporary directory:

```sh
schema_dir="$(mktemp -d)"
codex app-server generate-json-schema --out "$schema_dir"
```

For protocol changes, compare exact request, response, enum, and item fields
before editing Go types, and keep fake-server lifecycle tests deterministic.

For configuration changes, update the typed structure, embedded defaults,
documented example, validation or conflict detection, and behavior tests
together. Treat a `config_version` change as an explicit migration decision.
