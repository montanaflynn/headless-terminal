// Package session wraps a child process running in a pseudoterminal
// alongside a libghostty virtual terminal that tracks its screen state.
//
// A Session owns:
//   - the PTY master file descriptor (input to the child)
//   - a libghostty.Terminal parsing child output into a grid
//   - a set of subscribers that receive live output bytes (for `ht watch`)
//
// All libghostty access is serialized by a single mutex; the PTY reader
// is the only writer to the terminal, snapshots and resizes contend
// with it briefly under that same lock.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/mitchellh/go-libghostty"
)

type State int

const (
	StateStarting State = iota
	StateRunning
	StateExited
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateExited:
		return "exited"
	default:
		return "unknown"
	}
}

// Options configures a new session.
type Options struct {
	Cmd  []string // argv of the child, Cmd[0] is the program
	Cols uint16   // terminal width in cells; defaults to 80
	Rows uint16   // terminal height in cells; defaults to 24
	Cwd  string   // working directory; defaults to inherited
	Env  []string // environment; nil means inherit
	Name string   // optional friendly alias
}

// Format selects the shape of a snapshot.
type Format int

const (
	FormatPlain Format = iota
	FormatVT
	FormatHTML
)

// Snapshot is a point-in-time capture of a session.
type Snapshot struct {
	Screen string // text per Format
	Cols   uint16
	Rows   uint16
	// Cursor position (1-indexed to match VT conventions); Visible reflects
	// whether the cursor is currently hidden by the TUI.
	CursorRow uint16
	CursorCol uint16
	CursorVis bool
}

// Session is a running or exited child with an attached libghostty
// terminal. Safe for concurrent use.
type Session struct {
	ID        string
	Name      string
	Cmd       []string
	StartedAt time.Time

	// Wire guards all mutable state including vt, subs, state, exitCode,
	// cols/rows. Held briefly for reads; briefly for writes.
	mu       sync.Mutex
	vt       *libghostty.Terminal
	rs       *libghostty.RenderState
	state    State
	exitCode int
	cols     uint16
	rows     uint16
	subs     map[int]chan []byte
	nextSub  int

	ptmx *os.File
	cmd  *exec.Cmd

	done chan struct{} // closed when child has exited and reader drained
}

// Start launches opts.Cmd in a new PTY and begins parsing its output.
// The returned Session is live until Close.
func Start(opts Options) (*Session, error) {
	if len(opts.Cmd) == 0 {
		return nil, errors.New("session: empty Cmd")
	}
	cols, rows := opts.Cols, opts.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	vt, err := libghostty.NewTerminal(libghostty.WithSize(cols, rows))
	if err != nil {
		return nil, fmt.Errorf("libghostty: %w", err)
	}
	rs, err := libghostty.NewRenderState()
	if err != nil {
		vt.Close()
		return nil, fmt.Errorf("libghostty render state: %w", err)
	}

	c := exec.Command(opts.Cmd[0], opts.Cmd[1:]...)
	if opts.Cwd != "" {
		c.Dir = opts.Cwd
	}
	if opts.Env != nil {
		c.Env = opts.Env
	}

	ptmx, err := pty.StartWithSize(c, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		vt.Close()
		return nil, fmt.Errorf("pty start: %w", err)
	}

	s := &Session{
		ID:        newID(),
		Name:      opts.Name,
		Cmd:       append([]string(nil), opts.Cmd...),
		StartedAt: time.Now().UTC(),
		vt:        vt,
		rs:        rs,
		state:     StateRunning,
		cols:      cols,
		rows:      rows,
		subs:      make(map[int]chan []byte),
		ptmx:      ptmx,
		cmd:       c,
		done:      make(chan struct{}),
	}

	go s.readLoop()
	go s.waitLoop()

	return s, nil
}

// readLoop pumps PTY output into libghostty and to all subscribers.
// Exits when the PTY master hits EOF or errors.
func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			_, _ = s.vt.Write(chunk)
			// Fan out to subscribers. Blocking send: if a subscriber
			// can't keep up, it backpressures the PTY read loop. That
			// will in turn backpressure the child, which is acceptable.
			for _, ch := range s.subs {
				ch <- chunk
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}

// waitLoop waits for the child to exit and marks the session as exited.
// Also closes all subscriber channels so watchers see the session end.
func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	s.state = StateExited
	s.exitCode = code
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	// Close the PTY master so readLoop exits if it hasn't already.
	_ = s.ptmx.Close()
	close(s.done)
}

