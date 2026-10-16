package psd

import (
	"bytes"
	"fmt"
	"strconv"
)

// Name is a name written as a value in EngineData
type Name string

// ParseEngineData reads the text engine's PostScript like data into a tree.
// Values in the tree are one of
//
//	map[string]any  for a dictionary, << /Key value >>
//	[]any           for an array, [ value value ]
//	string          for a string in parentheses, decoded from UTF16 when it has a byte order mark
//	Name            for a name used as a value, /Name
//	int             for a number without a decimal point
//	float64         for a number with one
//	bool            for true and false
func ParseEngineData(b []byte) (map[string]any, error) {
	p := &engineParser{b: b}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("psd: engine data: top level is %T and not a dictionary", v)
	}
	return m, nil
}

type engineParser struct {
	b []byte
	p int
}

func (p *engineParser) errorf(format string, args ...any) error {
	return fmt.Errorf("psd: engine data at byte %d: %s", p.p, fmt.Sprintf(format, args...))
}

func isEngineSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == 0
}

func (p *engineParser) space() {
	for p.p < len(p.b) && isEngineSpace(p.b[p.p]) {
		p.p++
	}
}

func isDelimiter(c byte) bool {
	return isEngineSpace(c) || c == '/' || c == '[' || c == ']' || c == '(' || c == '<' || c == '>'
}

func (p *engineParser) word() string {
	start := p.p
	for p.p < len(p.b) && !isDelimiter(p.b[p.p]) {
		p.p++
	}
	return string(p.b[start:p.p])
}

func (p *engineParser) value(depth int) (any, error) {
	if depth > 256 {
		return nil, p.errorf("nesting too deep")
	}
	if p.p >= len(p.b) {
		return nil, p.errorf("unexpected end")
	}
	switch c := p.b[p.p]; {
	case c == '<' && p.p+1 < len(p.b) && p.b[p.p+1] == '<':
		p.p += 2
		m := map[string]any{}
		for {
			p.space()
			if p.p >= len(p.b) {
				return nil, p.errorf("unterminated dictionary")
			}
			if p.b[p.p] == '>' && p.p+1 < len(p.b) && p.b[p.p+1] == '>' {
				p.p += 2
				return m, nil
			}
			if p.b[p.p] != '/' {
				return nil, p.errorf("expected a key, found %q", p.b[p.p])
			}
			p.p++
			key := p.word()
			p.space()
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
	case c == '[':
		p.p++
		out := []any{}
		for {
			p.space()
			if p.p >= len(p.b) {
				return nil, p.errorf("unterminated array")
			}
			if p.b[p.p] == ']' {
				p.p++
				return out, nil
			}
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	case c == '(':
		return p.str()
	case c == '/':
		p.p++
		return Name(p.word()), nil
	default:
		w := p.word()
		if w == "" {
			return nil, p.errorf("unexpected %q", c)
		}
		switch w {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		if i, err := strconv.Atoi(w); err == nil {
			return i, nil
		}
		if f, err := strconv.ParseFloat(w, 64); err == nil {
			return f, nil
		}
		return Name(w), nil
	}
}

// str reads a parenthesized string, undoing backslash escapes
func (p *engineParser) str() (any, error) {
	p.p++
	var raw []byte
	for {
		if p.p >= len(p.b) {
			return nil, p.errorf("unterminated string")
		}
		c := p.b[p.p]
		p.p++
		if c == '\\' && p.p < len(p.b) {
			e := p.b[p.p]
			p.p++
			switch e {
			case 'n':
				raw = append(raw, '\n')
			case 'r':
				raw = append(raw, '\r')
			case 't':
				raw = append(raw, '\t')
			case 'b':
				raw = append(raw, '\b')
			case 'f':
				raw = append(raw, '\f')
			default:
				raw = append(raw, e)
			}
			continue
		}
		if c == ')' {
			break
		}
		raw = append(raw, c)
	}
	if bytes.HasPrefix(raw, []byte{0xfe, 0xff}) {
		return decodeUTF16(raw[2:]), nil
	}
	return string(raw), nil
}

// engineGet follows dictionary keys and array indexes through a tree
func engineGet(v any, path ...any) any {
	for _, k := range path {
		switch key := k.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[key]
		case int:
			a, ok := v.([]any)
			if !ok || key < 0 || key >= len(a) {
				return nil
			}
			v = a[key]
		}
	}
	return v
}

func engineFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func engineInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case float64:
		return int(x), true
	}
	return 0, false
}
