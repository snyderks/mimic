// Package psdrender draws the layers of a PSD the way Photoshop composites
// them, so a converter can bake what a template's PSD shows into images. It
// works in the document's own gamma encoded sRGB, as Photoshop blends an 8 bit
// RGB document, and follows its blend modes, opacity and fill opacity, layer and
// vector masks, clipping, pass through and isolated groups, fill layers,
// adjustment layers and layer styles. Where a style has options this package
// does not model, it draws the default and reports the option in Notes
package psdrender

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// Buffer is premultiplied RGBA covering Rect, each channel from 0 to 1 in the
// document's gamma encoded space
type Buffer struct {
	Rect image.Rectangle
	Pix  []float32
}

// NewBuffer is a transparent buffer covering r
func NewBuffer(r image.Rectangle) *Buffer {
	return &Buffer{Rect: r, Pix: make([]float32, 4*r.Dx()*r.Dy())}
}

// off is the index of the pixel at document point (x, y), which must be inside
func (b *Buffer) off(x, y int) int {
	return 4 * ((y-b.Rect.Min.Y)*b.Rect.Dx() + (x - b.Rect.Min.X))
}

// Clone copies the buffer
func (b *Buffer) Clone() *Buffer {
	return &Buffer{Rect: b.Rect, Pix: append([]float32(nil), b.Pix...)}
}

// crop copies the part of b inside r into a buffer covering r, transparent
// where b does not reach
func (b *Buffer) crop(r image.Rectangle) *Buffer {
	out := NewBuffer(r)
	in := r.Intersect(b.Rect)
	if in.Empty() {
		return out
	}
	n := 4 * in.Dx()
	for y := in.Min.Y; y < in.Max.Y; y++ {
		copy(out.Pix[out.off(in.Min.X, y):out.off(in.Min.X, y)+n], b.Pix[b.off(in.Min.X, y):b.off(in.Min.X, y)+n])
	}
	return out
}

// paste copies src into b where they overlap
func (b *Buffer) paste(src *Buffer) {
	in := src.Rect.Intersect(b.Rect)
	if in.Empty() {
		return
	}
	n := 4 * in.Dx()
	for y := in.Min.Y; y < in.Max.Y; y++ {
		copy(b.Pix[b.off(in.Min.X, y):b.off(in.Min.X, y)+n], src.Pix[src.off(in.Min.X, y):src.off(in.Min.X, y)+n])
	}
}

// Alpha is the buffer's alpha channel over r, zero outside the buffer
func (b *Buffer) Alpha(r image.Rectangle) []float32 {
	out := make([]float32, r.Dx()*r.Dy())
	in := r.Intersect(b.Rect)
	for y := in.Min.Y; y < in.Max.Y; y++ {
		o := b.off(in.Min.X, y)
		i := (y-r.Min.Y)*r.Dx() + (in.Min.X - r.Min.X)
		for x := in.Min.X; x < in.Max.X; x++ {
			out[i] = b.Pix[o+3]
			o += 4
			i++
		}
	}
	return out
}

// FromNRGBA reads img into a buffer covering r. Pixels of r outside img are
// transparent
func FromNRGBA(img *image.NRGBA, r image.Rectangle) *Buffer {
	b := NewBuffer(r)
	in := r.Intersect(img.Rect)
	rows(in, func(y int) {
		s := img.PixOffset(in.Min.X, y)
		d := b.off(in.Min.X, y)
		for x := in.Min.X; x < in.Max.X; x++ {
			a := unit8[img.Pix[s+3]]
			b.Pix[d] = unit8[img.Pix[s]] * a
			b.Pix[d+1] = unit8[img.Pix[s+1]] * a
			b.Pix[d+2] = unit8[img.Pix[s+2]] * a
			b.Pix[d+3] = a
			s += 4
			d += 4
		}
	})
	return b
}

// unit8 maps each 8 bit value to its fraction of 255
var unit8 = func() (t [256]float32) {
	for i := range t {
		t[i] = float32(i) / 255
	}
	return
}()

