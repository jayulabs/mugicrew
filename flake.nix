{
  description = "mugicrew - agent crew dispatch CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # mugicrew CLI. CGO_ENABLED=0 per OE ADR 0007 (portable static binary; CGO
        # is only needed for embedded-Dolt, which is deferred). The binary
        # invokes itself by bare name — the TUI spawns `mugicrew signal triage`
        # and Nami's charting session runs `mugicrew reconcile` — so it must be
        # on PATH. A host OE consumes this flake with its nixpkgs/flake-utils
        # `follows`, so this flake.lock only pins a standalone build (ADR 0120).
        mugicrew = pkgs.buildGoModule {
          pname = "mugicrew";
          version = "0.0.0";
          # Exclude files the binary never reads, so a doc-only land keeps the
          # outPath and the pinned loop doesn't restart onto an identical
          # binary (ADR 0062). Exclusion, not an allowlist: missing a non-build
          # file costs one harmless restart; missing a go:embed input breaks
          # the build. New non-build files join this list; flake.nix and
          # flake.lock are here so editing them keeps the outPath too.
          src = pkgs.lib.fileset.toSource {
            root = ./.;
            fileset = pkgs.lib.fileset.difference ./. (pkgs.lib.fileset.unions [
              ./GLOSSARY.md
              ./README.md
              ./docs
              ./package.json
              ./flake.nix
              ./flake.lock
            ]);
          };
          # Bump flake.lock by hand when this changes; no guard keeps it in
          # step with a host OE's lock.
          vendorHash = "sha256-gvkb5aCMbac4KYvrFmFjqiZ4rXW+lTb6YO7MeJ+0LFo=";
          env.CGO_ENABLED = "0";
          # The git package's tests shell out to a real `git`; without it in the
          # check sandbox `go test ./...` fails and the build never produces a
          # binary.
          nativeCheckInputs = [ pkgs.git ];
          # contract.NewInvocation materializes the Crew docs under
          # os.UserCacheDir(); the sandbox HOME (/homeless-shelter) is unwritable.
          preCheck = ''export HOME="$TMPDIR"'';
        };
      in
      {
        packages.mugicrew = mugicrew;
        packages.default = mugicrew;
      }
    );
}
