// Package daemon implements the ht background server: it owns a map of
// live sessions and speaks the protocol over a Unix socket. One goroutine
// per connected client; the server never mutates client state directly,
// only through session methods which are internally synchronized.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"headless-terminal/internal/protocol"
	"headless-terminal/internal/session"
	"headless-terminal/internal/wait"
)

// Daemon holds all live sessions and serves clients.
type Daemon struct {
	mu       sync.Mutex
	sessions map[string]*session.Session // by ID
	names    map[string]string           // name -> ID
	// pendingWatches holds watchers waiting for a session with a given
	// name to appear (follow mode). Keyed by name. Each channel receives
	// the session exactly once then is closed.
	pendingWatches map[string][]chan *session.Session

	socketPath string
	listener   net.Listener

	// shutdown coordinates a graceful exit from Run.
	shutdown chan struct{}
	once     sync.Once
}

// New constructs a Daemon that will listen at socketPath.
func New(socketPath string) *Daemon {
	return &Daemon{
		sessions:       make(map[string]*session.Session),
		names:          make(map[string]string),
		pendingWatches: make(map[string][]chan *session.Session),
		socketPath:     socketPath,
		shutdown:       make(chan struct{}),
	}
}

// Run binds the socket and serves until ctx is canceled, DaemonStop is
// invoked, or the listener errors. It removes the socket file on exit.
func (d *Daemon) Run(ctx context.Context) error {
	if err := prepareSocketDir(d.socketPath); err != nil {
		return err
	}
	// Clean up a stale socket from a previous crashed run.
	_ = os.Remove(d.socketPath)

	l, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", d.socketPath, err)
	}
	// Socket perms: owner-only.
	_ = os.Chmod(d.socketPath, 0o600)
	d.listener = l
	defer func() {
		_ = l.Close()
		_ = os.Remove(d.socketPath)
		d.closeAllSessions()
	}()

	// Stop accepting when ctx is canceled.
	go func() {
		select {
		case <-ctx.Done():
		case <-d.shutdown:
		}
		_ = l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-d.shutdown:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go d.handleConn(conn)
	}
}

// stop triggers a graceful shutdown. Idempotent.
func (d *Daemon) stop() {
	d.once.Do(func() { close(d.shutdown) })
}

func (d *Daemon) closeAllSessions() {
	d.mu.Lock()
	sessions := make([]*session.Session, 0, len(d.sessions))
	for _, s := range d.sessions {
		sessions = append(sessions, s)
	}
	d.sessions = nil
	d.names = nil
	d.mu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
}

// handleConn reads one request, dispatches, writes response(s), and
// closes the connection. One op per connection keeps the state model
// simple (no multiplexing, no outstanding-request tracking).
func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var req protocol.Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeErr(conn, fmt.Errorf("malformed request: %w", err))
		return
	}

	switch req.Op {
	case protocol.OpRun:
		d.opRun(conn, req.Params)
	case protocol.OpList:
		d.opList(conn)
	case protocol.OpStop:
		d.opStop(conn, req.Params)
	case protocol.OpKill:
		d.opKill(conn, req.Params)
	case protocol.OpRemove:
		d.opRemove(conn, req.Params)
	case protocol.OpSend:
		d.opSend(conn, req.Params)
	case protocol.OpView:
		d.opView(conn, req.Params)
	case protocol.OpWatch:
		d.opWatch(conn, req.Params)
	case protocol.OpWait:
		d.opWait(conn, req.Params)
	case protocol.OpDaemonStop:
		writeOK(conn, nil)
		d.stop()
	default:
		writeErr(conn, fmt.Errorf("unknown op %q", req.Op))
	}
}

// --- op handlers ---

