package psd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

// errShort reports a structure that ends before its declared contents
var errShort = errors.New("psd: unexpected end of data")

// cursor reads big endian values from a byte slice and keeps the first error, so
// a parse can read a whole structure and check once at the end
type cursor struct {
	b   []byte
	p   int
	err error
}

func newCursor(b []byte) *cursor { return &cursor{b: b} }

func (c *cursor) remaining() int { return len(c.b) - c.p }

func (c *cursor) take(n int) []byte {
	if c.err != nil {
		return nil
	}
	if n < 0 || n > len(c.b)-c.p {
		c.err = errShort
		c.p = len(c.b)
		return nil
	}
	s := c.b[c.p : c.p+n : c.p+n]
	c.p += n
	return s
}

func (c *cursor) skip(n int) { c.take(n) }

func (c *cursor) u8() uint8 {
	b := c.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (c *cursor) u16() uint16 {
	b := c.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (c *cursor) i16() int16 { return int16(c.u16()) }

func (c *cursor) u32() uint32 {
	b := c.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (c *cursor) i32() int32 { return int32(c.u32()) }

func (c *cursor) u64() uint64 {
	b := c.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func (c *cursor) i64() int64 { return int64(c.u64()) }

func (c *cursor) f64() float64 { return math.Float64frombits(c.u64()) }

func (c *cursor) f32() float32 { return math.Float32frombits(c.u32()) }

func (c *cursor) key4() string { return string(c.take(4)) }

// pascal reads a length byte and that many bytes, then skips to a multiple of pad
// counted from the length byte
func (c *cursor) pascal(pad int) string {
	n := int(c.u8())
	s := c.take(n)
	if pad > 1 {
		if r := (n + 1) % pad; r != 0 {
			c.skip(pad - r)
		}
	}
	return macRoman(s)
}

// macRomanHigh holds the characters of Mac OS Roman bytes 0x80 to 0xFF
var macRomanHigh = []rune("ÄÅÇÉÑÖÜáàâäãåçéèêëíìîïñóòôöõúùûü†°¢£§•¶ß®©™´¨≠ÆØ∞±≤≥¥µ∂∑∏π∫ªºΩæø¿¡¬√ƒ≈∆«»… ÀÃÕŒœ–—“”‘’÷◊ÿŸ⁄€‹›ﬁﬂ‡·‚„‰ÂÊÁËÈÍÎÏÌÓÔÒÚÛÙıˆ˜¯˘˙˚¸˝˛ˇ")

// macRoman decodes Mac OS Roman, the encoding of legacy names
func macRoman(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		if c < 0x80 {
			r[i] = rune(c)
		} else {
			r[i] = macRomanHigh[c-0x80]
		}
	}
	return string(r)
}

// unicode reads a count of UTF16 code units and the units, dropping a trailing NUL
func (c *cursor) unicode() string {
	n := c.u32()
	if c.err != nil {
		return ""
	}
	if int64(n)*2 > int64(c.remaining()) {
		c.err = errShort
		return ""
	}
	return decodeUTF16(c.take(int(n) * 2))
}

func decodeUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

// stream reads a file section front to back through a buffer over an io.ReaderAt
type stream struct {
	r    io.ReaderAt
	end  int64
	pos  int64
	buf  []byte
	bpos int64
	err  error
}

const streamChunk = 1 << 20

func newStream(r io.ReaderAt, off, end int64) *stream {
	return &stream{r: r, pos: off, end: end, bpos: off}
}

// read returns the next n bytes, valid until the next call
func (s *stream) read(n int64) []byte {
	if s.err != nil {
		return nil
	}
	if n < 0 || s.pos+n > s.end {
		s.err = fmt.Errorf("psd: read of %d bytes at offset %d passes the end of the section at %d", n, s.pos, s.end)
		return nil
	}
	if s.pos >= s.bpos && s.pos+n <= s.bpos+int64(len(s.buf)) {
		b := s.buf[s.pos-s.bpos : s.pos-s.bpos+n]
		s.pos += n
		return b
	}
	size := max(n, streamChunk)
	size = min(size, s.end-s.pos)
	if int64(cap(s.buf)) < size {
		s.buf = make([]byte, size)
	}
	s.buf = s.buf[:size]
	if m, err := s.r.ReadAt(s.buf, s.pos); m < len(s.buf) {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		s.err = fmt.Errorf("psd: reading at offset %d: %w", s.pos, err)
		s.buf = s.buf[:0]
		return nil
	}
	s.bpos = s.pos
	b := s.buf[:n]
	s.pos += n
	return b
}

// copyN returns the next n bytes in a slice of their own
func (s *stream) copyN(n int64) []byte {
	b := s.read(n)
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

func (s *stream) seek(off int64) { s.pos = off }

func (s *stream) u8() uint8 {
	b := s.read(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (s *stream) u16() uint16 {
	b := s.read(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (s *stream) u32() uint32 {
	b := s.read(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (s *stream) u64() uint64 {
	b := s.read(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// length reads a section length, which is 8 bytes in a large document
func (s *stream) length(big bool) int64 {
	if big {
		return int64(s.u64())
	}
	return int64(s.u32())
}

func (s *stream) key4() string { return string(s.read(4)) }
