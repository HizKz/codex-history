# Releasing

Releases are manual at the tag boundary and automated after it. Pushing a valid
`v*` tag from `main` verifies the release, publishes it with GoReleaser, updates
the Homebrew Cask, and checks the installed Cask on a macOS runner.

## Prerequisites

- A clean `main` branch with successful GitHub Actions.
- A currently supported Go 1.25 patch release, Nix, GoReleaser, and Syft
  available. GitHub Actions pins the release toolchain to Go 1.25.12. The Nix
  development shell provides all other release tools.
- The `HizKz/homebrew-tap` repository exists.
- `HOMEBREW_TAP_GITHUB_TOKEN` is configured as a repository Actions secret. Use
  a fine-grained personal access token scoped only to `HizKz/homebrew-tap` with
  repository Contents read/write permission.

## Prepare

1. Choose a SemVer version and move the relevant entries from `[Unreleased]` to
   a dated version section in `CHANGELOG.md`. Add its comparison link at the
   bottom of the file.
2. Update `package.nix` and its linker-injected version to the exact release
   version. Confirm the Go module remains `github.com/HizKz/codex-history`.
3. Run:

   ```sh
   go test -race ./...
   go vet ./...
   staticcheck ./...
   govulncheck ./...
   nix build --no-link 'path:.#codex-history'
   nix develop 'path:.' -c goreleaser check
   ```

4. Review the full diff and confirm no conversation data or local paths were added.

## Tag and publish

Pushing the tag is the release approval. Only do this after explicit
authorization:

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The workflow rejects tags that are not SemVer-shaped, do not point to a commit
contained in `main`, do not have a dated changelog section and release link, or
do not match the Nix package version. It then runs formatting and module checks,
race tests, vet, staticcheck, and `goreleaser check` before using either
publishing token.

For a stable tag, GoReleaser builds CGO-free Darwin and Linux archives for amd64
and arm64, writes `checksums.txt` and per-archive SPDX JSON SBOMs, publishes a
non-draft GitHub release, and updates the Homebrew Cask. GitHub Actions attaches
build-provenance attestations to the archives, checksums, and SBOMs. Users can
verify a downloaded artifact with:

```sh
gh attestation verify PATH/TO/ARCHIVE -R HizKz/codex-history
```

A final macOS job installs
`HizKz/tap/codex-history` and confirms that `codex-history --version` matches
the tag.

A tag such as `vX.Y.Z-rc.1` is published automatically as a GitHub prerelease.
It does not update the stable Homebrew Cask and skips the Homebrew installation
check.

## macOS trust

Release archives are not yet signed with an Apple Developer ID or notarized.
The Homebrew Cask removes the quarantine attribute only from its staged
`codex-history` binary so the unsigned community build can run. It must not
apply recursive attribute changes to a containing directory. Developer ID
signing and notarization are tracked separately and require dedicated Apple
credentials and repository secrets.

## Failure recovery

- Validation failures have no release or tap side effects. Fix `main` and use a
  new version tag. Do not force-move a published tag.
- If a transient service failure occurs before publication, rerun the failed
  workflow job only after confirming that no conflicting release assets were
  created.
- If the GitHub release exists or the Homebrew smoke test fails, do not replace
  its artifacts or roll back the tag automatically. Correct the tap when the
  release artifacts are valid, or publish the fix as a new patch release.
- Never delete, move, or republish a release tag without explicit authorization.

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
