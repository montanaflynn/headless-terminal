package main

import (
	"bytes"
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"io"
	"os"
	"strings"

	"headless-terminal/internal/protocol"
	"headless-terminal/internal/termshot"

	"github.com/mattn/go-isatty"
)

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht view [--format plain|ansi|html|png] [--output FILE] [--json] <sid>")
	}
	var (
		format  string
		output  string
		chrome  string
		jsonOut bool
	)
	fs.StringVar(&format, "format", "plain", "output format: plain | ansi | html | png")
	fs.StringVar(&output, "output", "", "write output to FILE instead of stdout (required for png to a tty)")
	fs.StringVar(&chrome, "chrome", "none", "png window chrome: none | mac (affects --format png only)")
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

	// PNG is rendered client-side from an ANSI snapshot so the daemon stays
	// free of font/image dependencies. Same pattern as piping to freeze: we
	// just bundle the rasterizer in the CLI.
	if format == "png" {
		res, err := c.View(fs.Arg(0), "ansi")
		if err != nil {
			return err
		}
		w, closer, err := openOutput(output, format)
		if err != nil {
			return err
		}
		defer closer()
		return renderPNG(w, res.Screen, int(res.Cols), chrome)
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

// renderPNG rasterizes an ANSI snapshot to a PNG via the vendored termshot
// Scaffold. Column count comes from the session size so line-wrap matches
// what the program rendered. Chrome selects whether to draw a window frame
// around the content; default "none" produces a padded bitmap with no
// decorations (the TUI stands on its own). "mac" draws the three-dot
// window controls plus a drop shadow via termshot's built-in style.
func renderPNG(w io.Writer, ansi string, cols int, chrome string) error {
	s := termshot.NewImageCreator()
	if cols > 0 {
		s.SetColumns(cols)
	}
	s.SetPadding(20)
	switch chrome {
	case "", "none":
		s.DrawDecorations(false)
		s.DrawShadow(false)
	case "mac":
		s.DrawDecorations(true)
		s.DrawShadow(true)
	default:
		return fmt.Errorf("unknown chrome %q: want none | mac", chrome)
	}
	if err := s.AddContent(strings.NewReader(ansi)); err != nil {
		return fmt.Errorf("parse ansi: %w", err)
	}
	var buf bytes.Buffer
	if err := s.WritePNG(&buf); err != nil {
		return fmt.Errorf("render png: %w", err)
	}
	_, err := io.Copy(w, &buf)
	return err
}

// openOutput resolves the output destination. Writing binary PNG bytes to a
// terminal is almost never what the user wants — refuse it unless they
// redirected stdout or passed --output.
func openOutput(path, format string) (io.Writer, func(), error) {
	if path == "" {
		if format == "png" && isatty.IsTerminal(os.Stdout.Fd()) {
			return nil, nil, errors.New("refusing to write png to a tty; pass --output FILE or redirect stdout")
		}
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
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
