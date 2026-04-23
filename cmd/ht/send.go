package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"time"

	"headless-terminal/internal/keys"
	"headless-terminal/internal/protocol"
)

// defaultKeyRate paces multi-byte sends, simulating a human typist.
// Gives the child process a chance to process each keystroke before
// the next arrives. At 20ms/key, "Hello World" takes ~200ms — still
// snappy for agents but slow enough that TUIs don't batch-process.
const defaultKeyRate = 20 * time.Millisecond

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ht send [--raw] [--wait-idle DUR] [--wait-text PAT [--regex]] [--wait-cursor R,C] [--wait-exit] [--timeout 5s] [--view] [--format plain|ansi|html] [--json] <sid> <keys...>")
	}
	var (
		raw          bool
		rate         time.Duration
		waitDuration time.Duration
		waitIdle     time.Duration
		waitChange   bool
		waitText     string
		waitRegex    bool
		waitCursor   string
		waitExit     bool
		timeout      time.Duration
		viewFlag     bool
		format       string
		jsonOut      bool
	)
	fs.BoolVar(&raw, "raw", false, "send literal bytes without vim-style parsing")
	fs.DurationVar(&rate, "rate", defaultKeyRate, "delay between keystrokes (0 = send as one chunk)")
	fs.DurationVar(&waitDuration, "wait-duration", 0, "sleep this long after send before view/wait (for keys that trigger slow work)")
	fs.DurationVar(&waitIdle, "wait-idle", 0, "after send, wait until no output for this duration")
	fs.BoolVar(&waitChange, "wait-change", false, "after send, wait for any output chunk to arrive")
	fs.StringVar(&waitText, "wait-text", "", "after send, wait until this text appears")
	fs.BoolVar(&waitRegex, "regex", false, "interpret --wait-text as RE2 regex")
	fs.StringVar(&waitCursor, "wait-cursor", "", "after send, wait for cursor at ROW,COL")
	fs.BoolVar(&waitExit, "wait-exit", false, "after send, wait for session to exit")
	fs.DurationVar(&timeout, "timeout", 5*time.Second, "wait timeout (0 = none)")
	fs.BoolVar(&viewFlag, "view", false, "include a snapshot in the response")
	fs.StringVar(&format, "format", "plain", "snapshot format: plain | ansi | html")
	fs.BoolVar(&jsonOut, "json", false, "emit full SendResult as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		fs.Usage()
		return errors.New("sid and at least one keys argument are required")
	}

	sid := fs.Arg(0)
	inputs := fs.Args()[1:]

	var data []byte
	if raw {
		for _, s := range inputs {
			data = append(data, []byte(s)...)
		}
	} else {
		b, err := keys.ParseAll(inputs)
		if err != nil {
			return err
		}
		data = b
	}

	// Only build a wait-params if the user explicitly requested a wait.
	// --view alone is satisfied by pacing: by the time SendPaced returns,
	// the child has had a ~20ms gap per keystroke to emit its response,
	// so the snapshot taken immediately after is accurate. For slow
	// children, users add explicit --wait-text / --wait-idle / etc.
	hasWait := waitIdle > 0 || waitChange || waitText != "" || waitCursor != "" || waitExit
	var waitParams *protocol.WaitParams
	if hasWait {
		wp := &protocol.WaitParams{
			IdleMs:    int(waitIdle / time.Millisecond),
			Change:    waitChange,
			Text:      waitText,
			Regex:     waitRegex,
			Exit:      waitExit,
			TimeoutMs: int(timeout / time.Millisecond),
		}
		if waitCursor != "" {
			r, c, err := parseCursor(waitCursor)
			if err != nil {
				return err
			}
			wp.Cursor = &protocol.Cursor{Row: r, Col: c}
		}
		waitParams = wp
	}

	var viewOpts *protocol.SendViewOpts
	if viewFlag {
		viewOpts = &protocol.SendViewOpts{Format: format}
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	res, err := c.SendExt(protocol.SendParams{
		ID:              sid,
		Data:            data,
		InterKeyDelayMs: int(rate / time.Millisecond),
		SettleMs:        int(waitDuration / time.Millisecond),
		Wait:            waitParams,
		View:            viewOpts,
	})
	if err != nil {
		return err
	}

	// Output. --json emits the full structured response. Otherwise, for
	// the common case, just print the screen (if --view was set).
	if jsonOut {
		_ = emitJSON(os.Stdout, res)
	} else if res.View != nil {
		writeScreen(os.Stdout, res.View.Screen, res.View.Cursor, format)
	}

	// Exit code 3 on wait timeout, per spec. View is still printed above
	// on best-effort basis so the agent sees the stuck state.
	if res.Wait != nil && !res.Wait.Matched {
		os.Exit(3)
	}
	return nil
}
