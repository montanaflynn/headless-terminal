# Key notation

Vim-style. Literals pass through; angle-bracket specials are parsed.

## Specials

- `<CR>` `<Enter>` — carriage return
- `<Esc>` — escape
- `<Tab>` `<S-Tab>` — tab / shift-tab
- `<BS>` — backspace
- `<Space>` — space (a literal space also works)
- `<Del>` `<Ins>`
- `<Up>` `<Down>` `<Left>` `<Right>`
- `<Home>` `<End>` `<PageUp>` `<PageDown>`
- `<F1>`..`<F12>`
- `<lt>` — literal `<` character

## Modifiers

- `<C-x>` — ctrl+x (letters a–z only)
- `<M-x>` — meta/alt+x (alias `<A-x>`; prefixes ESC 0x1b)
- `<C-M-x>` — combine modifiers in any order

Names and modifiers are case-insensitive: `<cr>` == `<CR>`, `<c-c>` == `<C-c>`.

## Common sequences

| Action | Keys |
|---|---|
| Save and quit vim | `:wq<CR>` |
| Quit vim without saving | `:q!<CR>` |
| Send SIGINT | `<C-c>` |
| Send EOF (closes stdin) | `<C-d>` |
| Vim: insert, type, escape | `ihello<Esc>` |
| Emacs: save buffer | `<C-x><C-s>` |
| Tmux prefix | `<C-b>` |

## Raw bytes

To bypass vim-style parsing entirely (useful for pasting pre-built escape sequences):

```
ht send --raw S $'\x1b[A'          # literal ESC-[-A (up arrow)
```

Use `--raw` sparingly — notation is easier to read in logs and more portable.

## Multi-argument joining

Multiple positional args concatenate:

```
ht send S "hello" "<CR>"           # same as: ht send S "hello<CR>"
```

Handy when part of the keys comes from a variable and you don't want to worry about quoting `<CR>` through a shell.