// Send writes bytes to the child's stdin via the PTY master.
// Returns an error if the session has exited.
func (s *Session) Send(b []byte) error {
	s.mu.Lock()
	if s.state == StateExited {
		s.mu.Unlock()
		return errors.New("session has exited")
	}
	s.mu.Unlock()
	_, err := s.ptmx.Write(b)
	return err
}

// SendPaced writes bytes with `delay` between logical keystrokes,
// simulating a human typing. This gives the child process time to
// process each keystroke before the next arrives, which avoids races
// where a snapshot reflects only a partial response to a multi-key
// send. When delay <= 0 or len(b) <= 1, behaves like Send.
//
// Keystrokes are atomic groups: VT escape sequences (CSI/SS3/OSC) are
// never split, and multi-byte UTF-8 runes are never split. Plain ASCII
// chars are one keystroke each.
func (s *Session) SendPaced(b []byte, delay time.Duration) error {
	if delay <= 0 || len(b) <= 1 {
		return s.Send(b)
	}
	s.mu.Lock()
	if s.state == StateExited {
		s.mu.Unlock()
		return errors.New("session has exited")
	}
	s.mu.Unlock()

	first := true
	for i := 0; i < len(b); {
		if !first {
			time.Sleep(delay)
		}
		first = false

		start := i
		if b[i] == 0x1b {
			i = escapeSeqEnd(b, i)
		} else {
			_, size := utf8.DecodeRune(b[i:])
			if size < 1 {
				size = 1 // defensive: advance at least one byte
			}
			i += size
		}
		if _, err := s.ptmx.Write(b[start:i]); err != nil {
			return err
		}
	}
	// Trailing gap: extend the same inter-key spacing to the final
	// keystroke so the caller's next action (e.g., snapshot) isn't
	// taken mid-response to the last byte.
	if !first {
		time.Sleep(delay)
	}
	return nil
}

// escapeSeqEnd returns the index just past the end of the VT escape
// sequence that starts at b[i] (which must be 0x1b). Handles CSI
// (ESC [ ... letter), SS3 (ESC O x), OSC (ESC ] ... BEL|ST), and
// single-byte alt-key (ESC x) forms. Falls through on truncation.
func escapeSeqEnd(b []byte, i int) int {
	i++ // past ESC
	if i >= len(b) {
		return i
	}
	switch b[i] {
	case '[': // CSI: params, then final byte in 0x40..0x7e
		i++
		for i < len(b) {
			c := b[i]
			if c >= 0x40 && c <= 0x7e {
				return i + 1
			}
			i++
		}
		return i
	case 'O': // SS3: one following byte
		if i+1 < len(b) {
			return i + 2
		}
		return i + 1
	case ']': // OSC: terminated by BEL (0x07) or ST (ESC \)
		i++
		for i < len(b) {
			if b[i] == 0x07 {
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return i
	default:
		// ESC + single byte (alt-key or bare ESC)
		return i + 1
	}
}

// View returns a snapshot in the requested format.
func (s *Session) View(f Format) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(f)
}

// snapshotLocked captures a Snapshot. Caller must hold s.mu.
func (s *Session) snapshotLocked(f Format) (*Snapshot, error) {
	var emit libghostty.FormatterFormat
	switch f {
	case FormatPlain:
		emit = libghostty.FormatterFormatPlain
	case FormatVT:
		emit = libghostty.FormatterFormatVT
	case FormatHTML:
		emit = libghostty.FormatterFormatHTML
	default:
		return nil, fmt.Errorf("unknown format %d", f)
	}

	formatter, err := libghostty.NewFormatter(s.vt,
		libghostty.WithFormatterFormat(emit),
		libghostty.WithFormatterTrim(true),
	)
	if err != nil {
		return nil, fmt.Errorf("formatter: %w", err)
	}
	defer formatter.Close()

	screen, err := formatter.FormatString()
	if err != nil {
		return nil, fmt.Errorf("format: %w", err)
	}

	snap := &Snapshot{
		Screen: screen,
		Cols:   s.cols,
		Rows:   s.rows,
	}

	// Pull cursor position. RenderState.Update snapshots the terminal
	// into rs; then we read viewport coords (0-indexed x=col, y=row)
	// and convert to 1-indexed for the public API.
	if err := s.rs.Update(s.vt); err == nil {
		if vis, err := s.rs.CursorVisible(); err == nil {
			snap.CursorVis = vis
		}
		if inView, err := s.rs.CursorViewportHasValue(); err == nil && inView {
			if x, err := s.rs.CursorViewportX(); err == nil {
				snap.CursorCol = uint16(x) + 1
			}
			if y, err := s.rs.CursorViewportY(); err == nil {
				snap.CursorRow = uint16(y) + 1
			}
		}
	}

	return snap, nil
}

// Subscribe registers a watcher. Returns a channel delivering live output
// chunks plus an unsubscribe func. The channel is closed when the session
// exits (whether Close was called or the child died naturally).
func (s *Session) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	if s.state == StateExited {
		// Already exited: give the subscriber a closed channel so
		// their read loop terminates cleanly.
		close(ch)
		s.mu.Unlock()
		return ch, func() {}
	}
	s.subs[id] = ch
	s.mu.Unlock()

	unsub := func() {
		s.mu.Lock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
		s.mu.Unlock()
	}
	return ch, unsub
}