func (d *Daemon) opRun(conn net.Conn, raw json.RawMessage) {
	var p protocol.RunParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	if len(p.Cmd) == 0 {
		writeErr(conn, errors.New("cmd is required"))
		return
	}

	// Name uniqueness check.
	if p.Name != "" {
		d.mu.Lock()
		if _, exists := d.names[p.Name]; exists {
			d.mu.Unlock()
			writeErr(conn, fmt.Errorf("name %q already in use", p.Name))
			return
		}
		d.mu.Unlock()
	}

	s, err := session.Start(session.Options{
		Cmd:  p.Cmd,
		Cols: p.Cols,
		Rows: p.Rows,
		Cwd:  p.Cwd,
		Env:  p.Env,
		Name: p.Name,
	})
	if err != nil {
		writeErr(conn, err)
		return
	}

	d.mu.Lock()
	d.sessions[s.ID] = s
	var waiters []chan *session.Session
	if p.Name != "" {
		d.names[p.Name] = s.ID
		// Fulfill any queued --follow watchers for this name.
		waiters = d.pendingWatches[p.Name]
		delete(d.pendingWatches, p.Name)
	}
	d.mu.Unlock()

	for _, ch := range waiters {
		ch <- s
		close(ch)
	}

	writeOK(conn, protocol.RunResult{Session: sessionInfo(s)})
}

func (d *Daemon) opList(conn net.Conn) {
	d.mu.Lock()
	out := make([]protocol.SessionInfo, 0, len(d.sessions))
	for _, s := range d.sessions {
		out = append(out, sessionInfo(s))
	}
	d.mu.Unlock()
	writeOK(conn, protocol.ListResult{Sessions: out})
}

func (d *Daemon) opStop(conn net.Conn, raw json.RawMessage) {
	var p protocol.StopParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	// Idempotent: already-exited is success.
	if s.State() == session.StateExited {
		writeOK(conn, nil)
		return
	}
	if err := s.Stop(); err != nil {
		writeErr(conn, err)
		return
	}
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-s.Done():
	case <-time.After(timeout):
		_ = s.Kill()
		<-s.Done()
	}
	writeOK(conn, nil)
}

func (d *Daemon) opKill(conn net.Conn, raw json.RawMessage) {
	var p protocol.SessionRef
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	// Idempotent: already-exited is success.
	if s.State() == session.StateExited {
		writeOK(conn, nil)
		return
	}
	if err := s.Kill(); err != nil {
		writeErr(conn, err)
		return
	}
	<-s.Done()
	writeOK(conn, nil)
}

func (d *Daemon) opRemove(conn net.Conn, raw json.RawMessage) {
	var p protocol.RemoveParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	if s.State() != session.StateExited && !p.Force {
		writeErr(conn, errors.New("session is running; use --force to remove"))
		return
	}
	_ = s.Close()

	d.mu.Lock()
	delete(d.sessions, s.ID)
	if s.Name != "" {
		delete(d.names, s.Name)
	}
	d.mu.Unlock()

	writeOK(conn, nil)
}

func (d *Daemon) opSend(conn net.Conn, raw json.RawMessage) {
	var p protocol.SendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	delay := time.Duration(p.InterKeyDelayMs) * time.Millisecond
	if err := s.SendPaced(p.Data, delay); err != nil {
		writeErr(conn, err)
		return
	}
	if p.SettleMs > 0 {
		time.Sleep(time.Duration(p.SettleMs) * time.Millisecond)
	}

	result := protocol.SendResult{}

	// Optional wait after send. A wait timeout does not fail the whole
	// op: we still want to return a view (if requested) so agents can
	// see where they got stuck.
	if p.Wait != nil {
		cond := wait.Condition{
			Idle:    time.Duration(p.Wait.IdleMs) * time.Millisecond,
			Change:  p.Wait.Change,
			Text:    p.Wait.Text,
			Regex:   p.Wait.Regex,
			Exit:    p.Wait.Exit,
			Timeout: time.Duration(p.Wait.TimeoutMs) * time.Millisecond,
		}
		if p.Wait.Cursor != nil {
			cond.Cursor = &wait.CursorCheck{Row: p.Wait.Cursor.Row, Col: p.Wait.Cursor.Col}
		}
		r, werr := wait.Wait(context.Background(), s, cond)
		if werr != nil {
			writeErr(conn, werr)
			return
		}
		result.Wait = &protocol.WaitResult{
			Matched:   r.Matched,
			Trigger:   r.Trigger,
			ElapsedMs: r.Elapsed.Milliseconds(),
		}
	}

	// Optional view after send (and after wait, if both set).
	if p.View != nil {
		format, err := parseFormat(p.View.Format)
		if err != nil {
			writeErr(conn, err)
			return
		}
		snap, err := s.View(format)
		if err != nil {
			writeErr(conn, err)
			return
		}
		vr := &protocol.ViewResult{
			ID:     s.ID,
			Cols:   snap.Cols,
			Rows:   snap.Rows,
			Screen: snap.Screen,
		}
		if snap.CursorRow > 0 && snap.CursorCol > 0 {
			vr.Cursor = &protocol.Cursor{
				Row:     snap.CursorRow,
				Col:     snap.CursorCol,
				Visible: snap.CursorVis,
			}
		}
		result.View = vr
	}

	writeOK(conn, result)
}

