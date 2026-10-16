package psdrender

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/odevine/mimic/engine/psd"
)

// doc is a document of w by h whose top level holds layers, top first
func doc(w, h int, layers ...*psd.Layer) *psd.File {
	f := &psd.File{Width: w, Height: h, Depth: 8, ColorMode: 3}
	f.Root = group("", layers...)
	return f
}

// group is a visible isolated group in normal mode holding layers, top first
func group(name string, layers ...*psd.Layer) *psd.Layer {
	g := &psd.Layer{Name: name, Kind: psd.KindGroup, Visible: true, Opacity: 255, FillOpacity: 255, Blend: psd.BlendNormal, BlendClippedAsGroup: true}
	g.Children = layers
	for _, l := range layers {
		l.Parent = g
	}
	return g
}

// through is a visible pass through group holding layers, top first
func through(name string, layers ...*psd.Layer) *psd.Layer {
	g := group(name, layers...)
	g.Blend = psd.BlendPassThrough
	return g
}

// renderer is a renderer for f that reads the given pixels and masks instead
// of decoding a file
func renderer(f *psd.File, pixels map[*psd.Layer]*image.NRGBA, masks map[*psd.Layer]*Plane) *Renderer {
	r := New(f)
	for l, img := range pixels {
		c := &cached{img: img}
		c.once.Do(func() {})
		r.cache[l] = c
	}
	for l, p := range masks {
		c := &cachedMask{plane: p}
		c.once.Do(func() {})
		r.masks[l] = c
	}
	return r
}

// solid is a visible pixel layer filling rect with c, and its pixels
func solid(name string, rect image.Rectangle, c color.NRGBA) (*psd.Layer, *image.NRGBA) {
	l := &psd.Layer{Name: name, Kind: psd.KindPixel, Rect: rect, Visible: true, Opacity: 255, FillOpacity: 255, Blend: psd.BlendNormal, BlendClippedAsGroup: true, TransparencyShapesLayer: true}
	img := image.NewNRGBA(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return l, img
}

// fillLayer is a visible solid color fill layer
func fillLayer(name string, c color.NRGBA) *psd.Layer {
	return &psd.Layer{Name: name, Kind: psd.KindSolidFill, Visible: true, Opacity: 255, FillOpacity: 255, Blend: psd.BlendNormal, BlendClippedAsGroup: true,
		Fill: &psd.Fill{Type: "solid", Color: psdColor(c)}}
}

func psdColor(c color.NRGBA) psd.Color {
	return psd.Color{R: float64(c.R), G: float64(c.G), B: float64(c.B), Model: "rgb"}
}

// at is the 8 bit straight color of a buffer at a point
func at(b *Buffer, x, y int) color.NRGBA {
	return b.NRGBA().NRGBAAt(x, y)
}

// near fails unless two colors differ by at most tol in every channel
func near(t *testing.T, what string, got, want color.NRGBA, tol int) {
	t.Helper()
	d := func(a, b uint8) int { return int(math.Abs(float64(int(a) - int(b)))) }
	if d(got.R, want.R) > tol || d(got.G, want.G) > tol || d(got.B, want.B) > tol || d(got.A, want.A) > tol {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func rgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{r, g, b, a} }

// rectPath is a closed rectangular subpath
func rectPath(r image.Rectangle, op int) psd.Subpath {
	k := func(x, y int) psd.Knot {
		p := psd.Point{X: float64(x), Y: float64(y)}
		return psd.Knot{Anchor: p, In: p, Out: p}
	}
	return psd.Subpath{Closed: true, Operation: op, Knots: []psd.Knot{k(r.Min.X, r.Min.Y), k(r.Max.X, r.Min.Y), k(r.Max.X, r.Max.Y), k(r.Min.X, r.Max.Y)}}
}
