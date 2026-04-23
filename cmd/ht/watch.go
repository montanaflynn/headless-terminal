package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"headless-terminal/internal/client"
	"headless-terminal/internal/protocol"
)

// Alternate-screen enter/exit. Entering gives the TUI a fresh rectangle;
// exiting restores the user's shell state. Matches what tmux/less do.
const (
	altScreenEnter = "\x1b[?1049h"
	altScreenExit  = "\x1b[?1049l"
	// Modes like mouse tracking and bracketed paste are terminal-global,
	// not per-screen-buffer — alt-screen exit won't turn them off. The
	// watched session may have enabled any of these; reset them so the
	// user's shell isn't left with stray mouse reports or hidden cursor.
	termModeReset = "\x1b[?1000l" + // X10 mouse
		"\x1b[?1002l" + // button-event mouse
		"\x1b[?1003l" + // any-event mouse
		"\x1b[?1006l" + // SGR mouse
		"\x1b[?1015l" + // urxvt mouse
		"\x1b[?2004l" + // bracketed paste
		"\x1b[?25h" + // show cursor
		"\x1b[0m" // reset SGR attrs
)

func cmdWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht watch <sid>")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("sid is required")
	}
	sid := fs.Arg(0)

	c, err := newClient()
	if err != nil {
		return err
	}

	useAlt := term.IsTerminal(int(os.Stdout.Fd()))

	// Show a one-line status on stderr while we wait for the daemon
	// to resolve. We do NOT enter the alt screen yet — the indicator
	// needs to stay in the normal shell scrollback.
	waiting := true
	fmt.Fprintf(os.Stderr, "waiting for session %q...\n", sid)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	done := make(chan error, 1)
	go func() {
		done <- c.Watch(client.WatchConfig{
			ID: sid,
			OnReady: func(_ *protocol.SessionInfo) {
				if waiting {
					fmt.Fprintln(os.Stderr, "attached.")
					waiting = false
				}
				if useAlt {
					_, _ = os.Stdout.WriteString(altScreenEnter)
				}
			},
			OnFrame: func(f protocol.WatchFrame) error {
				if len(f.Data) > 0 {
					if _, err := os.Stdout.Write(f.Data); err != nil {
						return err
					}
				}
				if f.EOF {
					fmt.Fprintln(os.Stderr, "\n[session ended]")
				}
				return nil
			},
		})
	}()

	defer func() {
		if useAlt && !waiting {
			// Only exit alt-screen if we entered it (i.e., OnReady fired).
			_, _ = os.Stdout.WriteString(termModeReset + altScreenExit)
		}
	}()

	select {
	case err := <-done:
		return err
	case <-sigCh:
		if waiting {
			fmt.Fprintln(os.Stderr, "aborted.")
		} else {
			fmt.Fprintln(os.Stderr, "\n[watch detached]")
		}
		return nil
	}
}
