package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"headless-terminal/internal/protocol"
)

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht view [--format plain|ansi|html] [--json] <sid>")
	}
	var (
		format  string
		jsonOut bool
	)
	fs.StringVar(&format, "format", "plain", "output format: plain | ansi | html")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON with metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("sid is required")
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.View(fs.Arg(0), format)
	if err != nil {
		return err
	}

	if jsonOut {
		return emitJSON(os.Stdout, res)
	}
	writeScreen(os.Stdout, res.Screen, res.Cursor, format)
	return nil
}

// writeScreen prints a snapshot for human/agent consumption. For plain and
// ansi formats it appends a trailing `cursor: R,C` line (cheap, disambiguates
// same-glyph monsters in TUIs like nethack). Skipped for html since it would
// break the document, and skipped when Cursor is nil (off-viewport).
func writeScreen(w io.Writer, screen string, cursor *protocol.Cursor, format string) {
	fmt.Fprint(w, screen)
	if len(screen) > 0 && screen[len(screen)-1] != '\n' {
		fmt.Fprintln(w)
	}
	if cursor == nil || format == "html" {
		return
	}
	if cursor.Visible {
		fmt.Fprintf(w, "cursor: %d,%d\n", cursor.Row, cursor.Col)
	} else {
		fmt.Fprintf(w, "cursor: %d,%d (hidden)\n", cursor.Row, cursor.Col)
	}
}
