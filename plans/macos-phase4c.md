# macOS port, Phase 4c: signed CI releases, Nix flake, home-manager module — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A tagged release (`vX.Y.Z`) makes GitHub Actions build `Wimy.app`, sign it with the same `wimy-dev` identity as local builds, and publish it on GitHub Releases. The repo's flake installs that prebuilt, signed app on macOS through a home-manager module, which updates wimy in place (state kept, Accessibility permission kept) on `darwin-rebuild switch`. Linux gets a from-source package.

**Architecture:**
- **Release workflow** (`.github/workflows/release.yml`, tag push `v*`, macOS arm64 runner, GitHub *environment* `release` holding the signing secrets — the job can't read them without passing the environment's protection rules). It imports the `.p12` into a throwaway keychain (`contrib/macos/ci-import-identity.sh`), runs `make mac-app VERSION=<tag>`, zips with `ditto -c -k --keepParent` (keeps the signature), and attaches `Wimy-<version>-arm64.zip` plus its `.sha256` to the release.
- **Same identity everywhere:** CI signs with the certificate the user exported from their `wimy-dev` identity, so CI builds, local builds and every Mac share one designated requirement (`identifier "io.github.jaym.wimy" and certificate leaf = H"e511a1df…"`) and one Accessibility grant.
- **Flake** (`flake.nix`): `packages.<linux>.wimy` from source (`buildGoModule`, `CGO_ENABLED=0`), `packages.aarch64-darwin.wimy` = the prebuilt signed app from `nix/release.json` (version, url, hash), with `dontFixup`/`dontStrip` so nothing touches the signed binaries; `packages.aarch64-darwin.wimy-src` for building from source (ad hoc signed — needs re-approval per change). `homeManagerModules.wimy`.
- **home-manager module** `programs.wimy`: `enable`, `package`, `settings` (a KDL string or path → `~/.config/wimy/config.kdl`, optional), `installDir` (default `~/Applications`). On macOS its activation copies the app out of the store to `installDir/Wimy.app` (writable copy, signature intact), links `wimyctl` into `~/.local/bin`, then `wimyctl restart` if wimy runs (the restart preflight protects against a broken build) or `open`s it. Login start stays wimy's own `start-at-login` (SMAppService), same as a `make mac-install` install. On Linux it only installs the package and config.
- **`nix/update-release.sh vX.Y.Z`** fetches the release asset, prefetches it into the store, and rewrites `nix/release.json`.

**Spec:** `plans/macos-port.md` "Packaging: Nix flake + nix-darwin module" (deviation: home-manager module + prebuilt signed release instead of a nix-darwin launchd agent and an ad-hoc store build — keeps the permission and the in-place restart).

## Global Constraints

- The private key never enters the repo, the Nix store, or logs; CI keychain and files are deleted in an `always()` step.
- The signing secrets live in the `release` environment; the workflow runs only on tag pushes (no `pull_request` trigger).
- Nix must not modify the signed app (`dontFixup`); verify `codesign --verify --strict` on the store copy and the installed copy.
- Linux: the from-source package builds with `CGO_ENABLED=0`; existing CI unchanged.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Tasks

1. **Bundle version** — `Makefile`: `CFBundleVersion` = numeric dotted form (tag without `v`; `0.0.0` for non-tag builds), `CFBundleShortVersionString` = the full version; a `/`-safe substitution. Check with `make mac-app VERSION=v0.1.0` + `plutil -p`.
2. **Release workflow** — `contrib/macos/ci-import-identity.sh` (create keychain, import p12 with `-T /usr/bin/codesign`, `set-key-partition-list` so codesign never prompts, trust the certificate for code signing, add to the search list) + `.github/workflows/release.yml` + `contrib/macos/export-signing-identity.md` instructions (export the p12, `gh secret set --env release`, create the environment). `bash -n`, `actionlint` if available.
3. **Flake packages** — `flake.nix`, `nix/release.json`, `nix/package-darwin.nix`, `nix/package.nix` (source). Check: `nix flake check`, `nix build .#wimy-src` on darwin (source build; proves `vendorHash`), `nix eval .#packages.x86_64-linux.wimy.name`.
4. **home-manager module** — `nix/hm-module.nix`; a flake check that evaluates a minimal home-manager configuration using it (options type-check, activation script contains the install steps).
5. **Update script** — `nix/update-release.sh`.
6. **Live** (with the user): create the `release` environment and secrets, tag `v0.1.0`, watch CI, `nix/update-release.sh v0.1.0`, verify the downloaded app's designated requirement equals the local one, wire into `~/nix-config` (flake input + module, drop the manual `wimy` config link if the module manages the config — ask), `darwin-rebuild switch`, confirm wimy restarted in place with the permission kept. Docs.
