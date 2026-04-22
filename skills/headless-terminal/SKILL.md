---
name: headless-terminal
description: Drive interactive terminal programs (vim, emacs, nethack, htop, CLI installers, REPLs, anything curses-based) headlessly via the `ht` CLI. Use when the task needs a program that expects a real TTY — won't run under plain pipes, draws to an alternate screen, or needs keystrokes like arrow keys, Ctrl-C, or function keys. Do not use for non-interactive commands that work fine with plain shell pipes.
license: MIT
compatibility: Requires the ht CLI (v0.1.0+) on macOS (Apple Silicon) or Linux.
metadata:
  author: Montana Flynn
  version: "1.0"
  homepage: https://github.com/montanaflynn/headless-terminal
---

# ht — headless terminal

`ht` is a daemon that owns a pseudo-terminal per session and parses output with the same VT engine Ghostty uses. You can launch a TUI, send keystrokes, snapshot the rendered screen, and block until a screen condition is met.

## Install ht

Check if the CLI is on `PATH`; if not, install it:

```sh
command -v ht || brew install montanaflynn/tap/ht
```

Without Homebrew, grab a tarball from the [releases page](https://github.com/montanaflynn/headless-terminal/releases) and move the binary onto `PATH`. macOS (Apple Silicon) and Linux (x86_64/arm64) only.

## When to reach for this

- Target program draws to the alternate screen / uses (n)curses / reads `$TERM`.
- Needs real keystrokes (arrow keys, `<C-c>`, `<F5>`), not just stdin text.
- You need to inspect the *rendered* screen state, not the raw output byte stream.

If the program works with `echo ... | cmd`, use a plain pipe — don't reach for ht.

## Core workflow

```
ht run --name S <cmd...>           # launch, prints session ID
ht send S "<keys>"                 # type into it
ht view S                          # snapshot screen (plain text)
ht wait S --text "READY"           # block until a condition
ht stop S && ht remove S           # cleanup
```

`ht run` prints a short hex session ID. Use `--name` to label it and reference the session by that name everywhere else — it's easier than juggling IDs.

## The one thing agents get wrong

Keystrokes reach the PTY before the program has finished rendering a response. **Do not `view` immediately after `send`** — you'll snapshot a stale screen.

Fold the wait into the send:

```
ht send S --wait-text "Saved" "ihello<Esc>:wq<CR>"
ht send S --wait-idle 200ms --view "q"
```

Picking the right wait flag is the hard part — see `references/waits.md` before writing sends for a new TUI.

## Quick pointers

- **Key notation** (vim-style `<C-c>`, `<CR>`, `<F1>`, …): `references/keys.md`
- **Wait strategies** (idle vs text vs cursor vs change vs duration): `references/waits.md`
- **Recipes** (edit-a-file, REPL, installer, watch-live, extract text): `references/recipes.md`
- **Troubleshooting** (exit codes, timeouts, stuck sessions): `references/troubleshooting.md`

## Output formats

- `ht view --format plain` (default) — text grid with a trailing `cursor: R,C`
  line. Use the cursor to disambiguate same-glyph entities (e.g. two `@`s in
  nethack: the cursor sits on you).
- `ht view --format ansi` — preserves colors/styles; good for showing the user.
  Also appends the `cursor:` line.
- `ht view --format html` — embeddable; no cursor line (would break the doc).
- `ht view --format png --output FILE` — rasterized screenshot with window
  chrome. Use for demos, bug reports, or to let yourself *see* a session
  (Claude can read PNGs, can't render SVG).
- `ht view --json` — structured (cursor position, size, text).

## Cleanup

Sessions persist across the daemon's lifetime. Always `ht stop <sid>` + `ht remove <sid>` when done, or the daemon accumulates zombie records. `ht list` shows what's live.
