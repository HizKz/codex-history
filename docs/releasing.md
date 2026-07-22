# Releasing

Releases are intentionally manual at the approval boundary. Pushing a `v*` tag
starts GoReleaser, but the GitHub release remains a draft for review.

## Prerequisites

- A clean `main` branch with successful GitHub Actions.
- Go, Nix, and GoReleaser available.
- The `HizKz/homebrew-tap` repository exists.
- `HOMEBREW_TAP_GITHUB_TOKEN` is configured as a repository Actions secret with
  Contents read/write permission scoped to the tap repository.

## Prepare

1. Choose a SemVer version and update `CHANGELOG.md`.
2. Confirm the Go module remains `github.com/HizKz/codex-history`.
3. Run:

   ```sh
   go test -race ./...
   go vet ./...
   staticcheck ./...
   nix build --no-link 'path:.#codex-history'
   nix develop 'path:.' -c goreleaser check
   ```

4. Review the full diff and confirm no conversation data or local paths were added.

## Tag and publish

Only after explicit authorization:

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow builds CGO-free Darwin and Linux archives for amd64 and
arm64, writes checksums, creates a draft GitHub release, and updates the
Homebrew Cask. Review artifact names, checksums, generated Cask content, and
installation on at least one supported platform before publishing the draft.

## Nixpkgs follow-up

The checked-in `package.nix` supports the local flake. A nixpkgs submission must
fetch the tagged GitHub source and use fixed source and vendor hashes for that
release. Add new packages below
`pkgs/by-name/co/codex-history/package.nix`, and add a maintainer entry in a
separate commit when the maintainer is not already registered.

The upstream package should wrap `codex-history` with the nixpkgs `codex`
package on `PATH`, while preserving `--codex-bin` and `codex.binary` overrides.
Before opening the upstream pull request, run the maintainer test, build the
package, check `codex-history --version` and `--help`, and run
`nixpkgs-review wip` on an available Darwin or Linux platform.

Use these upstream commit subjects:

```text
maintainers: add HizKz
codex-history: init at X.Y.Z
```

Never reuse `lib.fakeHash` in a submitted package and never publish a tag merely
to discover release hashes without the user's approval.