// parseFormat converts a wire format string to session.Format.
func parseFormat(s string) (session.Format, error) {
	switch strings.ToLower(s) {
	case "", "plain":
		return session.FormatPlain, nil
	case "ansi", "vt":
		return session.FormatVT, nil
	case "html":
		return session.FormatHTML, nil
	default:
		return 0, fmt.Errorf("unknown format %q", s)
	}
}

func (d *Daemon) opView(conn net.Conn, raw json.RawMessage) {
	var p protocol.ViewParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	format, err := parseFormat(p.Format)
	if err != nil {
		writeErr(conn, err)
		return
	}
	snap, err := s.View(format)
	if err != nil {
		writeErr(conn, err)
		return
	}
	result := protocol.ViewResult{
		ID:     s.ID,
		Cols:   snap.Cols,
		Rows:   snap.Rows,
		Screen: snap.Screen,
	}
	if snap.CursorRow > 0 && snap.CursorCol > 0 {
		result.Cursor = &protocol.Cursor{
			Row:     snap.CursorRow,
			Col:     snap.CursorCol,
			Visible: snap.CursorVis,
		}
	}
	writeOK(conn, result)
}

func (d *Daemon) opWatch(conn net.Conn, raw json.RawMessage) {
	var p protocol.WatchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		// Session doesn't exist yet: park and wait for a run with this
		// exact name to fulfill. A reader goroutine monitors the
		// connection so we can bail if the client disconnects first.
		ready := make(chan *session.Session, 1)
		d.mu.Lock()
		d.pendingWatches[p.ID] = append(d.pendingWatches[p.ID], ready)
		d.mu.Unlock()

		disconnect := make(chan struct{})
		go func() {
			buf := make([]byte, 1)
			_, _ = conn.Read(buf) // blocks until EOF/close
			close(disconnect)
		}()

		select {
		case s = <-ready:
			// got session, continue
		case <-disconnect:
			// Client went away before a session appeared. Remove our
			// entry from the queue if still there.
			d.mu.Lock()
			q := d.pendingWatches[p.ID]
			for i, ch := range q {
				if ch == ready {
					d.pendingWatches[p.ID] = append(q[:i], q[i+1:]...)
					break
				}
			}
			if len(d.pendingWatches[p.ID]) == 0 {
				delete(d.pendingWatches, p.ID)
			}
			d.mu.Unlock()
			return
		case <-d.shutdown:
			return
		}
	}

	// Ack with the session state. After this, the connection is in
	// streaming mode: frames until EOF.
	writeOK(conn, sessionInfo(s))

	// Atomic: capture prime bytes AND register subscriber under the
	// same lock. This closes the race where output between a separate
	// prime read and subscribe would be missed by the new watcher.
	prime, ch, unsub, err := s.SubscribeWithPrime()
	if err != nil {
		return
	}
	defer unsub()

	if len(prime) > 0 {
		_ = writeFrame(conn, protocol.WatchFrame{Data: prime})
	}

	for chunk := range ch {
		if err := writeFrame(conn, protocol.WatchFrame{Data: chunk}); err != nil {
			// Client disconnected; unsubscribe and return.
			return
		}
	}
	// Channel closed => session ended.
	_ = writeFrame(conn, protocol.WatchFrame{EOF: true})
}

