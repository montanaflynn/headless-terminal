package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"strconv"
	"strings"
	"time"

	"headless-terminal/internal/protocol"
)

func cmdWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht wait [--idle DUR] [--text PAT [--regex]] [--cursor R,C] [--exit] [--timeout 5s] [--json] <sid>")
	}
	var (
		idle    time.Duration
		change  bool
		text    string
		regex   bool
		cursor  string
		exit    bool
		timeout time.Duration
		jsonOut bool
	)
	fs.DurationVar(&idle, "idle", 0, "PTY output has been quiet for this duration")
	fs.BoolVar(&change, "change", false, "any output chunk has arrived since this wait started")
	fs.StringVar(&text, "text", "", "substring or regex present in view")
	fs.BoolVar(&regex, "regex", false, "interpret --text as RE2 regex")
	fs.StringVar(&cursor, "cursor", "", "cursor at exact 1-indexed ROW,COL")
	fs.BoolVar(&exit, "exit", false, "session has exited")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "abort if not met within this duration (0 = none)")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON result")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("sid is required")
	}

	p := protocol.WaitParams{
		ID:        fs.Arg(0),
		IdleMs:    int(idle / time.Millisecond),
		Change:    change,
		Text:      text,
		Regex:     regex,
		Exit:      exit,
		TimeoutMs: int(timeout / time.Millisecond),
	}
	if cursor != "" {
		r, col, err := parseCursor(cursor)
		if err != nil {
			return err
		}
		p.Cursor = &protocol.Cursor{Row: r, Col: col}
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.Wait(p)
	if err != nil {
		return err
	}

	if jsonOut {
		_ = emitJSON(os.Stdout, res)
	}
	if !res.Matched {
		// Exit code 3 = wait timeout, per spec.
		os.Exit(3)
	}
	return nil
}

// parseCursor parses "ROW,COL" into 1-indexed uint16s.
func parseCursor(s string) (uint16, uint16, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid cursor %q, want ROW,COL", s)
	}
	r, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid row in %q: %w", s, err)
	}
	c, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid col in %q: %w", s, err)
	}
	return uint16(r), uint16(c), nil
}
