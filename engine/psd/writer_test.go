package psd

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"math"
	"testing"
	"unicode/utf16"
)

// bw builds big endian test data
type bw struct{ bytes.Buffer }

func (b *bw) u8(v uint8)    { b.WriteByte(v) }
func (b *bw) u16(v uint16)  { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *bw) i16(v int16)   { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *bw) u32(v uint32)  { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *bw) i32(v int32)   { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *bw) i64(v int64)   { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *bw) f64(v float64) { b.i64(int64(math.Float64bits(v))) }
func (b *bw) str(s string)  { b.WriteString(s) }

// count writes an RLE row count, 4 bytes in a large document
func (b *bw) count(n int, large bool) {
	if large {
		b.u32(uint32(n))
	} else {
		b.u16(uint16(n))
	}
}

// length writes a section length, 8 bytes in a large document
func (b *bw) length(n int, large bool) {
	if large {
		b.i64(int64(n))
	} else {
		b.u32(uint32(n))
	}
}

func (b *bw) unicode(s string) {
	u := utf16.Encode([]rune(s))
	b.u32(uint32(len(u)))
	for _, c := range u {
		b.u16(c)
	}
}

func (b *bw) pascal(s string, pad int) {
	b.u8(uint8(len(s)))
	b.str(s)
	for n := len(s) + 1; n%pad != 0; n++ {
		b.u8(0)
	}
}

// key writes a descriptor key, four character keys behind a zero length
func (b *bw) key(k string) {
	if len(k) == 4 {
		b.u32(0)
	} else {
		b.u32(uint32(len(k)))
	}
	b.str(k)
}

// desc is a descriptor written for tests, items in order
type desc struct {
	class string
	items []kv
}

type kv struct {
	key string
	val any
}

// Values for desc items: *desc, []any, float64, UnitFloat, bool, int32, int64,
// Enum, string, RawData, Class
func (b *bw) descriptor(d *desc) {
	b.unicode("")
	b.key(d.class)
	b.u32(uint32(len(d.items)))
	for _, it := range d.items {
		b.key(it.key)
		b.value(it.val)
	}
}

func (b *bw) value(v any) {
	switch x := v.(type) {
	case *desc:
		b.str("Objc")
		b.descriptor(x)
	case []any:
		b.str("VlLs")
		b.u32(uint32(len(x)))
		for _, e := range x {
			b.value(e)
		}
	case float64:
		b.str("doub")
		b.f64(x)
	case UnitFloat:
		b.str("UntF")
		b.str(x.Unit)
		b.f64(x.Value)
	case bool:
		b.str("bool")
		if x {
			b.u8(1)
		} else {
			b.u8(0)
		}
	case int32:
		b.str("long")
		b.i32(x)
	case int64:
		b.str("comp")
		b.i64(x)
	case Enum:
		b.str("enum")
		b.key(x.Type)
		b.key(x.Value)
	case string:
		b.str("TEXT")
		b.unicode(x)
	case RawData:
		b.str("tdta")
		b.u32(uint32(len(x)))
		b.Write(x)
	case Class:
		b.str("type")
		b.unicode(x.Name)
		b.key(x.ID)
	default:
		panic("unsupported test value")
	}
}

func rgb(r, g, bl float64) *desc {
	return &desc{class: "RGBC", items: []kv{{"Rd  ", r}, {"Grn ", g}, {"Bl  ", bl}}}
}

func pct(v float64) UnitFloat { return UnitFloat{Unit: "#Prc", Value: v} }
func px(v float64) UnitFloat  { return UnitFloat{Unit: "#Pxl", Value: v} }
func ang(v float64) UnitFloat { return UnitFloat{Unit: "#Ang", Value: v} }

// descBlock is a descriptor behind a version number, as most blocks store one
func descBlock(versions []uint32, d *desc) []byte {
	var b bw
	for _, v := range versions {
		b.u32(v)
	}
	b.descriptor(d)
	return b.Bytes()
}

