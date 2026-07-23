# Changelog

All notable changes to this project will be documented in this file. The format
is based on Keep a Changelog, and the project follows Semantic Versioning.

## [Unreleased]

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

[Unreleased]: https://github.com/HizKz/codex-history/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/HizKz/codex-history/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/HizKz/codex-history/releases/tag/v0.1.0
