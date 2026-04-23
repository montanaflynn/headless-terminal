package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"strconv"
	"strings"

	"headless-terminal/internal/protocol"
)

// envList collects repeated --env K=V flags.
type envList []string

func (e *envList) String() string     { return strings.Join(*e, ",") }
func (e *envList) Set(s string) error { *e = append(*e, s); return nil }
func (e *envList) Type() string       { return "K=V" }

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	// `ht run` runs an arbitrary child command; flags after the cmd belong
	// to the child (e.g. `ht run nethack -u Claude`). Disable interspersed
	// parsing so pflag stops at the first positional and hands everything
	// after it to the child.
	fs.SetInterspersed(false)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht run [--size 80x24] [--cwd DIR] [--env K=V] [--name N] [--json] <cmd> [args...]")
	}
	var (
		size    string
		cwd     string
		env     envList
		name    string
		jsonOut bool
	)
	fs.StringVar(&size, "size", "", "terminal size as COLSxROWS (default 80x24)")
	fs.StringVar(&cwd, "cwd", "", "working directory")
	fs.Var(&env, "env", "environment variable KEY=VALUE (repeatable)")
	fs.StringVar(&name, "name", "", "friendly alias for the session")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("cmd is required")
	}

	cols, rows, err := parseSize(size)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.Run(protocol.RunParams{
		Cmd:  fs.Args(),
		Cols: cols,
		Rows: rows,
		Cwd:  cwd,
		Env:  []string(env),
		Name: name,
	})
	if err != nil {
		return err
	}

	if jsonOut || !stdoutIsTTY() {
		return emitJSON(os.Stdout, res.Session)
	}
	fmt.Println(res.Session.ID)
	return nil
}

// parseSize parses "COLSxROWS" into uint16 pair. Empty input returns 0,0
// so the daemon uses defaults.
func parseSize(s string) (uint16, uint16, error) {
	if s == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size %q, want COLSxROWS", s)
	}
	cols, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cols in %q: %w", s, err)
	}
	rows, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid rows in %q: %w", s, err)
	}
	return uint16(cols), uint16(rows), nil
}
