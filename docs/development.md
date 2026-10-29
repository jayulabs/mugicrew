# Developing mugicrew

## Build

```bash
nix build ./apps/mugicrew#mugicrew   # from the OE root
nix build .#mugicrew                 # from apps/mugicrew/
```

Without nix, build it from `apps/mugicrew/`:

```bash
CGO_ENABLED=0 go build .
```

`mugicrew` invokes itself by bare name, so put the binary on `PATH`.

## Test

From `apps/mugicrew/`:

```bash
CGO_ENABLED=0 go test ./...
```

The `git` package's tests need `git` on `PATH`.
