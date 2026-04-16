package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"headless-terminal/internal/client"
)

// newClient returns a client with the daemon auto-started if needed.
func newClient() (*client.Client, error) {
	c := client.New(os.Getenv("HT_SOCKET"))
	if err := c.EnsureDaemon(); err != nil {
		return nil, err
	}
	return c, nil
}

// stdoutIsTTY reports whether output is going to a terminal. Used to
// auto-enable --json when piped.
func stdoutIsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// emitJSON writes v as a JSON line to w.
func emitJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
