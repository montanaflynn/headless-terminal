# Recipes

## Edit a file with vim

```
ht run --name v vim /tmp/notes.md
ht send --wait-idle 100ms v "ihello world<Esc>:wq<CR>"
ht wait --exit v
ht remove v
cat /tmp/notes.md
```

## Run a REPL, capture each response

```
ht run --name py python3 -i
ht send --wait-text ">>> " --view py "print(2+2)<CR>"
ht send --wait-text ">>> " --view py "import sys; sys.version<CR>"
ht send --wait-exit py "exit()<CR>"
ht remove py
```

The `--view` flag appends a snapshot to the send response. Add `--json` to get structured output (cursor, size, text).

## Drive an interactive installer

```
ht run --name i ./install.sh
ht wait --text "(y/n)" i                     # wait for first prompt
ht send --wait-text "path:" i "y<CR>"
ht send --wait-exit i "/opt/myapp<CR>"
ht remove i
```

`ht wait` (no `send`) is the cleanest way to block on a prompt for an already-running session.

## Drive a turn-based TUI (nethack, roguelikes, any curses game)

```
ht run --size 80x24 --name g nethack -u Agent
ht send --wait-text "\[yn\]" --regex --view g "y"          # dismiss startup prompt
ht send --wait-idle 200ms --view g "Fl"                    # fight left (east)
ht send --wait-text "--More--" --view g "<Space>"          # dismiss message
```

Three things that make this work:

- **One call per turn.** `--view` appends the snapshot to the send result,
  so there is no separate `ht view` step.
- **Deterministic sync.** `--wait-text "\[yn\]" --regex` blocks until the
  next prompt actually renders. `--wait-idle 200ms` alone is racy on fast
  transitions — a short idle window can return between two prompts and
  your next keystroke lands on the wrong one.
- **Cursor in the snapshot.** The trailing `cursor: R,C` line in the plain
  output tells you where the game thinks "you" are. Critical when the map
  has multiple `@` glyphs (player + human-shaped monsters like Medusa).

For death/game-over screens, chain wait-text patterns through each prompt
rather than firing blind keystrokes at idle:

```
ht send --wait-text "Die?"     --view g "y"
ht send --wait-text "identify" --view g "n"
ht send --wait-text "overview" --view g "n"
ht send --wait-text "bones"    --view g "n"
```

Plain `--wait-text` does substring match — no regex escaping needed. Add
`--regex` only when you need alternatives or character classes.

## Dismiss a modal / unexpected prompt

```
ht view S                      # see what's actually on screen
ht send S "<Esc>"              # or <CR>, <C-c>, q — program-dependent
```

When scripted automation hits a dialog you didn't anticipate, view first, guess the dismissal key, verify. Don't blindly `<CR>` — some TUIs treat that as "OK, destroy my data."

## Watch live from another pane while an agent drives

```
# Pane A — blocks until the named session is created, then live-streams it
ht watch demo

# Pane B — agent
ht run --size 100x40 --name demo htop
ht send --wait-duration 1s demo "q"
```

`ht watch` is useful during skill development — you can see exactly what the agent sees.

## Run under a specific terminal size

```
ht run --size 132x50 --name big vim big.txt
```

Default is 80x24. Use a larger size when the TUI's layout depends on having room (e.g. emacs splits, k9s dashboards).

## Pass environment or a working directory

```
ht run --cwd /tmp --env FOO=bar --env DEBUG=1 --name s my-tui
```

`--env` can repeat.

## Record a session to asciicast / GIF

```
ht record --output session.cast S     # start recording; Ctrl-C to stop
agg session.cast session.gif          # render an animated GIF
```

Output is [asciicast v2](https://docs.asciinema.org/manual/asciicast/v2/)
— standard newline-delimited JSON with a header line and `[t, "o", bytes]`
event lines. Works with `asciinema play`, `agg` (Rust, by the asciinema
authors), `svg-term-cli`, and the asciinema web player. Nothing
ht-specific; any tool that reads asciicasts will read ours.

Record a specific window of action by chaining with `send`:

```
ht record --output action.cast S &    # background recorder
REC=$!
ht send --wait-text "Done" S "<commands...>"
kill -INT $REC                        # stop when your action finishes
agg action.cast action.gif
```

## Screenshot a session to PNG

```
ht view --format png --output /tmp/session.png S
```

Rasterizes the current screen with window chrome, macOS-style dots, and a
drop shadow. Handy for:
- Letting yourself *look* at a session (Claude can read PNGs through the
  normal file-read tool).
- Attaching to bug reports or demo write-ups.
- Verifying UI changes — snapshot before and after.

Refuses to write PNG bytes to an interactive terminal (pass `--output FILE`
or redirect stdout). Colors, bold, italics, and inverse all render.

## Extract text from the screen

```
ht view --format plain S | grep -A2 "Summary:"
```

For structured extraction (need cursor position or character attributes):

```
ht view --json S | jq '.text'
```

## Pipe-aware list

```
ht list             # pretty table in a tty
ht list --json      # JSON when piped or scripted
```

`ht list` auto-detects whether stdout is a terminal and switches format — you don't usually need `--json` explicitly unless building a pipeline in a subshell.
