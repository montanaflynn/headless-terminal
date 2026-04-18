package main

import (
	"encoding/json"
	"testing"
)

// TestAsciicastV2Header verifies the header marshals into the exact JSON
// shape asciinema/agg expect. The spec is small and stable; if this test
// fails, either we broke the output or the spec has genuinely moved.
func TestAsciicastV2Header(t *testing.T) {
	h := asciicastV2Header{
		Version:   2,
		Width:     80,
		Height:    24,
		Timestamp: 1700000000,
		Env:       map[string]string{"TERM": "xterm-256color"},
		Title:     "hello",
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["version"].(float64) != 2 {
		t.Errorf("version = %v, want 2", got["version"])
	}
	if got["width"].(float64) != 80 || got["height"].(float64) != 24 {
		t.Errorf("size = %vx%v, want 80x24", got["width"], got["height"])
	}
	if got["title"].(string) != "hello" {
		t.Errorf("title = %q, want %q", got["title"], "hello")
	}
	env := got["env"].(map[string]any)
	if env["TERM"].(string) != "xterm-256color" {
		t.Errorf("TERM = %q, want xterm-256color", env["TERM"])
	}
}

// TestAsciicastV2HeaderOmitsEmptyTitle ensures an unset title doesn't
// appear in the JSON at all (spec allows title but many tools barf on
// an empty string). Same for env when nil.
func TestAsciicastV2HeaderOmitsEmptyTitle(t *testing.T) {
	h := asciicastV2Header{
		Version:   2,
		Width:     80,
		Height:    24,
		Timestamp: 1700000000,
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if containsKey(s, "title") {
		t.Errorf("empty title should be omitted, got %s", s)
	}
	if containsKey(s, "env") {
		t.Errorf("nil env should be omitted, got %s", s)
	}
}

// TestAsciicastEvent verifies the 3-element event tuple serializes to the
// expected array shape, including control-character escaping.
func TestAsciicastEvent(t *testing.T) {
	evt := [3]any{1.5, "o", "\x1b[2J\r\nhello"}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[1.5,"o","\u001b[2J\r\nhello"]`
	if string(b) != want {
		t.Errorf("event = %s, want %s", b, want)
	}
}

// containsKey returns whether the JSON object body contains a top-level
// "key": token. Coarse but sufficient for these assertions.
func containsKey(jsonBody, key string) bool {
	return stringContains(jsonBody, `"`+key+`":`)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