// PrimeBytes returns the current screen reconstructed as VT output. Callers
// feed this to a watcher on connect so they see current state rather than
// a blank terminal until the next redraw.
func (s *Session) PrimeBytes() ([]byte, error) {
	snap, err := s.View(FormatVT)
	if err != nil {
		return nil, err
	}
	return []byte(snap.Screen), nil
}

// SubscribeWithPrime atomically captures the current screen as VT bytes
// and registers a new subscriber to receive subsequent output. Returns
// the prime bytes, a channel delivering live chunks, and an unsub func.
//
// This exists to close a race in the watch flow: capturing prime then
// subscribing as two separate steps means any bytes emitted between
// those calls are baked into the grid (and thus the prime the caller
// already has) but never flow through the new subscriber's channel,
// leaving the caller's local terminal drifted from the true state.
func (s *Session) SubscribeWithPrime() ([]byte, <-chan []byte, func(), error) {
	s.mu.Lock()
	snap, err := s.snapshotLocked(FormatVT)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, nil, err
	}
	// Prepend cursor-home + clear-screen so the receiving terminal
	// starts from a known-blank state. libghostty's VT formatter paints
	// the grid but doesn't issue a full clear, which can leave stale
	// content in cells the current frame doesn't write to.
	prime := append([]byte("\x1b[H\x1b[2J"), []byte(snap.Screen)...)

	ch := make(chan []byte, 64)
	id := s.nextSub
	s.nextSub++
	if s.state == StateExited {
		close(ch)
		s.mu.Unlock()
		return prime, ch, func() {}, nil
	}
	s.subs[id] = ch
	s.mu.Unlock()

	unsub := func() {
		s.mu.Lock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
		s.mu.Unlock()
	}
	return prime, ch, unsub, nil
}

// Resize updates dimensions for both the PTY and libghostty.
func (s *Session) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("resize: cols and rows must be > 0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return fmt.Errorf("pty setsize: %w", err)
	}
	if err := s.vt.Resize(cols, rows, 0, 0); err != nil {
		return fmt.Errorf("vt resize: %w", err)
	}
	s.cols = cols
	s.rows = rows
	return nil
}

// Stop sends SIGTERM to the child process.
func (s *Session) Stop() error { return s.signal(syscall.SIGTERM) }

// Kill sends SIGKILL to the child process.
func (s *Session) Kill() error { return s.signal(syscall.SIGKILL) }

func (s *Session) signal(sig syscall.Signal) error {
	s.mu.Lock()
	if s.state == StateExited {
		s.mu.Unlock()
		return errors.New("session has already exited")
	}
	p := s.cmd.Process
	s.mu.Unlock()
	if p == nil {
		return errors.New("session has no process")
	}
	return p.Signal(sig)
}

// Wait blocks until the child has exited and the reader has drained.
func (s *Session) Wait() { <-s.done }

// Done returns a channel closed when the session has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close terminates the child (if running) and releases resources.
// Safe to call multiple times.
func (s *Session) Close() error {
	s.mu.Lock()
	running := s.state != StateExited
	s.mu.Unlock()
	if running {
		_ = s.Kill()
		<-s.done
	}
	s.mu.Lock()
	if s.rs != nil {
		s.rs.Close()
		s.rs = nil
	}
	if s.vt != nil {
		s.vt.Close()
		s.vt = nil
	}
	s.mu.Unlock()
	return nil
}

// State returns the current lifecycle state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// ExitCode returns the child's exit code. Only meaningful once State() == StateExited.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// PID returns the child's OS process ID.
func (s *Session) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Size returns the current dimensions.
func (s *Session) Size() (cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// newID returns an 8-hex-character random session ID.
func newID() string {
	var b [4]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		// crypto/rand should not fail; fall back to a time-based ID.
		t := time.Now().UnixNano()
		return fmt.Sprintf("%08x", uint32(t))
	}
	return hex.EncodeToString(b[:])
}
