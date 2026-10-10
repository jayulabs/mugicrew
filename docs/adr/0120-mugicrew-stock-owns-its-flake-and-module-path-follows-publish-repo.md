# ADR 0120: The Stock owns its flake, and the module path follows the publish repo

Status: Accepted
Date: 2026-10-10

## Context

A Projection carries only `apps/mugicrew/`, and it must build on its own.
Until now the build recipe was in the OE's root `flake.nix`: the fileset
exclusions, `vendorHash`, `CGO_ENABLED=0` and the test inputs. The old module
path, which named the owner's OE and its layout, was imported by about 100
files.

## Decision

1. **The Stock carries `apps/mugicrew/flake.nix`.** It exports only
   `packages.mugicrew` and `packages.default`. Its fileset excludes
   `GLOSSARY.md`, `docs`, `package.json`, `flake.nix` and `flake.lock`, so a
   doc-only or lock-only change keeps the outPath (ADR 0062). It sets
   `CGO_ENABLED=0`, `nativeCheckInputs = [ git ]` and a writable HOME for
   tests. The README gives `CGO_ENABLED=0 go build .` as a fallback for people
   without nix.
2. **The OE consumes the Stock's flake and does not copy it.** The root flake
   takes `path:./apps/mugicrew` as an input, with `nixpkgs` and `flake-utils`
   following its own, and re-exports `packages.mugicrew`. This keeps today's
   outPath exactly, so the cutover costs no restart.
3. **The pinned loop stays root-addressed.** `looplock` still builds
   `git+file://…?ref&rev#mugicrew` against the root flake, never
   `?dir=apps/mugicrew`. In an OE, the OE pins nixpkgs. The Stock's
   `flake.lock` serves only a standalone build. The dev wrapper and the
   `sourceRoot` stamp stay OE-side.
4. **No check keeps the two locks in step.** A standalone build and an in-OE
   build share an outPath only when both locks pin the same nixpkgs. Bump the
   Stock's lock by hand when you touch `vendorHash`.
5. **The module path is the publish repo's path.** The `go.mod` module equals
   the host/path of the public repo the Stock is published to, so
   `go install <path>@latest` works and the two cannot drift. The rename is a
   single mechanical commit (`go.mod`, import lines, prose mentions) that
   lands before the first Publish. It costs one ordinary restart.

## Considered Options

- **Plain `go build` only, with the build knobs in README prose:** rejected.
  `CGO_ENABLED=0` is a requirement, so it belongs in code.
- **Loop flakeRef with `?dir=apps/mugicrew`:** rejected. It hands the OE's
  nixpkgs pin to the Stock's lock and changes the outPath.
- **A vanity module path on the owner's domain, or a bare `mugicrew`:**
  rejected. The vanity path needs `go-import` hosting. A bare path cannot be
  installed remotely.
