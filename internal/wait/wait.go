// Package wait implements the condition-based blocking primitives
// that back both `ht wait` and `ht send --wait-*`. Conditions compose
// as AND: all must be simultaneously true for Wait to report success.
package wait

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"headless-terminal/internal/session"
)

// Condition is a set of wait conditions. Any zero-valued field is not
// checked. Multiple conditions compose as logical AND.
type Condition struct {
	// Idle: no PTY output for this duration. Timer starts when Wait
	// begins and resets on every new output chunk.
	Idle time.Duration

	// Change: true once any output chunk arrives after Wait starts.
	// Monotonic — once satisfied it stays so for the life of this Wait.
	// Useful as "the send actually produced a reaction" before taking
	// a snapshot, and AND-composes with Idle for a burst detector
	// (wait for activity to start, then for it to settle).
	Change bool

	// Text: substring (default) or RE2 regex (when Regex is true)
	// matched against the session's current plain-text view. Re-checked
	// after every output chunk and once at start.
	Text  string
	Regex bool

	// Cursor: exact 1-indexed cursor position required.
	Cursor *CursorCheck

	// Exit: session has entered StateExited.
	Exit bool

	// Timeout: overall deadline. Zero means no timeout (wait forever).
	// On timeout, Wait returns Result{Matched:false, Trigger:"timeout"}
	// with a nil error.
	Timeout time.Duration
}

// CursorCheck requires the cursor to be at an exact 1-indexed position.
type CursorCheck struct {
	Row uint16
	Col uint16
}

// Result describes how Wait completed.
type Result struct {
	Matched bool          // true if all conditions were met
	Trigger string        // "idle" | "text" | "cursor" | "exit" | "timeout"
	Elapsed time.Duration // from Wait call start
}

