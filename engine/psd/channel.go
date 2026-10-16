package psd

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// decodeChannel reads one channel of w by h pixels at one byte each. A channel
// stored with no data decodes to zeros
func (f *File) decodeChannel(ch channel, w, h int) ([]byte, error) {
	out := make([]byte, w*h)
	if w == 0 || h == 0 {
		return out, nil
	}
	if !ch.bare && ch.length < 2 {
		return out, nil
	}
	data := make([]byte, ch.length)
	if n, err := f.r.ReadAt(data, ch.offset); n < len(data) {
		return nil, fmt.Errorf("reading %d bytes at %d: %w", len(data), ch.offset, err)
	}
	comp := ch.compression
	if !ch.bare {
		comp = int(binary.BigEndian.Uint16(data))
		data = data[2:]
	}
	switch comp {
	case 0:
		if len(data) < len(out) {
			return nil, fmt.Errorf("raw data holds %d bytes for %d pixels", len(data), len(out))
		}
		copy(out, data)
	case 1:
		counts := ch.counts
		if counts == nil {
			cw := 2
			if f.Large {
				cw = 4
			}
			if len(data) < h*cw {
				return nil, fmt.Errorf("RLE row counts need %d bytes and have %d", h*cw, len(data))
			}
			counts, data = data[:h*cw], data[h*cw:]
		}
		cw := len(counts) / h
		pos := 0
		for y := range h {
			var n int
			if cw == 2 {
				n = int(binary.BigEndian.Uint16(counts[y*2:]))
			} else {
				n = int(binary.BigEndian.Uint32(counts[y*4:]))
			}
			if pos+n > len(data) {
				return nil, fmt.Errorf("RLE row %d passes the end of the data", y)
			}
			if err := unpackBits(data[pos:pos+n], out[y*w:(y+1)*w]); err != nil {
				return nil, fmt.Errorf("RLE row %d: %w", y, err)
			}
			pos += n
		}
	case 2, 3:
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("zip data: %w", err)
		}
		if _, err := io.ReadFull(zr, out); err != nil {
			return nil, fmt.Errorf("zip data: %w", err)
		}
		if comp == 3 {
			for y := range h {
				row := out[y*w : (y+1)*w]
				for x := 1; x < w; x++ {
					row[x] += row[x-1]
				}
			}
		}
	default:
		return nil, fmt.Errorf("unknown compression %d", comp)
	}
	return out, nil
}

// unpackBits expands a PackBits row into dst, which it must fill exactly
func unpackBits(src, dst []byte) error {
	i, o := 0, 0
	for i < len(src) && o < len(dst) {
		n := int(int8(src[i]))
		i++
		switch {
		case n >= 0:
			k := n + 1
			if i+k > len(src) || o+k > len(dst) {
				return fmt.Errorf("literal run of %d overflows", k)
			}
			copy(dst[o:], src[i:i+k])
			i += k
			o += k
		case n > -128:
			k := 1 - n
			if i >= len(src) || o+k > len(dst) {
				return fmt.Errorf("repeat run of %d overflows", k)
			}
			b := src[i]
			i++
			for j := range k {
				dst[o+j] = b
			}
			o += k
		}
	}
	if o != len(dst) {
		return fmt.Errorf("row decodes to %d bytes, want %d", o, len(dst))
	}
	return nil
}
