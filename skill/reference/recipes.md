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