// packBits run length encodes one row
func packBits(row []byte) []byte {
	var out []byte
	i := 0
	for i < len(row) {
		j := i + 1
		for j < len(row) && row[j] == row[i] && j-i < 128 {
			j++
		}
		if j-i >= 2 {
			out = append(out, byte(int8(1-(j-i))), row[i])
			i = j
			continue
		}
		j = i + 1
		for j < len(row) && j-i < 128 && (j+1 >= len(row) || row[j] != row[j+1]) {
			j++
		}
		out = append(out, byte(j-i-1))
		out = append(out, row[i:j]...)
		i = j
	}
	return out
}

// encodeChannel compresses a plane of w by h with compression 0 to 3, the
// compression field first. RLE row counts take 4 bytes in a large document
func encodeChannel(plane []byte, w, h, comp int, large bool) []byte {
	var b bw
	b.u16(uint16(comp))
	switch comp {
	case 0:
		b.Write(plane)
	case 1:
		var rows [][]byte
		for y := range h {
			rows = append(rows, packBits(plane[y*w:(y+1)*w]))
		}
		for _, r := range rows {
			b.count(len(r), large)
		}
		for _, r := range rows {
			b.Write(r)
		}
	case 2, 3:
		src := plane
		if comp == 3 {
			src = append([]byte(nil), plane...)
			for y := range h {
				row := src[y*w : (y+1)*w]
				for x := w - 1; x > 0; x-- {
					row[x] -= row[x-1]
				}
			}
		}
		zw := zlib.NewWriter(&b)
		zw.Write(src)
		zw.Close()
	}
	return b.Bytes()
}

type testChannel struct {
	id    int
	plane []byte
}

type testMask struct {
	rect     image.Rectangle
	def      uint8
	flags    uint8
	real     *testMask
	rawExtra []byte
}

type testBlock struct {
	key  string
	data []byte
}

type testLayer struct {
	name     string
	rect     image.Rectangle
	blend    string
	opacity  uint8
	clipping bool
	flags    uint8
	channels []testChannel
	comp     int
	mask     *testMask
	ranges   []byte
	blocks   []testBlock
}

type testDoc struct {
	w, h      int
	channels  int
	resources map[int][]byte
	layers    []testLayer
	negative  bool
	global    []testBlock
	merged    [][]byte
	mergedRLE bool
	// large writes a PSB document
	large bool
	// depth is the bits per channel written in the header, 8 when zero
	depth int
	// lr16 moves the layer info into an Lr16 global block, as 16 bit documents
	// store it
	lr16 bool
}

func writeRect(b *bw, r image.Rectangle) {
	b.i32(int32(r.Min.Y))
	b.i32(int32(r.Min.X))
	b.i32(int32(r.Max.Y))
	b.i32(int32(r.Max.X))
}