// Wait blocks until the session meets the condition set, the Timeout
// fires, ctx is canceled, or an unrecoverable error occurs.
//
// Returning (_, nil) with Matched=false means the timeout fired. A
// non-nil error indicates an unrecoverable problem (e.g., empty condition
// set, bad regex).
func Wait(ctx context.Context, s *session.Session, cond Condition) (Result, error) {
	start := time.Now()

	if cond.Idle == 0 && cond.Text == "" && cond.Cursor == nil && !cond.Exit && !cond.Change {
		return Result{}, errors.New("wait: no condition specified")
	}

	var textRE *regexp.Regexp
	if cond.Text != "" && cond.Regex {
		re, err := regexp.Compile(cond.Text)
		if err != nil {
			return Result{}, fmt.Errorf("wait: invalid regex: %w", err)
		}
		textRE = re
	}

	// Per-condition predicates. A predicate returning true means that
	// condition is currently satisfied. Unlisted conditions are treated
	// as always-true (not required).
	checkText := func(screen string) bool {
		if cond.Text == "" {
			return true
		}
		if textRE != nil {
			return textRE.MatchString(screen)
		}
		return strings.Contains(screen, cond.Text)
	}
	checkCursor := func(snap *session.Snapshot) bool {
		if cond.Cursor == nil {
			return true
		}
		return snap.CursorRow == cond.Cursor.Row && snap.CursorCol == cond.Cursor.Col
	}
	checkExit := func() bool {
		return !cond.Exit || s.State() == session.StateExited
	}

	// evalSnapshot checks text + cursor by calling View once. We avoid
	// calling it for idle/exit-only waits.
	needsSnapshot := cond.Text != "" || cond.Cursor != nil
	evalSnapshot := func() (textOK, cursorOK bool, err error) {
		if !needsSnapshot {
			return true, true, nil
		}
		snap, err := s.View(session.FormatPlain)
		if err != nil {
			return false, false, err
		}
		return checkText(snap.Screen), checkCursor(snap), nil
	}

	// Subscribe first so we don't miss any output between the initial
	// snapshot check and entering the select loop.
	ch, unsub := s.Subscribe()
	defer unsub()

	// Initial check: maybe the session already satisfies everything.
	textOK, cursorOK, err := evalSnapshot()
	if err != nil {
		return Result{}, err
	}
	// Idle and Change are never satisfied by the initial check: Idle's
	// timer starts at Wait start (so it needs a full quiet duration),
	// and Change requires an actual chunk to arrive after we subscribe.
	if cond.Idle == 0 && !cond.Change && textOK && cursorOK && checkExit() {
		return Result{
			Matched: true,
			Trigger: pickTrigger(cond, textOK, cursorOK, true, false, false),
			Elapsed: time.Since(start),
		}, nil
	}

	// Idle timer: fires when no output has arrived for cond.Idle.
	var idleTimer *time.Timer
	var idleC <-chan time.Time
	idleFired := false
	if cond.Idle > 0 {
		idleTimer = time.NewTimer(cond.Idle)
		defer idleTimer.Stop()
		idleC = idleTimer.C
	}

	// Overall timeout.
	var timeoutC <-chan time.Time
	if cond.Timeout > 0 {
		t := time.NewTimer(cond.Timeout)
		defer t.Stop()
		timeoutC = t.C
	}

	done := s.Done()

	changeSeen := false

	// allMet returns true if every requested condition is currently
	// satisfied. Idle is tracked separately via idleFired; Change via
	// changeSeen.
	allMet := func() bool {
		if !textOK || !cursorOK {
			return false
		}
		if !checkExit() {
			return false
		}
		if cond.Idle > 0 && !idleFired {
			return false
		}
		if cond.Change && !changeSeen {
			return false
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return Result{Elapsed: time.Since(start)}, ctx.Err()

		case <-timeoutC:
			return Result{Matched: false, Trigger: "timeout", Elapsed: time.Since(start)}, nil

		case <-idleC:
			idleFired = true
			if allMet() {
				return Result{
					Matched: true,
					Trigger: pickTrigger(cond, textOK, cursorOK, checkExit(), idleFired, changeSeen),
					Elapsed: time.Since(start),
				}, nil
			}

		case <-done:
			// Session exited. Re-evaluate text/cursor once more (final
			// state may satisfy them even if Exit wasn't requested).
			textOK, cursorOK, err = evalSnapshot()
			if err != nil {
				return Result{Elapsed: time.Since(start)}, err
			}
			if allMet() {
				return Result{
					Matched: true,
					Trigger: pickTrigger(cond, textOK, cursorOK, true, idleFired, changeSeen),
					Elapsed: time.Since(start),
				}, nil
			}
			// Session is gone and conditions still not met. Continue so
			// idle/timeout can fire naturally; drain remaining chunks
			// from ch as the fanout closes it.
			done = nil

		case _, ok := <-ch:
			if !ok {
				// Subscriber channel closed (session ended). Loop back
				// to let the done case (already handled) or timeout run.
				ch = nil
				continue
			}
			// New output: mark change seen, reset idle timer, re-check.
			changeSeen = true
			if idleTimer != nil {
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(cond.Idle)
				idleFired = false
			}
			textOK, cursorOK, err = evalSnapshot()
			if err != nil {
				return Result{Elapsed: time.Since(start)}, err
			}
			if allMet() {
				return Result{
					Matched: true,
					Trigger: pickTrigger(cond, textOK, cursorOK, checkExit(), idleFired, changeSeen),
					Elapsed: time.Since(start),
				}, nil
			}
		}
	}
}

// pickTrigger chooses a human-readable label for which condition fired.
// Priority: explicit exit > text > cursor > idle > change.
func pickTrigger(cond Condition, textOK, cursorOK, exitOK, idleFired, changeSeen bool) string {
	if cond.Exit && exitOK {
		return "exit"
	}
	if cond.Text != "" && textOK {
		return "text"
	}
	if cond.Cursor != nil && cursorOK {
		return "cursor"
	}
	if cond.Idle > 0 && idleFired {
		return "idle"
	}
	if cond.Change && changeSeen {
		return "change"
	}
	return ""
}
