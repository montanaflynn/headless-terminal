package keys

import (
	"bytes"
	"testing"
)

func TestParse_Literals(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"a", "a"},
		{"hello", "hello"},
		{"ihello", "ihello"},          // vim: i + hello
		{"你好", "你好"},                 // utf-8 passthrough
		{":wq", ":wq"},                // colons are literals
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_NamedSpecials(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"<CR>", []byte{'\r'}},
		{"<cr>", []byte{'\r'}},
		{"<Enter>", []byte{'\r'}},
		{"<Return>", []byte{'\r'}},
		{"<Esc>", []byte{0x1b}},
		{"<ESC>", []byte{0x1b}},
		{"<Escape>", []byte{0x1b}},
		{"<Tab>", []byte{'\t'}},
		{"<BS>", []byte{0x7f}},
		{"<Backspace>", []byte{0x7f}},
		{"<Space>", []byte{' '}},
		{"<Del>", []byte("\x1b[3~")},
		{"<Ins>", []byte("\x1b[2~")},
		{"<Up>", []byte("\x1b[A")},
		{"<Down>", []byte("\x1b[B")},
		{"<Right>", []byte("\x1b[C")},
		{"<Left>", []byte("\x1b[D")},
		{"<Home>", []byte("\x1b[H")},
		{"<End>", []byte("\x1b[F")},
		{"<PageUp>", []byte("\x1b[5~")},
		{"<PageDown>", []byte("\x1b[6~")},
		{"<F1>", []byte("\x1bOP")},
		{"<F4>", []byte("\x1bOS")},
		{"<F5>", []byte("\x1b[15~")},
		{"<F12>", []byte("\x1b[24~")},
		{"<lt>", []byte("<")},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParse_CtrlModifier(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"<C-a>", []byte{0x01}},
		{"<C-A>", []byte{0x01}},
		{"<c-a>", []byte{0x01}},
		{"<C-c>", []byte{0x03}}, // canonical interrupt
		{"<C-z>", []byte{0x1a}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParse_MetaModifier(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"<M-x>", []byte{0x1b, 'x'}},
		{"<A-x>", []byte{0x1b, 'x'}},  // A- is an alias
		{"<m-x>", []byte{0x1b, 'x'}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParse_CombinedModifiers(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"<C-M-x>", []byte{0x1b, 0x18}}, // meta wraps ctrl: ESC, ^X
		{"<M-C-x>", []byte{0x1b, 0x18}}, // order insensitive
		{"<S-Tab>", []byte("\x1b[Z")},
		{"<S-a>", []byte("A")},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParse_Mixed(t *testing.T) {
	// Realistic vim sequence: insert "hello", escape, :wq<CR>
	got, err := Parse("ihello<Esc>:wq<CR>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := append([]byte("ihello"), 0x1b)
	want = append(want, []byte(":wq")...)
	want = append(want, '\r')
	if !bytes.Equal(got, want) {
		t.Errorf("Parse mixed = %v, want %v", got, want)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []string{
		"<",              // unterminated
		"<Esc",           // unterminated
		"<>",             // empty
		"<C->",           // modifier without key
		"<C-1>",          // ctrl+digit not supported
		"<S-F1>",         // shift+function not in SLC
		"<Bogus>",        // unknown name
		"<X-a>",          // unknown modifier prefix
	}
	for _, tc := range cases {
		if _, err := Parse(tc); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", tc)
		}
	}
}

func TestParseAll_Concat(t *testing.T) {
	got, err := ParseAll([]string{"i", "hello", "<Esc>"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := append([]byte("ihello"), 0x1b)
	if !bytes.Equal(got, want) {
		t.Errorf("ParseAll = %v, want %v", got, want)
	}
}
