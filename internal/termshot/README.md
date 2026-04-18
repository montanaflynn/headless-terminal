# internal/termshot — vendored ANSI→PNG rasterizer

This directory contains a minimal subset of
[homeport/termshot](https://github.com/homeport/termshot) — specifically its
`internal/img/output.go` file — vendored so `ht view --format png` can
rasterize terminal output without an external dependency.

## What's here

- `output.go` — termshot's `Scaffold` renderer, verbatim except for the
  package declaration (renamed `img` → `termshot` to suit our path). Feeds
  on an ANSI byte stream, emits a PNG via `fogleman/gg` + `freetype`. No
  WebAssembly, no resvg.
- `LICENSE` — termshot's original MIT license.

## Why vendored, not a dep

The upstream file lives at `internal/img/output.go` in termshot, so Go's
`internal/` visibility rule prevents an external module from importing it.
Forking the repo just to move that directory outward felt heavier than
copying one file with its license header preserved.

## Attribution

Copyright © 2020 The Homeport Team. MIT License (see `LICENSE`).
The transitive font (Hack) and rendering library licenses are surfaced
via the top-level `NOTICE` file at the repo root.

## Updating

If you pull a newer version of termshot upstream, re-copy from
`internal/img/output.go` at the desired tag, then re-apply the package
rename at the top. Keep changes minimal so future merges stay diffable.
