# Contributing

Issues and pull requests are welcome. Please keep changes focused and include
tests for behavior changes.

## Development setup

Install Go 1.25 or newer. Nix is optional for normal Go development and required
only for validating the Nix package. A working Codex CLI is needed for live
compatibility checks, but the automated test suite uses synthetic data and a
fake app-server.

Start with:

```sh
go test ./...
go run ./cmd/codex-history --help
```

Keep blocking I/O outside Bubble Tea `Update`, preserve `ctrl+c` as the
emergency exit, and keep the build CGO-free. Public CLI flags and configuration
semantics should remain compatible unless a change is explicitly documented as
breaking.

## Privacy

Conversation data must stay on the user's machine. Never add real conversation
content, credentials, usernames, absolute home-directory paths, command output,
MCP arguments or results, or other personal data to fixtures, issues, logs, or
pull requests. Use short synthetic examples.

Search indexing must continue to exclude command output, MCP payloads, tool
results, and raw expandable details.

## Protocol changes

Use `codex app-server`; do not parse private rollout JSONL or Codex SQLite
files. Before changing protocol-facing fields, generate a schema from the
installed Codex CLI:

```sh
schema_dir="$(mktemp -d)"
codex app-server generate-json-schema --out "$schema_dir"
```

Compare exact method, request, response, enum, and item fields, then update the
deterministic fake-server tests without copying real history.

## Configuration and keys

Configuration changes must update the Go structure, embedded `default.toml`,
`examples/config.toml`, validation, and tests together. Key changes must check
conflicts in every active scope and may not rebind `ctrl+c`.

## Before opening a pull request

Before submitting a pull request, run:

```sh
gofmt -w cmd internal
go mod tidy -diff
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
nix build --no-link 'path:.#codex-history'
```

Run the Nix check when Nix is available, and document any skipped or additional
checks in the pull request. Packaging and release changes should also pass
`nix develop 'path:.' -c goreleaser check`.

By submitting a contribution, you agree that it is provided under the
repository's [MIT License](LICENSE).