// NRGBA converts the buffer to 8 bit straight alpha, rounding to the nearest
// value
func (b *Buffer) NRGBA() *image.NRGBA {
	img := image.NewNRGBA(b.Rect)
	rows(b.Rect, func(y int) {
		i := b.off(b.Rect.Min.X, y)
		for x := b.Rect.Min.X; x < b.Rect.Max.X; x++ {
			a := clamp01(b.Pix[i+3])
			if a > 0 {
				img.Pix[i] = to8(b.Pix[i] / a)
				img.Pix[i+1] = to8(b.Pix[i+1] / a)
				img.Pix[i+2] = to8(b.Pix[i+2] / a)
				img.Pix[i+3] = to8(a)
			}
			i += 4
		}
	})
	return img
}

func to8(v float32) uint8 {
	return uint8(math.Round(float64(clamp01(v)) * 255))
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Plane is one value per pixel over Rect, such as a mask's coverage
type Plane struct {
	Rect image.Rectangle
	V    []float32
	// Outside is the value at every point outside Rect
	Outside float32
}

// NewPlane is a plane over r filled with v, which is also its value outside
func NewPlane(r image.Rectangle, v float32) *Plane {
	p := &Plane{Rect: r, V: make([]float32, r.Dx()*r.Dy()), Outside: v}
	if v != 0 {
		for i := range p.V {
			p.V[i] = v
		}
	}
	return p
}

// At is the plane's value at a document point
func (p *Plane) At(x, y int) float32 {
	if !image.Pt(x, y).In(p.Rect) {
		return p.Outside
	}
	return p.V[(y-p.Rect.Min.Y)*p.Rect.Dx()+(x-p.Rect.Min.X)]
}

// row fills out with the plane's values along row y from x0, one per element
func (p *Plane) row(out []float32, x0, y int) {
	if y < p.Rect.Min.Y || y >= p.Rect.Max.Y {
		for i := range out {
			out[i] = p.Outside
		}
		return
	}
	base := (y - p.Rect.Min.Y) * p.Rect.Dx()
	for i := range out {
		x := x0 + i
		if x < p.Rect.Min.X || x >= p.Rect.Max.X {
			out[i] = p.Outside
		} else {
			out[i] = p.V[base+x-p.Rect.Min.X]
		}
	}
}

// mulPlane scales every pixel of b by the plane's value there
func (b *Buffer) mulPlane(p *Plane) {
	if p == nil {
		return
	}
	rows(b.Rect, func(y int) {
		o := b.off(b.Rect.Min.X, y)
		m := make([]float32, b.Rect.Dx())
		p.row(m, b.Rect.Min.X, y)
		for _, v := range m {
			if v != 1 {
				b.Pix[o] *= v
				b.Pix[o+1] *= v
				b.Pix[o+2] *= v
				b.Pix[o+3] *= v
			}
			o += 4
		}
	})
}

// scale multiplies every channel of b by s
func (b *Buffer) scale(s float32) {
	if s == 1 {
		return
	}
	for i := range b.Pix {
		b.Pix[i] *= s
	}
}

// rows calls fn for every row of r, spreading large areas over the CPUs
func rows(r image.Rectangle, fn func(y int)) {
	h := r.Dy()
	if h <= 0 || r.Dx() <= 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers <= 1 || r.Dx()*h < 1<<15 {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			fn(y)
		}
		return
	}
	chunk := max(1, h/(workers*4))
	var wg sync.WaitGroup
	next := make(chan int, (h+chunk-1)/chunk)
	for y := r.Min.Y; y < r.Max.Y; y += chunk {
		next <- y
	}
	close(next)
	for range min(workers, (h+chunk-1)/chunk) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y0 := range next {
				for y := y0; y < min(y0+chunk, r.Max.Y); y++ {
					fn(y)
				}
			}
		}()
	}
	wg.Wait()
}

// grow is r grown by n pixels on every side
func grow(r image.Rectangle, n int) image.Rectangle {
	if r.Empty() {
		return r
	}
	return image.Rect(r.Min.X-n, r.Min.Y-n, r.Max.X+n, r.Max.Y+n)
}

// everywhere stands for a layer that covers the whole plane, such as a fill
var everywhere = image.Rect(-1<<28, -1<<28, 1<<28, 1<<28)
