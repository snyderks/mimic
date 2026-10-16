package proxyshop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Object is a JSON object that keeps its members in the order they were read
// or added, so a template written from a recipe reads in the order its author
// chose rather than sorted
type Object []Member

// Member is one key and its raw value
type Member struct {
	Key   string
	Value json.RawMessage
}

// Get returns the raw value of key, or nil when it is absent
func (o Object) Get(key string) json.RawMessage {
	for _, m := range o {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

// Has reports whether key is present
func (o Object) Has(key string) bool { return o.Get(key) != nil }

// Set replaces the value of key in place, or appends it when it is new
func (o *Object) Set(key string, v any) error {
	raw, err := marshalRaw(v)
	if err != nil {
		return err
	}
	for i := range *o {
		if (*o)[i].Key == key {
			(*o)[i].Value = raw
			return nil
		}
	}
	*o = append(*o, Member{Key: key, Value: raw})
	return nil
}

// Delete removes key when it is present
func (o *Object) Delete(key string) {
	out := (*o)[:0]
	for _, m := range *o {
		if m.Key != key {
			out = append(out, m)
		}
	}
	*o = out
}

// Keys lists the keys in order
func (o Object) Keys() []string {
	keys := make([]string, len(o))
	for i, m := range o {
		keys[i] = m.Key
	}
	return keys
}

// UnmarshalJSON reads an object and keeps its member order
func (o *Object) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected an object, found %v", tok)
	}
	*o = (*o)[:0]
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("expected a key, found %v", tok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		*o = append(*o, Member{Key: key, Value: raw})
	}
	_, err = dec.Token()
	return err
}

// MarshalJSON writes the members in order
func (o Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := marshalRaw(m.Key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		if len(m.Value) == 0 {
			b.WriteString("null")
		} else {
			b.Write(m.Value)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalRaw encodes v without escaping HTML characters
func marshalRaw(v any) (json.RawMessage, error) {
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n")), nil
}

// Indent formats JSON with one space of indent per level and no trailing
// newline, keeping member order and number literals as written. It is the
// layout of the template files this package writes
func Indent(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var b bytes.Buffer
	if err := indentValue(dec, &b, 0); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after the JSON value")
	}
	return b.Bytes(), nil
}

func indentValue(dec *json.Decoder, b *bytes.Buffer, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	return indentToken(dec, b, tok, depth)
}

func indentToken(dec *json.Decoder, b *bytes.Buffer, tok json.Token, depth int) error {
	pad := func(n int) {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", n))
	}
	switch t := tok.(type) {
	case json.Delim:
		closer := byte('}')
		if t == '[' {
			closer = ']'
		}
		b.WriteByte(byte(t))
		n := 0
		for dec.More() {
			if n > 0 {
				b.WriteByte(',')
			}
			pad(depth + 1)
			if t == '{' {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				k, err := marshalRaw(key)
				if err != nil {
					return err
				}
				b.Write(k)
				b.WriteString(": ")
			}
			if err := indentValue(dec, b, depth+1); err != nil {
				return err
			}
			n++
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		if n > 0 {
			pad(depth)
		}
		b.WriteByte(closer)
	case json.Number:
		b.WriteString(t.String())
	case string:
		s, err := marshalRaw(t)
		if err != nil {
			return err
		}
		b.Write(s)
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	default:
		return fmt.Errorf("unexpected JSON token %v", tok)
	}
	return nil
}
