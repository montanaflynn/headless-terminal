package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"headless-terminal/internal/client"
	"headless-terminal/internal/protocol"
)

// asciicastV2Header is the first line of an asciicast v2 recording.
// Spec: https://docs.asciinema.org/manual/asciicast/v2/
type asciicastV2Header struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Env       map[string]string `json:"env,omitempty"`
	Title     string            `json:"title,omitempty"`
}

func cmdRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht record [--output FILE] [--title TITLE] <sid>")
	}
	var (
		output string
		title  string
	)
	fs.StringVar(&output, "output", "", "write asciicast to FILE instead of stdout")
	fs.StringVar(&title, "title", "", "optional title embedded in the asciicast header")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("sid is required")
	}
	sid := fs.Arg(0)

	var out io.Writer = os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	// Serialize writes: header comes from OnReady, events from OnFrame on the
	// same goroutine, but json encoding + timing share state we want tidy.
	var (
		mu      sync.Mutex
		enc     = json.NewEncoder(out)
		t0      time.Time
		started bool
	)

	// Recording survives signals by stopping the watch cleanly; signal
	// handling lives alongside client.Watch the same way `ht watch` does.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	done := make(chan error, 1)
	go func() {
		done <- c.Watch(client.WatchConfig{
			ID: sid,
			OnReady: func(info *protocol.SessionInfo) {
				mu.Lock()
				defer mu.Unlock()
				hdr := asciicastV2Header{
					Version:   2,
					Width:     int(info.Cols),
					Height:    int(info.Rows),
					Timestamp: time.Now().Unix(),
					Env:       map[string]string{"TERM": "xterm-256color"},
					Title:     title,
				}
				// Best-effort: if the header write fails we let the watch
				// continue and report the error when it ends.
				_ = enc.Encode(hdr)
				fmt.Fprintf(os.Stderr, "recording %q (%dx%d)...\n", sid, info.Cols, info.Rows)
			},
			OnFrame: func(f protocol.WatchFrame) error {
				if len(f.Data) == 0 {
					return nil
				}
				mu.Lock()
				defer mu.Unlock()
				if !started {
					// Anchor t=0 at the first real data frame, not at
					// OnReady, so the priming paint isn't artificially
					// offset from a header-write wall-clock reading.
					t0 = time.Now()
					started = true
				}
				elapsed := time.Since(t0).Seconds()
				// Event form: [float_seconds, "o", utf8_string].
				evt := [3]any{elapsed, "o", string(f.Data)}
				return enc.Encode(evt)
			},
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "[recording ended]")
		return nil
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "\n[recording stopped]")
		return nil
	}
}
