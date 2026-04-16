package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"github.com/mitchellh/go-libghostty"
	"golang.org/x/term"
)

// cmdDebug implements `ht debug <cmd...>`: a foreground, daemon-less
// command that spawns a child in a PTY, tees the output through libghostty
// in parallel to the real terminal, and prints libghostty's final view
// on exit. Useful for validating the VT stack against edge-case output;
// not the primary product surface (see `ht run`).
func cmdDebug(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ht debug <cmd> [args...]")
	}
	name, rest := args[0], args[1:]

	cmd := exec.Command(name, rest...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	cols, rows := ttySize(os.Stdin)
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})

	vt, err := libghostty.NewTerminal(libghostty.WithSize(cols, rows))
	if err != nil {
		return fmt.Errorf("libghostty new: %w", err)
	}
	defer vt.Close()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			c, r := ttySize(os.Stdin)
			_ = pty.Setsize(ptmx, &pty.Winsize{Cols: c, Rows: r})
			_ = vt.Resize(c, r, 0, 0)
		}
	}()

	var restore func() error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			restore = func() error { return term.Restore(int(os.Stdin.Fd()), oldState) }
		}
	}
	defer func() {
		if restore != nil {
			_ = restore()
		}
	}()

	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(io.MultiWriter(os.Stdout, vt), ptmx)
	waitErr := cmd.Wait()

	if restore != nil {
		_ = restore()
		restore = nil
	}
	printSnapshot(vt)

	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		os.Exit(exitErr.ExitCode())
	}
	return waitErr
}

func ttySize(f *os.File) (cols, rows uint16) {
	ws, err := pty.GetsizeFull(f)
	if err != nil || ws.Cols == 0 || ws.Rows == 0 {
		return 80, 24
	}
	return ws.Cols, ws.Rows
}

func printSnapshot(vt *libghostty.Terminal) {
	f, err := libghostty.NewFormatter(vt,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain),
		libghostty.WithFormatterTrim(true),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ht: formatter:", err)
		return
	}
	defer f.Close()

	out, err := f.FormatString()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ht: format:", err)
		return
	}
	fmt.Fprintln(os.Stderr, "\n--- final screen (libghostty view) ---")
	fmt.Fprintln(os.Stderr, out)
}
