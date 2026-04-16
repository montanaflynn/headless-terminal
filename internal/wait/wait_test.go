package wait_test

import (
	"context"
	"testing"
	"time"

	"headless-terminal/internal/session"
	"headless-terminal/internal/wait"
)

func startCat(t *testing.T) *session.Session {
	t.Helper()
	s, err := session.Start(session.Options{Cmd: []string{"cat"}})
	if err != nil {
		t.Fatalf("Start cat: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWait_NoCondition(t *testing.T) {
	s := startCat(t)
	_, err := wait.Wait(context.Background(), s, wait.Condition{})
	if err == nil {
		t.Error("expected error for empty condition")
	}
}

func TestWait_Idle(t *testing.T) {
	s := startCat(t)
	// cat emits nothing on its own; idle timer should fire after 100ms.
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Idle:    100 * time.Millisecond,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "idle" {
		t.Errorf("got %+v, want matched=true trigger=idle", r)
	}
}

func TestWait_IdleResetsOnOutput(t *testing.T) {
	s := startCat(t)
	// Send output in two bursts 80ms apart. Idle window is 120ms; the
	// first burst starts the timer, the second should reset it. After
	// the second burst, another 120ms of quiet is required.
	go func() {
		_ = s.Send([]byte("burst1\n"))
		time.Sleep(80 * time.Millisecond)
		_ = s.Send([]byte("burst2\n"))
	}()
	start := time.Now()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Idle:    120 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched {
		t.Fatalf("got %+v, want matched", r)
	}
	if elapsed < 180*time.Millisecond {
		t.Errorf("elapsed=%v, expected >=180ms (second burst should reset timer)", elapsed)
	}
}

func TestWait_TextSubstring(t *testing.T) {
	s := startCat(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = s.Send([]byte("hello TARGET world\n"))
	}()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Text:    "TARGET",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "text" {
		t.Errorf("got %+v, want matched=true trigger=text", r)
	}
}

func TestWait_TextRegex(t *testing.T) {
	s := startCat(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = s.Send([]byte("value=42\n"))
	}()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Text:    `value=\d+`,
		Regex:   true,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched {
		t.Errorf("got %+v, want matched", r)
	}
}

func TestWait_BadRegex(t *testing.T) {
	s := startCat(t)
	_, err := wait.Wait(context.Background(), s, wait.Condition{
		Text:  "(",
		Regex: true,
	})
	if err == nil {
		t.Error("expected error on invalid regex")
	}
}

func TestWait_Cursor(t *testing.T) {
	s, err := session.Start(session.Options{Cmd: []string{"bash", "-c", "sleep 0.08 && printf '\\033[5;10H'; sleep 1"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Cursor:  &wait.CursorCheck{Row: 5, Col: 10},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "cursor" {
		t.Errorf("got %+v, want matched=true trigger=cursor", r)
	}
}

func TestWait_Exit(t *testing.T) {
	s, err := session.Start(session.Options{Cmd: []string{"bash", "-c", "sleep 0.1"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Exit:    true,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "exit" {
		t.Errorf("got %+v, want matched=true trigger=exit", r)
	}
}

func TestWait_ExitAlreadyExited(t *testing.T) {
	s, err := session.Start(session.Options{Cmd: []string{"true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()
	s.Wait() // child already done

	r, err := wait.Wait(context.Background(), s, wait.Condition{Exit: true})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched {
		t.Errorf("got %+v, want matched on already-exited", r)
	}
}

func TestWait_Timeout(t *testing.T) {
	s := startCat(t) // cat blocks indefinitely waiting for input
	start := time.Now()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Text:    "never-appears",
		Timeout: 150 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.Matched || r.Trigger != "timeout" {
		t.Errorf("got %+v, want matched=false trigger=timeout", r)
	}
	if elapsed < 150*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Errorf("elapsed=%v, expected ~150ms", elapsed)
	}
}

func TestWait_ANDComposition(t *testing.T) {
	// Require both TARGET to appear AND 100ms of idle. Send TARGET,
	// then keep emitting dots for 80ms (below idle threshold), then
	// stop. Wait should succeed ~100ms after dots stop.
	s := startCat(t)
	go func() {
		_ = s.Send([]byte("TARGET\n"))
		deadline := time.Now().Add(80 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = s.Send([]byte("."))
			time.Sleep(10 * time.Millisecond)
		}
	}()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Text:    "TARGET",
		Idle:    100 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched {
		t.Errorf("got %+v, want matched", r)
	}
	// Trigger should reflect idle (the last condition to come true)
	// OR text (still valid). We accept either for this test since both
	// were required; we just want to know which filled in last.
	if r.Trigger != "text" && r.Trigger != "idle" {
		t.Errorf("trigger = %q, want text or idle", r.Trigger)
	}
}

func TestWait_Change_FiresOnFirstChunk(t *testing.T) {
	s := startCat(t)
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = s.Send([]byte("chunk\n"))
	}()
	start := time.Now()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Change:  true,
		Timeout: time.Second,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched || r.Trigger != "change" {
		t.Errorf("got %+v, want matched=true trigger=change", r)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("elapsed=%v, expected prompt return after first chunk", elapsed)
	}
}

func TestWait_Change_TimesOutWithNoOutput(t *testing.T) {
	s := startCat(t) // cat emits nothing without input
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Change:  true,
		Timeout: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if r.Matched || r.Trigger != "timeout" {
		t.Errorf("got %+v, want matched=false trigger=timeout", r)
	}
}

func TestWait_Change_AND_Idle(t *testing.T) {
	// Burst detector: wait for activity to start, then settle.
	s := startCat(t)
	go func() {
		// Nothing for 80ms (change should NOT fire yet), then a burst,
		// then quiet. Change+Idle should fire after the burst settles.
		time.Sleep(80 * time.Millisecond)
		_ = s.Send([]byte("a"))
		time.Sleep(20 * time.Millisecond)
		_ = s.Send([]byte("b"))
	}()
	start := time.Now()
	r, err := wait.Wait(context.Background(), s, wait.Condition{
		Change:  true,
		Idle:    120 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !r.Matched {
		t.Fatalf("got %+v, want matched", r)
	}
	// Must be at least 80ms (no change before) + 20ms (between sends)
	// + 120ms (idle) = 220ms, with slack.
	if elapsed < 200*time.Millisecond {
		t.Errorf("elapsed=%v, too fast: change should have waited for burst+idle", elapsed)
	}
}

func TestWait_ContextCancel(t *testing.T) {
	s := startCat(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := wait.Wait(ctx, s, wait.Condition{
		Text: "never",
	})
	if err == nil {
		t.Error("expected context-canceled error")
	}
}
