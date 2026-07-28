# Changelog

All notable changes to this project will be documented in this file. The format
is based on Keep a Changelog, and the project follows Semantic Versioning.

## [Unreleased]

## [0.4.1] - 2026-07-28

### Changed

- Made resumed conversations replace the history browser by default so exiting
  Codex returns to the shell; the previous return-to-browser behavior remains
  available with `resume.mode = "return"`

## [0.4.0] - 2026-07-27

### Added

- Contributor issue forms, a code of conduct, and a synthetic TUI preview
- Nix, vulnerability, SBOM, and release provenance checks

### Changed

- Redesigned transcripts with left/right chat bubbles and centered turn and Activity rows
- Kept conversation-list headings, items, metadata, and search snippets aligned within the panel
- Expanded contribution, security, and development documentation for public collaboration
- Pinned GitHub Actions and patched Go toolchains, and synchronized the Nix package with the latest release

## [0.3.0] - 2026-07-26

### Added

- Relevance-ranked full-text results with highlighted match snippets
- Non-destructive project filtering by conversation working directory

### Changed

- Preserved literal substring behavior for short and syntax-like search terms
- Made explicit key bindings take precedence over colliding inherited defaults

## [0.2.0] - 2026-07-23

### Added

- Line-based transcript scrolling, half-page movement, and turn navigation
- Turn-level Activity inspector with event lists and separately scrollable details
- Configurable Activity and detail-view key bindings

### Changed

- Reworked transcripts around user messages and final Codex responses, with intermediate work summarized into compact Activity rows
- Made pane focus, compact-mode location, footer hints, and help contextual to the active view

## [0.1.0] - 2026-07-22

### Added

- Initial TUI for listing, reading, searching, and resuming Codex conversations
- Strict TOML configuration and customizable key bindings
- Local SQLite full-text index
- Doctor and configuration management commands
- GoReleaser, GitHub Actions, Homebrew tap, and Nix packaging foundations

[Unreleased]: https://github.com/HizKz/codex-history/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/HizKz/codex-history/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/HizKz/codex-history/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/HizKz/codex-history/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/HizKz/codex-history/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/HizKz/codex-history/releases/tag/v0.1.0
