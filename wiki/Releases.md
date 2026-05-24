# Releases

The release workflow at `.github/workflows/release.yml` triggers on any pushed tag matching `v*` and produces a GitHub Release with binaries, a vsix per architecture, and a `checksums.txt`.

## Pipeline

```
push tag v* ──┬─► build (matrix: amd64, arm64) ───┐
              │      └─ ci + bongo-ls Linux binaries, tarballed
              │      └─ uploads bongo-ls for the vsix job
              │
              ├─► package-vsix (matrix: linux-x64, linux-arm64)
              │      └─ pulls bongo-ls artifact, runs `vsce package`
              │
              └─► release (after both)
                     └─ downloads all artifacts, sha256sums them, creates a GH release
```

## What gets shipped

| Artifact | From job |
| --- | --- |
| `bongoci-<version>-linux-<arch>.tar.gz` | `build` |
| `bongo-ls-<version>-linux-<arch>.tar.gz` | `build` |
| `vscode-bongo-<version>-<vsce_target>.vsix` | `package-vsix` |
| `checksums.txt` | `release` |

Tarballs contain a single binary (`ci` or `bongo-ls`) built with `-trimpath -ldflags="-s -w"` and `CGO_ENABLED=0`.

## Tagging conventions

- `vX.Y.Z` → stable release
- `vX.Y.Z-<anything>` (e.g. `v1.2.0-rc1`) → flagged `prerelease: true` (the workflow checks for `-` in `github.ref_name`)
- The vsix `version` is derived by stripping the leading `v` **and** any pre-release suffix, because `vsce` requires strict `x.y.z` SemVer. So `v1.2.0-rc1` packages as vsix `1.2.0`.

## How to cut a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `release` job runs `softprops/action-gh-release@v2` with `generate_release_notes: true`, so release notes are auto-generated from PRs/commits since the previous tag.
