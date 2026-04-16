package session

import (
	"strings"
	"testing"
	"time"
)

// waitFor polls f until it returns true or timeout. Returns false on timeout.
func waitFor(timeout time.Duration, f func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return f()
}

func TestStart_RunsAndExits(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"echo", "hello"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if s.State() != StateRunning {
		t.Errorf("state = %v, want running", s.State())
	}
	if s.PID() == 0 {
		t.Error("PID should be non-zero immediately after Start")
	}

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("echo never exited")
	}

	if s.State() != StateExited {
		t.Errorf("state = %v, want exited", s.State())
	}
	if code := s.ExitCode(); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestStart_RejectsEmptyCmd(t *testing.T) {
	if _, err := Start(Options{}); err == nil {
		t.Error("expected error for empty Cmd")
	}
}

func TestView_PlainTextReflectsOutput(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"echo", "hello snapshot"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait() // child exits immediately

	// Give readLoop a moment to drain any remaining output.
	waitFor(500*time.Millisecond, func() bool {
		snap, _ := s.View(FormatPlain)
		return snap != nil && strings.Contains(snap.Screen, "hello snapshot")
	})

	snap, err := s.View(FormatPlain)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if !strings.Contains(snap.Screen, "hello snapshot") {
		t.Errorf("screen = %q, want to contain %q", snap.Screen, "hello snapshot")
	}
}

func TestView_DefaultSize(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	if c, r := s.Size(); c != 80 || r != 24 {
		t.Errorf("default size = %dx%d, want 80x24", c, r)
	}
}

func TestView_CustomSize(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"true"}, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	if c, r := s.Size(); c != 120 || r != 40 {
		t.Errorf("size = %dx%d, want 120x40", c, r)
	}
}

func TestSend_DeliversToChild(t *testing.T) {
	// cat echoes stdin to stdout. We send a line, then look for it
	// in the libghostty view.
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if err := s.Send([]byte("echo-this\n")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	found := waitFor(1*time.Second, func() bool {
		snap, _ := s.View(FormatPlain)
		return snap != nil && strings.Contains(snap.Screen, "echo-this")
	})
	if !found {
		snap, _ := s.View(FormatPlain)
		t.Errorf("sent text never appeared; screen = %q", snap.Screen)
	}
}

func TestSend_FailsAfterExit(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait()
	if err := s.Send([]byte("x")); err == nil {
		t.Error("Send after exit should error")
	}
}

func TestSubscribe_ReceivesOutput(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.Send([]byte("hello-sub\n")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var got []byte
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				break loop
			}
			got = append(got, chunk...)
			if strings.Contains(string(got), "hello-sub") {
				return
			}
		case <-deadline:
			t.Fatalf("never received sent bytes; got %q", got)
		}
	}
	if !strings.Contains(string(got), "hello-sub") {
		t.Errorf("subscribe stream did not contain sent bytes; got %q", got)
	}
}

func TestSubscribe_ClosedOnExit(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"echo", "bye"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	ch, _ := s.Subscribe()
	// Drain the channel until it closes.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed as expected
			}
		case <-deadline:
			t.Fatal("subscriber channel never closed after child exit")
		}
	}
}

func TestSubscribe_AfterExitGivesClosedChannel(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait()

	ch, _ := s.Subscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel for post-exit subscription")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("subscribing after exit should return a closed channel")
	}
}

func TestPrimeBytes_ReconstructsScreen(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"echo", "prime-test"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait()
	waitFor(500*time.Millisecond, func() bool {
		b, _ := s.PrimeBytes()
		return strings.Contains(string(b), "prime-test")
	})
	b, err := s.PrimeBytes()
	if err != nil {
		t.Fatalf("PrimeBytes: %v", err)
	}
	if !strings.Contains(string(b), "prime-test") {
		t.Errorf("prime bytes missing screen content; got %q", b)
	}
}

func TestStop_TerminatesLongRunning(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"sleep", "60"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("sleep did not exit after Stop")
	}
	if s.State() != StateExited {
		t.Errorf("state = %v, want exited", s.State())
	}
}

func TestKill_TerminatesLongRunning(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"sleep", "60"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if err := s.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("sleep did not exit after Kill")
	}
}