func (d *Daemon) opWait(conn net.Conn, raw json.RawMessage) {
	var p protocol.WaitParams
	if err := json.Unmarshal(raw, &p); err != nil {
		writeErr(conn, err)
		return
	}
	s, err := d.resolve(p.ID)
	if err != nil {
		writeErr(conn, err)
		return
	}
	cond := wait.Condition{
		Idle:    time.Duration(p.IdleMs) * time.Millisecond,
		Change:  p.Change,
		Text:    p.Text,
		Regex:   p.Regex,
		Exit:    p.Exit,
		Timeout: time.Duration(p.TimeoutMs) * time.Millisecond,
	}
	if p.Cursor != nil {
		cond.Cursor = &wait.CursorCheck{Row: p.Cursor.Row, Col: p.Cursor.Col}
	}
	r, err := wait.Wait(context.Background(), s, cond)
	if err != nil {
		writeErr(conn, err)
		return
	}
	writeOK(conn, protocol.WaitResult{
		Matched:   r.Matched,
		Trigger:   r.Trigger,
		ElapsedMs: r.Elapsed.Milliseconds(),
	})
}

// resolve finds a session by full ID, unambiguous ID prefix, or name.
func (d *Daemon) resolve(ref string) (*session.Session, error) {
	if ref == "" {
		return nil, errors.New("session id required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	// Name match wins first (explicit alias).
	if id, ok := d.names[ref]; ok {
		if s, ok := d.sessions[id]; ok {
			return s, nil
		}
	}
	// Full ID match.
	if s, ok := d.sessions[ref]; ok {
		return s, nil
	}
	// Prefix match (must be unique).
	var match *session.Session
	for id, s := range d.sessions {
		if strings.HasPrefix(id, ref) {
			if match != nil {
				return nil, fmt.Errorf("session prefix %q is ambiguous", ref)
			}
			match = s
		}
	}
	if match != nil {
		return match, nil
	}
	return nil, fmt.Errorf("session %q not found", ref)
}

// --- helpers ---

func sessionInfo(s *session.Session) protocol.SessionInfo {
	cols, rows := s.Size()
	info := protocol.SessionInfo{
		ID:        s.ID,
		Name:      s.Name,
		PID:       s.PID(),
		State:     s.State().String(),
		Cols:      cols,
		Rows:      rows,
		StartedAt: s.StartedAt,
		Cmd:       s.Cmd,
	}
	if s.State() == session.StateExited {
		code := s.ExitCode()
		info.ExitCode = &code
	}
	return info
}

func writeOK(w io.Writer, data any) {
	resp := protocol.Response{OK: true}
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			writeErr(w, err)
			return
		}
		resp.Data = b
	}
	writeJSON(w, resp)
}

func writeErr(w io.Writer, err error) {
	writeJSON(w, protocol.Response{OK: false, Error: err.Error()})
}

func writeJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = w.Write(b)
}

func writeFrame(w io.Writer, f protocol.WatchFrame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// DefaultSocketPath returns the socket path under $XDG_RUNTIME_DIR or
// /tmp, scoped to the current UID.
func DefaultSocketPath() string {
	if override := os.Getenv("HT_SOCKET"); override != "" {
		return override
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "ht", "ht.sock")
	}
	uid := strconv.Itoa(syscall.Getuid())
	return filepath.Join("/tmp", "ht-"+uid, "ht.sock")
}

// prepareSocketDir ensures the parent directory exists with 0700 perms.
func prepareSocketDir(socketPath string) error {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// Tighten perms in case it pre-existed with looser ones.
	_ = os.Chmod(dir, 0o700)
	return nil
}
