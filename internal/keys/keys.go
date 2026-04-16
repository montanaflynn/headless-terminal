// Package keys parses vim-style key notation strings into the byte
// sequences a terminal would produce if those keys were typed.
//
// Grammar:
//
//	literals:   hello  -> "hello"
//	specials:   <CR> <Enter> <Esc> <Tab> <BS> <Space> <Del> <Ins>
//	            <Up> <Down> <Left> <Right> <Home> <End>
//	            <PageUp> <PageDown> <F1>..<F12> <lt>
//	ctrl:       <C-c>     a-z only
//	meta:       <M-x>     aka <A-x>; prefixes ESC (0x1b)
//	combined:   <C-M-x>   modifiers in any order
//	shift:      <S-Tab>   (other <S-*> combos are intentionally unsupported)
//
// Names and modifiers are case-insensitive. Multiple top-level strings
// concatenate when passed separately.
package keys

import (
	"errors"
	"fmt"
	"strings"
)

// Parse converts a single notation string to its byte sequence.
func Parse(s string) ([]byte, error) {
	var out []byte
	for i := 0; i < len(s); {
		c := s[i]
		if c != '<' {
			out = append(out, c)
			i++
			continue
		}
		end := strings.IndexByte(s[i+1:], '>')
		if end < 0 {
			return nil, fmt.Errorf("unterminated `<` at position %d", i)
		}
		spec := s[i+1 : i+1+end]
		if spec == "" {
			return nil, fmt.Errorf("empty `<>` at position %d", i)
		}
		bytes, err := parseSpecial(spec)
		if err != nil {
			return nil, fmt.Errorf("at position %d: %w", i, err)
		}
		out = append(out, bytes...)
		i += 1 + end + 1
	}
	return out, nil
}

// ParseAll concatenates Parse across all inputs.
func ParseAll(ss []string) ([]byte, error) {
	var out []byte
	for _, s := range ss {
		b, err := Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

func parseSpecial(spec string) ([]byte, error) {
	// Split modifiers from the trailing name. Modifiers are "C-", "M-",
	// "A-" (alias for M), "S-" in any order.
	var ctrl, meta, shift bool
	rest := spec
	for {
		if len(rest) < 2 || rest[1] != '-' {
			break
		}
		switch rest[0] {
		case 'C', 'c':
			ctrl = true
		case 'M', 'm', 'A', 'a':
			meta = true
		case 'S', 's':
			shift = true
		default:
			return nil, fmt.Errorf("unknown modifier %q", rest[:1])
		}
		rest = rest[2:]
	}
	if rest == "" {
		return nil, errors.New("modifier without key")
	}

	// Resolve the base key to its byte sequence, then apply modifiers.
	base, err := resolveBase(rest, shift)
	if err != nil {
		return nil, err
	}

	if ctrl {
		base, err = applyCtrl(base, rest)
		if err != nil {
			return nil, err
		}
	}
	if meta {
		// Meta/Alt = ESC prefix. Standard xterm convention.
		base = append([]byte{0x1b}, base...)
	}
	return base, nil
}

// resolveBase returns the byte sequence for a named key or a literal char.
// shift is handled here for the cases we support (<S-Tab>); other shift
// combinations are rejected unless the caller handles them (e.g. <S-a>
// for which the caller should just write "A" directly).
func resolveBase(name string, shift bool) ([]byte, error) {
	// Named specials (case-insensitive).
	if seq, ok := named[strings.ToLower(name)]; ok {
		if shift {
			shifted, ok := shifted[strings.ToLower(name)]
			if !ok {
				return nil, fmt.Errorf("shift modifier not supported for <%s>", name)
			}
			return []byte(shifted), nil
		}
		return []byte(seq), nil
	}

	// Single character: either a literal or shift-applied literal.
	if len([]rune(name)) == 1 {
		r := []rune(name)[0]
		if shift {
			if r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			} else if r >= 'A' && r <= 'Z' {
				// already shifted; no-op
			} else {
				return nil, fmt.Errorf("shift modifier not supported for <%c>", r)
			}
		}
		return []byte(string(r)), nil
	}

	return nil, fmt.Errorf("unknown key name %q", name)
}

// applyCtrl converts a base sequence to its ctrl-modified form.
// Only ASCII letters a-z/A-Z are supported, matching vim convention.
func applyCtrl(base []byte, name string) ([]byte, error) {
	if len(base) != 1 {
		return nil, fmt.Errorf("ctrl modifier not supported for <%s>", name)
	}
	b := base[0]
	switch {
	case b >= 'a' && b <= 'z':
		return []byte{b - 'a' + 1}, nil
	case b >= 'A' && b <= 'Z':
		return []byte{b - 'A' + 1}, nil
	default:
		return nil, fmt.Errorf("ctrl modifier requires a letter a-z, got <%s>", name)
	}
}

// named maps a lowercased key name to its raw byte sequence.
var named = map[string]string{
	"cr":       "\r",
	"enter":    "\r",
	"return":   "\r",
	"esc":      "\x1b",
	"escape":   "\x1b",
	"tab":      "\t",
	"bs":       "\x7f",
	"backspace": "\x7f",
	"space":    " ",
	"del":      "\x1b[3~",
	"delete":   "\x1b[3~",
	"ins":      "\x1b[2~",
	"insert":   "\x1b[2~",
	"up":       "\x1b[A",
	"down":     "\x1b[B",
	"right":    "\x1b[C",
	"left":     "\x1b[D",
	"home":     "\x1b[H",
	"end":      "\x1b[F",
	"pageup":   "\x1b[5~",
	"pagedown": "\x1b[6~",
	"f1":       "\x1bOP",
	"f2":       "\x1bOQ",
	"f3":       "\x1bOR",
	"f4":       "\x1bOS",
	"f5":       "\x1b[15~",
	"f6":       "\x1b[17~",
	"f7":       "\x1b[18~",
	"f8":       "\x1b[19~",
	"f9":       "\x1b[20~",
	"f10":      "\x1b[21~",
	"f11":      "\x1b[23~",
	"f12":      "\x1b[24~",
	"lt":       "<",
}

// shifted maps a lowercased key name to its shift-modified byte sequence.
// Only the explicit shift combinations we support are listed.
var shifted = map[string]string{
	"tab": "\x1b[Z",
}
