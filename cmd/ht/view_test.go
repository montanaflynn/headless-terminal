package main

import (
	"bytes"
	"testing"

	"headless-terminal/internal/protocol"
)

func TestWriteScreen_AppendsCursorLineForPlain(t *testing.T) {
	var buf bytes.Buffer
	cur := &protocol.Cursor{Row: 13, Col: 38, Visible: true}
	writeScreen(&buf, "hello\n", cur, "plain")
	want := "hello\ncursor: 13,38\n"
	if got := buf.String(); got != want {
		t.Errorf("plain output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteScreen_AppendsCursorLineForAnsi(t *testing.T) {
	var buf bytes.Buffer
	cur := &protocol.Cursor{Row: 2, Col: 5, Visible: true}
	writeScreen(&buf, "\x1b[2Jhi", cur, "ansi")
	want := "\x1b[2Jhi\ncursor: 2,5\n"
	if got := buf.String(); got != want {
		t.Errorf("ansi output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteScreen_SkipsCursorForHTML(t *testing.T) {
	var buf bytes.Buffer
	cur := &protocol.Cursor{Row: 1, Col: 1, Visible: true}
	writeScreen(&buf, "<pre>x</pre>\n", cur, "html")
	want := "<pre>x</pre>\n"
	if got := buf.String(); got != want {
		t.Errorf("html should omit cursor line\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteScreen_NilCursor(t *testing.T) {
	var buf bytes.Buffer
	writeScreen(&buf, "foo\n", nil, "plain")
	want := "foo\n"
	if got := buf.String(); got != want {
		t.Errorf("nil cursor should produce no cursor line\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteScreen_HiddenCursorAnnotated(t *testing.T) {
	var buf bytes.Buffer
	cur := &protocol.Cursor{Row: 4, Col: 9, Visible: false}
	writeScreen(&buf, "x\n", cur, "plain")
	want := "x\ncursor: 4,9 (hidden)\n"
	if got := buf.String(); got != want {
		t.Errorf("hidden cursor label mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWriteScreen_InsertsNewlineBeforeCursor(t *testing.T) {
	// Screen without trailing newline should get one before the cursor line.
	var buf bytes.Buffer
	cur := &protocol.Cursor{Row: 1, Col: 1, Visible: true}
	writeScreen(&buf, "no-nl", cur, "plain")
	want := "no-nl\ncursor: 1,1\n"
	if got := buf.String(); got != want {
		t.Errorf("missing-newline handling mismatch\n got: %q\nwant: %q", got, want)
	}
}
