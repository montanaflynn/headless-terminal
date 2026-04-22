# Troubleshooting

## Exit codes

| Code | Meaning | Common cause |
|---|---|---|
| 0 | success | — |
| 1 | runtime error | session missing, daemon unreachable, IO |
| 2 | usage error | bad flags, wrong arg count |
| 3 | wait timeout | condition never fired within `--timeout` |

Scripts should special-case **3** — a timeout isn't the same as a crash. Often the right response is to `ht view` the session, see *why* the condition didn't fire, then decide whether to retry, send a recovery key, or abort.

## "I sent keys but the screen didn't change"

1. `ht view <sid>` — is the program parked on a modal or sub-prompt you didn't account for?
2. Is key notation right? `ht send <sid> "<C-c>"` — if that works, notation parsing is fine.
3. `ht list` — state column will read `exited` if the child already died.

## "ht view shows stale content"

You viewed before the program re-rendered. Always pair `send` with a wait:

```
ht send --wait-idle 100ms --view S "<Down>"
```

Never do `ht send S X && ht view S` as two separate calls — there's no synchronization between them.

## Wait always times out (exit 3)

- **`--wait-text`** times out: pattern isn't on screen. Try without `--regex` (or with, if you need escaping). Check for trailing whitespace or non-ASCII chars — `ht view --json` shows exact text.
- **`--wait-cursor`** times out: rows and columns are **1-indexed**. `1,1` is top-left.
- **`--wait-idle`** times out: the program is redrawing constantly (clock, spinner, progress bar). Switch to `--wait-text` if you have a stable string, or `--wait-duration` as a last resort.

## Stuck / zombie sessions

```
ht list                  # see everything, including exited sessions
ht kill <sid>            # SIGKILL a live session
ht remove --force <sid>  # force-drop the record even if running
```

If the daemon itself seems wedged:

```
ht daemon stop           # next ht command auto-restarts it
```

## Diagnose VT-parsing issues without daemon overhead

```
ht debug <cmd...>
```

Runs the command in the foreground, pipes it through libghostty-vt, prints the final screen when it exits. No session lifecycle, no daemon. Useful for asking "does the VT engine even render this TUI correctly" in isolation from session-management bugs.

## The daemon won't start

`ht` auto-starts the daemon on first use. If that fails:

1. Check the Unix socket path: `ls -la $TMPDIR/ht-*.sock` (macOS) or `/tmp/ht-*.sock` (Linux).
2. Stale socket from a crashed daemon? `ht daemon stop` is a no-op if it's already dead — remove the socket file manually, then retry.
3. Permissions: the socket inherits your UID's umask. If another user owns a leftover socket, you can't reuse it.
