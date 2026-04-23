<h1 align="center">headless terminal</h1>

<p align="center">
  <a href="#examples">Examples</a> ·
  <a href="#install">Install</a> ·
  <a href="#agent-skill">Agent Skill</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#architecture">Architecture</a>
</p>

<p align="center">
  <img src="screenshot.png" alt="Claude Code driving nvim via ht">
</p>

Puppeteer for terminal UIs. Drive `vim`, `emacs`, `htop`, `nethack`, or any
other interactive TUI from a CLI (or an AI agent) — spawn the program in a
background session, send keystrokes, snapshot the screen, and watch the whole
thing live from another shell.

Under the hood: a Unix-socket daemon owns a pseudo-terminal per session and
pipes its output through [libghostty-vt](https://github.com/ghostty-org/ghostty)
for VT parsing. You get the terminal emulation used by the Ghostty app, in a 
detached, scriptable, snapshot-able form.

## Why

Interactive TUIs assume a human at a keyboard. If you want an agent — or a
test, or a debugger, or a recorder — to drive one, you need three things the
shell doesn't give you:

- A pseudo-terminal so the TUI thinks it's attached to a real tty.
- A VT parser that tracks cursor position, scrollback, styles, etc. from the
  TUI's output stream.
- Synchronization primitives so the driver knows when the TUI has finished
  redrawing.

`ht` is those three things, wrapped in a simple CLI with a daemon.

## Examples

**Agentic coding.** Let an agent drive interactive CLIs it otherwise can't —
  `git add -p`, `gh auth login`, `create-next-app`, REPLs, debuggers, even
  `vim` for surgical edits.

```shell
# Start a headless vim session, returns a short session ID.
ht run --name notes vim /tmp/notes.md

# Drive it. Keys use vim-style notation (<CR>, <Esc>, <C-c>, <F1>, …).
ht send notes "ihello from an agent<Esc>:wq<CR>" --view # return view

# Session exited and the file is saved:
cat /tmp/notes.md
# → hello from an agent

# Remove the session completely
ht remove notes
```

**CI tests for TUIs.** Script an interactive program in GitHub Actions:
  run it, send keys, assert against the rendered screen (as text or a PNG
  snapshot). Covers paths `expect`/`pexpect` can't — alternate screen,
  colors, cursor position.

```shell
# Boot a TUI, send keys, fail the build if the screen doesn't match.
ht run --name smoke vim /tmp/demo.md
ht send smoke "ihello from CI<Esc>" --wait-idle 200ms
ht view smoke | grep -q "hello from CI" || { echo "render failed"; exit 1; }
ht send smoke ":q!<CR>"
```

**Demo and doc generation.** Record keystroke-perfect asciicasts with
  `ht record`, render to GIF via `agg`, or grab a one-shot PNG of the
  current frame for a README or bug report.

```shell
# Drive a session, then grab a PNG of the current frame for a README or bug report.
ht run --name demo bash
ht send demo "echo 'headless terminal'<CR>" --wait-idle 200ms
ht view demo --format png > screenshot.png
ht stop demo
```

**Follow-along debugging.** `ht watch` streams a session live to another
  pane, so a human can shoulder-surf whatever an agent (or a detached
  process) is driving — handy during skill development or pair debugging.

```shell
# Pane A: the watcher blocks until a matching session is created.
ht watch nethack-demo

# Pane B (or an agent): create the session the watcher is waiting for.
ht run --size 78x46 --name nethack-demo nethack -u Claude
ht send nethack-demo "y" --wait-duration 150ms --view
# (pane A now shows nethack, live)
```

## Install

```shell
brew install montanaflynn/tap/ht
```

<details>
<summary>From release</summary>

Grab a tarball from the [releases page](https://github.com/montanaflynn/headless-terminal/releases),
or extract + install in place:

**macOS (Apple Silicon)**

```shell
curl -L https://github.com/montanaflynn/headless-terminal/releases/latest/download/ht-v0.1.0-darwin-arm64.tar.gz | tar xz
sudo mv ht /usr/local/bin/
```

**Linux (x86_64)**

```shell
curl -L https://github.com/montanaflynn/headless-terminal/releases/latest/download/ht-v0.1.0-linux-amd64.tar.gz | tar xz
sudo mv ht /usr/local/bin/
```

**Linux (arm64)**

```shell
curl -L https://github.com/montanaflynn/headless-terminal/releases/latest/download/ht-v0.1.0-linux-arm64.tar.gz | tar xz
sudo mv ht /usr/local/bin/
```

Bump the version segment when newer releases drop. The binary is ~6MB,
statically links `libghostty-vt`, and depends only on libc.

</details>

<details>
<summary>From source</summary>

Requires [Zig](https://ziglang.org) 0.15.2, CMake, pkg-config, and Go 1.22+.

```shell
git clone https://github.com/montanaflynn/headless-terminal
cd headless-terminal
make build
```

`make` orchestrates two phases: CMake fetches [ghostty](https://github.com/ghostty-org/ghostty)
at a pinned commit and builds `libghostty-vt.a` with Zig; then Go builds
`./ht` with cgo, linking that static lib via pkg-config.

</details>


## Agent Skill

An `ht`-aware skill lives in [`skills/headless-terminal/`](skills/headless-terminal/). It teaches an agent
when to reach for `ht`, the vim-style key notation, the wait-strategy decision
tree (the part agents get wrong), and common recipes.

Preferred — [skills CLI](https://skills.sh) (handles per-agent paths for
Claude Code, Codex, Cursor, Gemini, etc.):

```shell
npx skills add montanaflynn/headless-terminal --skill headless-terminal
```

Fallback — drop it into Claude Code directly:

```shell
cp -r skills/headless-terminal ~/.claude/skills/headless-terminal
```

The skill uses Anthropic's standard skills format — other agent frameworks
that consume the same layout can point their loader at `skills/headless-terminal/` or copy
it into their equivalent directory. Progressive disclosure: only the short
`SKILL.md` is always in context; reference docs load on demand.

## Commands

```
ht run <cmd...>       start a session (returns a session ID)
ht list               list sessions (table in a tty, JSON when piped)
ht view <sid>         snapshot current screen (plain | ansi | html | png | json)
ht send <sid> <keys>  send keystrokes; optional --view / --wait-* / --rate
ht wait <sid> ...     block until a condition is met
ht watch <sid>        live-stream a session; blocks until it exists
ht record <sid>       record session as asciicast (pipe to agg for GIFs)
ht stop <sid>         graceful shutdown (SIGTERM, escalates)
ht kill <sid>         immediate SIGKILL
ht remove <sid>       delete an exited session's record
ht daemon [stop]      manual daemon control (normally auto-started)
```

Session IDs can be the short 8-hex ID, an unambiguous prefix, or `--name`.

### Key notation

Vim-style. Literals pass through; angle-bracket specials are recognized:

```
<CR> <Enter> <Esc> <Tab> <BS> <Space> <Del> <Ins>
<Up> <Down> <Left> <Right> <Home> <End> <PageUp> <PageDown>
<F1>…<F12>
<C-x>          ctrl+x (letters a–z only)
<M-x>          meta/alt+x  (alias <A-x>)
<S-Tab>        shift+Tab
<C-M-x>        combine modifiers in any order
<lt>           literal '<'
```

### Send synchronization

The keystroke-to-snapshot problem in a nutshell: the child process hasn't
necessarily processed your keys by the time the daemon returns. `ht send`
gives you three ways to deal with this:

- **Pacing (default: 20ms/keystroke).** The daemon writes one keystroke at a
  time with a 20ms gap and a trailing gap. Fast TUIs reliably echo their
  reaction in that window. Override with `--rate 10ms` or `--rate 0`.
- **`--wait-duration DUR`**: a plain post-send sleep. Use when a single key
  triggers slow work (character creation in nethack, emacs startup).
- **`--wait-text`/`--wait-cursor`/`--wait-idle`/`--wait-change`/`--wait-exit`**:
  block on a deterministic condition. Compose with AND — e.g.
  `--wait-text READY --wait-idle 200ms` waits for `READY` to appear AND
  output to be quiet for 200ms.

All of these are also available as standalone `ht wait` subcommand flags.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | runtime error (session missing, IO, daemon unreachable) |
| 2 | usage error (bad flags) |
| 3 | `wait` timeout |

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                         ht CLI                               │
│  (run / send / view / watch / wait / list / stop / …)        │
└──────────────────────────────┬───────────────────────────────┘
                               │  Unix socket, JSON lines
┌──────────────────────────────┴───────────────────────────────┐
│                        ht daemon                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │
│  │ Session      │  │ Session      │  │ Session      │   …    │
│  │ ┌──────────┐ │  │ ┌──────────┐ │  │ ┌──────────┐ │        │
│  │ │ PTY      │ │  │ │ PTY      │ │  │ │ PTY      │ │        │
│  │ │ ↓        │ │  │ │ ↓        │ │  │ │ ↓        │ │        │
│  │ │ vim/etc  │ │  │ │ nethack  │ │  │ │ emacs    │ │        │
│  │ └──────────┘ │  │ └──────────┘ │  │ └──────────┘ │        │
│  │ libghostty-vt│  │ libghostty-vt│  │ libghostty-vt│        │
│  │ (grid)       │  │ (grid)       │  │ (grid)       │        │
│  └──────────────┘  └──────────────┘  └──────────────┘        │
└──────────────────────────────────────────────────────────────┘
```

Each session owns a PTY master, a libghostty terminal (the authoritative
screen model), and a set of subscriber channels for `ht watch`. All three
are serialized behind a single mutex.
