// Package proxyshoptest writes small Photoshop documents for tests of code
// that converts Proxyshop templates
package proxyshoptest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// Layer is a layer of a written test document. A group lists its children
// top to bottom
type Layer struct {
	name     string
	hidden   bool
	img      *image.NRGBA
	children []Layer
	group    bool
}

// Pixels is a layer covering r, colored by c
func Pixels(r image.Rectangle, c func(x, y int) color.NRGBA) Layer {
	img := image.NewNRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c(x, y))
		}
	}
	return Layer{img: img}
}

// Named names a layer and sets whether it is hidden
func Named(l Layer, name string, hidden bool) Layer {
	l.name, l.hidden = name, hidden
	return l
}

// Folder is a group of layers given top to bottom
func Folder(name string, hidden bool, children ...Layer) Layer {
	return Layer{name: name, hidden: hidden, group: true, children: children}
}

type writer struct{ bytes.Buffer }

func (b *writer) u8(v uint8)   { b.WriteByte(v) }
func (b *writer) u16(v uint16) { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *writer) u32(v uint32) { binary.Write(&b.Buffer, binary.BigEndian, v) }
func (b *writer) i32(v int32)  { binary.Write(&b.Buffer, binary.BigEndian, v) }

// Write writes an 8 bit RGB document with raw channels, its layers given top
// to bottom
func Write(w, h int, layers ...Layer) []byte {
	type record struct {
		name    string
		rect    image.Rectangle
		hidden  bool
		section uint32
		planes  [4][]byte
	}
	var recs []record
	var flat func(ls []Layer)
	flat = func(ls []Layer) {
		for i := len(ls) - 1; i >= 0; i-- {
			l := ls[i]
			if l.group {
				recs = append(recs, record{name: "</Layer group>", section: 3})
				flat(l.children)
				recs = append(recs, record{name: l.name, hidden: l.hidden, section: 1})
				continue
			}
			rec := record{name: l.name, rect: l.img.Rect, hidden: l.hidden}
			n := l.img.Rect.Dx() * l.img.Rect.Dy()
			for c := range rec.planes {
				rec.planes[c] = make([]byte, n)
			}
			for i := 0; i < n; i++ {
				for c := 0; c < 4; c++ {
					rec.planes[c][i] = l.img.Pix[4*i+c]
				}
			}
			recs = append(recs, rec)
		}
	}
	flat(layers)

	var info writer
	info.u16(uint16(len(recs)))
	ids := []int16{-1, 0, 1, 2}
	for _, r := range recs {
		info.i32(int32(r.rect.Min.Y))
		info.i32(int32(r.rect.Min.X))
		info.i32(int32(r.rect.Max.Y))
		info.i32(int32(r.rect.Max.X))
		info.u16(4)
		n := r.rect.Dx() * r.rect.Dy()
		for _, id := range ids {
			binary.Write(&info.Buffer, binary.BigEndian, id)
			info.u32(uint32(2 + n))
		}
		info.WriteString("8BIM")
		if r.section == 1 {
			info.WriteString("pass")
		} else {
			info.WriteString("norm")
		}
		info.u8(255)
		info.u8(0)
		flags := uint8(0)
		if r.hidden {
			flags |= 2
		}
		info.u8(flags)
		info.u8(0)
		var extra writer
		extra.u32(0)
		extra.u32(0)
		extra.u8(uint8(len(r.name)))
		extra.WriteString(r.name)
		for k := len(r.name) + 1; k%4 != 0; k++ {
			extra.u8(0)
		}
		if r.section != 0 {
			extra.WriteString("8BIMlsct")
			extra.u32(12)
			extra.u32(r.section)
			extra.WriteString("8BIMpass")
		}
		info.u32(uint32(extra.Len()))
		info.Write(extra.Bytes())
	}
	for _, r := range recs {
		for _, c := range []int{3, 0, 1, 2} {
			info.u16(0)
			info.Write(r.planes[c])
		}
	}
	if info.Len()%2 == 1 {
		info.u8(0)
	}

	var b writer
	b.WriteString("8BPS")
	b.u16(1)
	b.Write(make([]byte, 6))
	b.u16(3)
	b.u32(uint32(h))
	b.u32(uint32(w))
	b.u16(8)
	b.u16(3)
	b.u32(0)
	b.u32(0)
	b.u32(uint32(4 + info.Len() + 4))
	b.u32(uint32(info.Len()))
	b.Write(info.Bytes())
	b.u32(0)
	b.u16(0)
	b.Write(make([]byte, 3*w*h))
	return b.Bytes()
}