// build writes a document in the PSD format
func (d testDoc) build() []byte {
	var b bw
	b.str("8BPS")
	if d.large {
		b.u16(2)
	} else {
		b.u16(1)
	}
	b.Write(make([]byte, 6))
	ch := d.channels
	if ch == 0 {
		ch = 3
	}
	b.u16(uint16(ch))
	b.u32(uint32(d.h))
	b.u32(uint32(d.w))
	depth := d.depth
	if depth == 0 {
		depth = 8
	}
	b.u16(uint16(depth))
	b.u16(3)
	b.u32(0)

	var res bw
	for id, data := range d.resources {
		res.str("8BIM")
		res.u16(uint16(id))
		res.u16(0)
		res.u32(uint32(len(data)))
		res.Write(data)
		if len(data)%2 == 1 {
			res.u8(0)
		}
	}
	b.u32(uint32(res.Len()))
	b.Write(res.Bytes())

	var li bw
	n := int16(len(d.layers))
	if d.negative {
		n = -n
	}
	li.i16(n)
	var data bw
	for _, l := range d.layers {
		writeRect(&li, l.rect)
		li.u16(uint16(len(l.channels)))
		var encoded [][]byte
		for _, c := range l.channels {
			w, h := l.rect.Dx(), l.rect.Dy()
			if c.id <= -2 && l.mask != nil {
				m := l.mask
				if c.id == -3 {
					m = m.real
				}
				w, h = m.rect.Dx(), m.rect.Dy()
			}
			e := encodeChannel(c.plane, w, h, l.comp, d.large)
			encoded = append(encoded, e)
			li.i16(int16(c.id))
			li.length(len(e), d.large)
		}
		for _, e := range encoded {
			data.Write(e)
		}
		li.str("8BIM")
		blend := l.blend
		if blend == "" {
			blend = "norm"
		}
		li.str(blend)
		li.u8(l.opacity)
		if l.clipping {
			li.u8(1)
		} else {
			li.u8(0)
		}
		li.u8(l.flags)
		li.u8(0)
		var extra bw
		if l.mask == nil {
			extra.u32(0)
		} else {
			var m bw
			writeRect(&m, l.mask.rect)
			m.u8(l.mask.def)
			m.u8(l.mask.flags)
			if r := l.mask.real; r != nil {
				m.u8(r.flags)
				m.u8(r.def)
				writeRect(&m, r.rect)
			}
			m.Write(l.mask.rawExtra)
			if m.Len() == 18 {
				m.u16(0)
			}
			extra.u32(uint32(m.Len()))
			extra.Write(m.Bytes())
		}
		extra.u32(uint32(len(l.ranges)))
		extra.Write(l.ranges)
		extra.pascal(l.name, 4)
		for _, blk := range l.blocks {
			extra.str("8BIM")
			extra.str(blk.key)
			data := blk.data
			for len(data)%2 != 0 {
				data = append(data, 0)
			}
			extra.length(len(data), d.large && bigKeys[blk.key])
			extra.Write(data)
		}
		li.u32(uint32(extra.Len()))
		li.Write(extra.Bytes())
	}
	li.Write(data.Bytes())
	if li.Len()%2 == 1 {
		li.u8(0)
	}

	var lm bw
	global := d.global
	if d.lr16 {
		lm.length(0, d.large)
		global = append([]testBlock{{"Lr16", li.Bytes()}}, global...)
	} else {
		lm.length(li.Len(), d.large)
		lm.Write(li.Bytes())
	}
	lm.u32(0)
	for _, blk := range global {
		lm.str("8BIM")
		lm.str(blk.key)
		lm.length(len(blk.data), d.large && bigKeys[blk.key])
		lm.Write(blk.data)
		for n := len(blk.data); n%4 != 0; n++ {
			lm.u8(0)
		}
	}
	b.length(lm.Len(), d.large)
	b.Write(lm.Bytes())

	if d.mergedRLE {
		b.u16(1)
		var rows [][]byte
		for _, p := range d.merged {
			for y := range d.h {
				rows = append(rows, packBits(p[y*d.w:(y+1)*d.w]))
			}
		}
		for _, r := range rows {
			b.count(len(r), d.large)
		}
		for _, r := range rows {
			b.Write(r)
		}
	} else {
		b.u16(0)
		for _, p := range d.merged {
			b.Write(p)
		}
	}
	return b.Bytes()
}

func decodeDoc(t *testing.T, d testDoc) *File {
	t.Helper()
	raw := d.build()
	f, err := Decode(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return f
}

// pattern returns a plane of n bytes that varies with the seed and has runs, so
// RLE uses both kinds of packet
func pattern(n int, seed byte) []byte {
	p := make([]byte, n)
	for i := range p {
		if (i/7)%2 == 0 {
			p[i] = seed
		} else {
			p[i] = byte(i*31) + seed
		}
	}
	return p
}

func u32b(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func unicodeBlock(s string) []byte {
	var b bw
	b.unicode(s)
	return b.Bytes()
}

func sectionBlock(kind uint32, blend string) []byte {
	var b bw
	b.u32(kind)
	if blend != "" {
		b.str("8BIM")
		b.str(blend)
	}
	return b.Bytes()
}