func TestResize_Succeeds(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"sleep", "5"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if err := s.Resize(100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if c, r := s.Size(); c != 100 || r != 30 {
		t.Errorf("size after resize = %dx%d, want 100x30", c, r)
	}
}

func TestResize_RejectsZero(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"sleep", "5"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	if err := s.Resize(0, 30); err == nil {
		t.Error("expected error for zero cols")
	}
}

func TestView_CursorTracksPosition(t *testing.T) {
	// Drive the cursor with an absolute-position escape (ESC[3;10H)
	// and verify libghostty reports cursor at row 3, col 10.
	s, err := Start(Options{Cmd: []string{"bash", "-c", "printf '\\033[3;10H'; sleep 0.1"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait()

	var snap *Snapshot
	waitFor(500*time.Millisecond, func() bool {
		snap, _ = s.View(FormatPlain)
		return snap != nil && snap.CursorRow == 3 && snap.CursorCol == 10
	})
	if snap == nil || snap.CursorRow != 3 || snap.CursorCol != 10 {
		t.Errorf("cursor = %d,%d want 3,10", snap.CursorRow, snap.CursorCol)
	}
	if !snap.CursorVis {
		t.Error("cursor should be visible by default")
	}
}

func TestView_CursorHidden(t *testing.T) {
	// ESC[?25l hides the cursor.
	s, err := Start(Options{Cmd: []string{"bash", "-c", "printf '\\033[?25l'; sleep 0.1"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait()

	waitFor(500*time.Millisecond, func() bool {
		snap, _ := s.View(FormatPlain)
		return snap != nil && !snap.CursorVis
	})
	snap, _ := s.View(FormatPlain)
	if snap.CursorVis {
		t.Error("cursor should be hidden after ESC[?25l")
	}
}

func TestSendPaced_DeliversAllBytes(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	if err := s.SendPaced([]byte("abcde\n"), 10*time.Millisecond); err != nil {
		t.Fatalf("SendPaced: %v", err)
	}
	found := waitFor(2*time.Second, func() bool {
		snap, _ := s.View(FormatPlain)
		return snap != nil && strings.Contains(snap.Screen, "abcde")
	})
	if !found {
		snap, _ := s.View(FormatPlain)
		t.Errorf("paced bytes never appeared; screen=%q", snap.Screen)
	}
}

func TestSendPaced_TimingIsRoughlyCorrect(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	// 5 bytes at 30ms between keystrokes = 4 gaps × 30ms = 120ms min.
	start := time.Now()
	if err := s.SendPaced([]byte("hello"), 30*time.Millisecond); err != nil {
		t.Fatalf("SendPaced: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Errorf("elapsed=%v, expected >=100ms from pacing 5 bytes at 30ms", elapsed)
	}
}

func TestSendPaced_KeepsEscapeSequenceAtomic(t *testing.T) {
	// <F1> = ESC O P. Pacing per byte would split this and confuse the
	// receiver. With escape-aware pacing, these 3 bytes arrive together.
	// Verify by driving a cat that echoes our input: the total pacing
	// time for ESC O P + 'x' should be one gap (between F1 and x),
	// not three (if we paced per byte inside the escape sequence).
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	start := time.Now()
	if err := s.SendPaced([]byte{0x1b, 'O', 'P', 'x'}, 50*time.Millisecond); err != nil {
		t.Fatalf("SendPaced: %v", err)
	}
	elapsed := time.Since(start)
	// Two logical keystrokes (<F1>, 'x') → 1 gap of 50ms. If we paced
	// per-byte we'd see ~3 gaps (150ms). Allow slack for scheduling.
	if elapsed < 40*time.Millisecond || elapsed > 120*time.Millisecond {
		t.Errorf("elapsed=%v, expected ~50ms (one gap between F1 and x)", elapsed)
	}
}

func TestSendPaced_ZeroDelayEquivalentToSend(t *testing.T) {
	s, err := Start(Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	start := time.Now()
	if err := s.SendPaced([]byte("abcdefghij"), 0); err != nil {
		t.Fatalf("SendPaced: %v", err)
	}
	elapsed := time.Since(start)
	// No pacing → should return essentially immediately.
	if elapsed > 20*time.Millisecond {
		t.Errorf("elapsed=%v, expected <20ms for zero-delay send", elapsed)
	}
}

func TestEscapeSeqEnd_CoversCommonSequences(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want int
	}{
		{"ESC alone", []byte{0x1b}, 1},
		{"ESC + alt-x", []byte{0x1b, 'x'}, 2},
		{"SS3 F1", []byte{0x1b, 'O', 'P'}, 3},
		{"CSI arrow", []byte{0x1b, '[', 'A'}, 3},
		{"CSI PageUp", []byte{0x1b, '[', '5', '~'}, 4},
		{"CSI F5", []byte{0x1b, '[', '1', '5', '~'}, 5},
		{"OSC with BEL", []byte{0x1b, ']', '0', ';', 't', 'i', 't', 'l', 'e', 0x07}, 10},
		{"OSC with ST", []byte{0x1b, ']', '0', ';', 'x', 0x1b, '\\'}, 7},
	}
	for _, tc := range cases {
		got := escapeSeqEnd(tc.in, 0)
		if got != tc.want {
			t.Errorf("%s: escapeSeqEnd = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestNewID_UniqueAndFormatted(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id := newID()
		if len(id) != 8 {
			t.Errorf("id %q has wrong length %d", id, len(id))
		}
		if seen[id] {
			t.Errorf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
