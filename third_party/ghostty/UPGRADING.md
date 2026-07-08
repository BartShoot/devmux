# Upgrading the vendored libghostty-vt

The committed artifacts here (`lib/**`, `include/**`) are built from the ghostty
source in `../ghostty-src` (a gitignored local checkout of
`https://github.com/ghostty-org/ghostty`).

## Current state

- **Headers (`include/`)**: synced from upstream commit **`91f66da2`** (2026, `main`).
- **Linux (`lib/linux/`)**: **rebuilt** from `91f66da2` with zig **0.15.2**. ✅ matches headers.
- **Windows (`lib/windows/`)**: ⚠️ **STALE** — still built from the previous commit
  (`bebca84`). It could NOT be cross-compiled from Linux (the `simdutf` / `highway`
  C++ dependencies fail for the `x86_64-windows-msvc` target). **The Windows lib must
  be rebuilt on a Windows machine (or an environment that can target MSVC) from the
  same commit before shipping a Windows build**, otherwise the Go/cgo code compiles
  against new headers but links an old ABI.

## How to rebuild

Requires zig **0.15.2** (`minimum_zig_version` in `../ghostty-src/build.zig.zon`).

```sh
cd third_party/ghostty-src
git fetch origin && git checkout 91f66da2   # or the desired commit

# Linux (native):
zig build -Demit-lib-vt=true -Doptimize=ReleaseFast

# Windows (run on Windows or an MSVC-capable env):
zig build -Demit-lib-vt=true -Doptimize=ReleaseFast -Dtarget=x86_64-windows
```

Then sync the artifacts + headers into this directory:

```sh
cd ../..                      # repo root
go run scripts/build_deps.go  # copies zig-out/{lib,bin,include} into third_party/ghostty/
```

`build_deps.go` skips the build if the target's output already exists, so delete the
relevant `lib/<target>/` output first to force a fresh copy.

## After upgrading

Rebuild and test the daemon (the only consumer of libghostty):

```sh
LD_LIBRARY_PATH=third_party/ghostty/lib/linux CGO_ENABLED=1 \
  go test -tags ghostty -race ./internal/terminal/
CGO_ENABLED=1 go build -tags ghostty ./cmd/...
```

Watch for ABI/API drift in the headers `internal/terminal/terminal.go` depends on:
`GhosttyTerminalOptions`, `GhosttyStyle`, `GhosttyColorRgb`, and the render-state
enums. cgo regenerates against the new headers, so a clean build is the signal.
