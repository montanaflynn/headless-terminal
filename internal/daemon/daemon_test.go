package daemon_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"headless-terminal/internal/client"
	"headless-terminal/internal/daemon"
	"headless-terminal/internal/protocol"
)

var testSocketCounter atomic.Uint64

// startDaemon boots a daemon on a per-test Unix socket and returns a
// client pointed at it. The daemon shuts down when the test completes.
// Sockets live under a short /tmp path because macOS caps sun_path at
// ~104 chars, which t.TempDir() with long test names can exceed.
func startDaemon(t *testing.T) *client.Client {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("htt%d", testSocketCounter.Add(1)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	d := daemon.New(socket)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not shut down within 2s")
		}
	})

	c := client.New(socket)
	// Wait briefly for the socket to appear.
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := c.List(); err == nil {
			return c
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("daemon never became reachable: last error: %v", lastErr)
	return nil
}

func TestIntegration_RunListSendViewRemove(t *testing.T) {
	c := startDaemon(t)

	// Start a `cat` so we can write to it and see output.
	run, err := c.Run(protocol.RunParams{Cmd: []string{"cat"}, Name: "itest"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID
	if sid == "" {
		t.Fatal("empty session ID")
	}
	if run.Session.State != "running" {
		t.Errorf("state = %q, want running", run.Session.State)
	}

	// List should see it.
	list, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].ID != sid {
		t.Fatalf("List = %+v, want 1 session with id %s", list.Sessions, sid)
	}

	// Send some text, expect view to reflect it (cat echoes stdin).
	if err := c.Send(sid, []byte("integration-marker\n")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got string
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		v, err := c.View(sid, "plain")
		if err != nil {
			t.Fatalf("View: %v", err)
		}
		got = v.Screen
		if strings.Contains(got, "integration-marker") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(got, "integration-marker") {
		t.Errorf("view never contained sent marker; got %q", got)
	}

	// Resolve by prefix and by name.
	if _, err := c.View(sid[:4], "plain"); err != nil {
		t.Errorf("prefix resolution failed: %v", err)
	}
	if _, err := c.View("itest", "plain"); err != nil {
		t.Errorf("name resolution failed: %v", err)
	}

	// Kill + remove.
	if err := c.Kill(sid); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := c.Remove(sid, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, err = c.List()
	if err != nil {
		t.Fatalf("List post-remove: %v", err)
	}
	if len(list.Sessions) != 0 {
		t.Errorf("sessions after remove = %+v, want empty", list.Sessions)
	}
}

func TestIntegration_WatchStreamsAndPrimes(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{
		Cmd: []string{"bash", "-c", "echo initial; sleep 0.1; echo live1; sleep 0.1; echo live2"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID

	// Give the session a moment to emit "initial" before we subscribe,
	// so that prime-on-connect has something to replay.
	time.Sleep(50 * time.Millisecond)

	var accum []byte
	gotEOF := false
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Watch(client.WatchConfig{
			ID: sid,
			OnFrame: func(f protocol.WatchFrame) error {
				accum = append(accum, f.Data...)
				if f.EOF {
					gotEOF = true
				}
				return nil
			},
		})
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch never returned")
	}

	if !gotEOF {
		t.Error("watch did not receive EOF frame")
	}
	out := string(accum)
	for _, want := range []string{"initial", "live1", "live2"} {
		if !strings.Contains(out, want) {
			t.Errorf("watch output missing %q; got %q", want, out)
		}
	}
}

func TestIntegration_RemoveRunningRequiresForce(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{Cmd: []string{"sleep", "30"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID

	if err := c.Remove(sid, false); err == nil {
		t.Error("Remove on running session should fail without --force")
	}
	if err := c.Remove(sid, true); err != nil {
		t.Errorf("Remove --force should succeed: %v", err)
	}
}

func TestIntegration_NameUniqueness(t *testing.T) {
	c := startDaemon(t)

	if _, err := c.Run(protocol.RunParams{Cmd: []string{"sleep", "1"}, Name: "dup"}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := c.Run(protocol.RunParams{Cmd: []string{"sleep", "1"}, Name: "dup"}); err == nil {
		t.Error("second run with same name should fail")
	}
}

func TestIntegration_NotFoundError(t *testing.T) {
	c := startDaemon(t)
	if _, err := c.View("nonexistent", "plain"); err == nil {
		t.Error("View on nonexistent session should error")
	}
}

func TestIntegration_Wait_Text(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{
		Cmd: []string{"bash", "-c", "sleep 0.1; echo READY; sleep 1"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r, err := c.Wait(protocol.WaitParams{
		ID:        run.Session.ID,
		Text:      "READY",
		TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "text" {
		t.Errorf("got %+v, want matched=true trigger=text", r)
	}
}

func TestIntegration_Wait_Exit(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{Cmd: []string{"bash", "-c", "sleep 0.1"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r, err := c.Wait(protocol.WaitParams{
		ID:        run.Session.ID,
		Exit:      true,
		TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "exit" {
		t.Errorf("got %+v, want matched=true trigger=exit", r)
	}
}

func TestIntegration_SendWithWaitAndView(t *testing.T) {
	c := startDaemon(t)

	// cat echoes input; send "COMPOUND-MARKER\n" and wait for that text
	// to appear in the view, then include a snapshot.
	run, err := c.Run(protocol.RunParams{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID

	res, err := c.SendExt(protocol.SendParams{
		ID:   sid,
		Data: []byte("COMPOUND-MARKER\n"),
		Wait: &protocol.WaitParams{
			Text:      "COMPOUND-MARKER",
			TimeoutMs: 2000,
		},
		View: &protocol.SendViewOpts{Format: "plain"},
	})
	if err != nil {
		t.Fatalf("SendExt: %v", err)
	}
	if res.Wait == nil || !res.Wait.Matched || res.Wait.Trigger != "text" {
		t.Errorf("wait = %+v, want matched=true trigger=text", res.Wait)
	}
	if res.View == nil || !strings.Contains(res.View.Screen, "COMPOUND-MARKER") {
		t.Errorf("view did not include sent text; got %+v", res.View)
	}
}

func TestIntegration_SendWithWaitTimeout(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID

	// Wait for text that will never appear; view should still be returned.
	res, err := c.SendExt(protocol.SendParams{
		ID:   sid,
		Data: []byte("some-input\n"),
		Wait: &protocol.WaitParams{
			Text:      "NEVER-APPEARS",
			TimeoutMs: 100,
		},
		View: &protocol.SendViewOpts{Format: "plain"},
	})
	if err != nil {
		t.Fatalf("SendExt: %v", err)
	}
	if res.Wait == nil || res.Wait.Matched || res.Wait.Trigger != "timeout" {
		t.Errorf("wait = %+v, want matched=false trigger=timeout", res.Wait)
	}
	// Best-effort view should still be populated so agents can see state.
	if res.View == nil {
		t.Error("expected view even on wait timeout")
	}
}

func TestIntegration_WatchFollowWaitsForSession(t *testing.T) {
	c := startDaemon(t)

	// Start watching a name that doesn't exist yet.
	readyCh := make(chan struct{}, 1)
	var accum []byte
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Watch(client.WatchConfig{
			ID: "late-arrival",
			OnReady: func(*protocol.SessionInfo) {
				readyCh <- struct{}{}
			},
			OnFrame: func(f protocol.WatchFrame) error {
				accum = append(accum, f.Data...)
				return nil
			},
		})
	}()

	// Ready should not fire yet (no session with that name exists).
	select {
	case <-readyCh:
		t.Fatal("OnReady fired before session was created")
	case <-time.After(150 * time.Millisecond):
	}

	// Create the session; watcher should then attach.
	if _, err := c.Run(protocol.RunParams{
		Cmd:  []string{"bash", "-c", "echo FOLLOWED; sleep 0.1"},
		Name: "late-arrival",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case <-readyCh:
	case <-time.After(1 * time.Second):
		t.Fatal("OnReady never fired after matching session was created")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not return after session exit")
	}

	if !strings.Contains(string(accum), "FOLLOWED") {
		t.Errorf("watch accum missing expected output; got %q", accum)
	}
}

func TestIntegration_KillStopIdempotent(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{Cmd: []string{"true"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sid := run.Session.ID

	// Wait for exit so the session is in StateExited.
	if _, err := c.Wait(protocol.WaitParams{ID: sid, Exit: true, TimeoutMs: 2000}); err != nil {
		t.Fatalf("Wait exit: %v", err)
	}

	// Both should succeed (idempotent) on an already-exited session.
	if err := c.Kill(sid); err != nil {
		t.Errorf("Kill on exited session should succeed: %v", err)
	}
	if err := c.Stop(sid, 0); err != nil {
		t.Errorf("Stop on exited session should succeed: %v", err)
	}
}

func TestIntegration_Wait_Timeout(t *testing.T) {
	c := startDaemon(t)

	run, err := c.Run(protocol.RunParams{Cmd: []string{"sleep", "30"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r, err := c.Wait(protocol.WaitParams{
		ID:        run.Session.ID,
		Text:      "never-going-to-appear",
		TimeoutMs: 100,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.Matched || r.Trigger != "timeout" {
		t.Errorf("got %+v, want matched=false trigger=timeout", r)
	}
}
