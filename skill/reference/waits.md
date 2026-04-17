# Wait strategies

The single hardest thing about driving a TUI is knowing when the program has finished reacting to your keystrokes. `ht` gives you six mechanisms; pick the most deterministic one that fits the situation.

## Decision tree

```
Does a specific string appear on screen when the program is ready?
  └─ YES →  --wait-text "PATTERN"      (add --regex for RE2)

Does the cursor land at a known position when ready?
  └─ YES →  --wait-cursor ROW,COL      (1-indexed)

Will the session exit when the work is done?
  └─ YES →  --wait-exit

Output reliably goes quiet for a predictable interval after ready?
  └─ YES →  --wait-idle 200ms          (most common fallback)

You just need *any* output to confirm the key was processed?
  └─ YES →  --wait-change

None of the above work?
  └─ --wait-duration 500ms             (last resort: unconditional sleep)
```

## Composing

Wait flags AND together. A useful combo:

```
ht send --wait-text "READY" --wait-idle 150ms S "<CR>"
```

Waits until `READY` appears AND output has been quiet for 150ms — handles the case where "READY" flashes onto the screen mid-redraw before the final state stabilizes.

## Timeout

All waits default to `--timeout 5s`. Exit code **3** means timeout. Bump for slow startup:

```
ht send --wait-text "Prompt>" --timeout 30s S "<CR>"
```

Set `--timeout 0` to disable it entirely (dangerous — wedges indefinitely if the condition never fires).

## --wait-duration vs --wait-idle

These are easy to confuse:

- `--wait-duration 300ms` — unconditionally sleep 300ms. Use when output isn't a reliable readiness signal (e.g. a persistent animation or spinner).
- `--wait-idle 300ms` — wait *until* output has been quiet for 300ms. Adaptive: slow machines naturally wait longer, fast machines return sooner.

**Prefer `--wait-idle`.** Use `--wait-duration` only when idle-detection false-fires (e.g. programs with periodic redraws or slow key echo).

## Standalone wait

The same conditions are available on `ht wait` for use outside a `send`:

```
ht wait --text "Install complete" --timeout 5m S
```

Use when you've already sent keys and want to run unrelated work before blocking on a condition. The `ht wait` flags drop the `--wait-` prefix: `--idle`, `--text`, `--cursor`, `--exit`, `--regex`, `--timeout`.

## Default send pacing

`ht send` writes one keystroke at a time with a 20ms gap by default. For most TUIs this alone is enough for echo to land correctly. Override:

```
ht send --rate 0 S "lots of text"     # send as one chunk, no pacing
ht send --rate 10ms S "..."           # tighter pacing
```

Don't tune `--rate` unless you have a specific reason — the default is picked to work across the common case.
