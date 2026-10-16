package psdrender

import (
	"image"

	"github.com/odevine/impasto/path"
	"github.com/odevine/mimic/engine/psd"
)

// pathTolerance is how far a flattened curve may stray from the true one, in
// pixels
const pathTolerance = 0.05

// userMask is a layer's pixel mask as coverage, its default color outside the
// mask's own rectangle
func userMask(l *psd.Layer) (*Plane, error) {
	m := l.Mask
	img, err := l.MaskImage()
	if err != nil {
		return nil, err
	}
	p := &Plane{Rect: m.Rect, V: make([]float32, m.Rect.Dx()*m.Rect.Dy()), Outside: float32(m.DefaultColor) / 255}
	for y := m.Rect.Min.Y; y < m.Rect.Max.Y; y++ {
		for x := m.Rect.Min.X; x < m.Rect.Max.X; x++ {
			p.V[(y-m.Rect.Min.Y)*m.Rect.Dx()+(x-m.Rect.Min.X)] = float32(img.GrayAt(x, y).Y) / 255
		}
	}
	if m.Density != 0 && m.Density != 255 {
		d := float32(m.Density) / 255
		for i, v := range p.V {
			p.V[i] = 1 - d*(1-v)
		}
		p.Outside = 1 - d*(1-p.Outside)
	}
	if m.Feather > 0 {
		blurPlane(p, featherSigma(m.Feather))
	}
	return p, nil
}

// featherSigma is the Gaussian that a mask's feather radius in pixels blurs by
func featherSigma(feather float64) float32 {
	return float32(feather / 2)
}

// vectorCoverage rasterizes a vector mask over r. Each subpath is filled on
// its own and combined with those before it by its operation, starting from
// empty or, when the mask says so, from full
func vectorCoverage(vm *psd.VectorMask, r image.Rectangle) *Plane {
	w, h := r.Dx(), r.Dy()
	acc := NewPlane(r, 0)
	if vm.InitialFill == 1 {
		for i := range acc.V {
			acc.V[i] = 1
		}
		acc.Outside = 1
	}
	first := true
	for _, sp := range vm.Subpaths {
		if len(sp.Knots) < 2 {
			continue
		}
		p := subpath(sp, r.Min)
		cov := p.Coverage(w, h, path.NonZero, pathTolerance)
		op := sp.Operation
		if op < 0 {
			op = 1
		}
		if first && vm.InitialFill != 1 {
			switch op {
			case 2:
				// A path that starts by subtracting starts from every pixel
				for i := range acc.V {
					acc.V[i] = 1
				}
				acc.Outside = 1
			default:
				op = 1
			}
		}
		first = false
		for i, s := range cov {
			c := acc.V[i]
			switch op {
			case 1:
				acc.V[i] = c + s - c*s
			case 2:
				acc.V[i] = c * (1 - s)
			case 3:
				acc.V[i] = c * s
			case 0:
				acc.V[i] = c + s - 2*c*s
			}
		}
		if op == 3 {
			acc.Outside = 0
		}
	}
	if vm.Inverted {
		for i, v := range acc.V {
			acc.V[i] = 1 - v
		}
		acc.Outside = 1 - acc.Outside
	}
	return acc
}

// subpath builds one subpath as a path in coordinates relative to origin
func subpath(sp psd.Subpath, origin image.Point) *path.Path {
	ox, oy := float64(origin.X), float64(origin.Y)
	pt := func(q psd.Point) (float32, float32) { return float32(q.X - ox), float32(q.Y - oy) }
	p := path.New()
	k := sp.Knots
	x, y := pt(k[0].Anchor)
	p.MoveTo(x, y)
	seg := func(a, b psd.Knot) {
		c1x, c1y := pt(a.Out)
		c2x, c2y := pt(b.In)
		x, y := pt(b.Anchor)
		p.CubicTo(c1x, c1y, c2x, c2y, x, y)
	}
	for i := 1; i < len(k); i++ {
		seg(k[i-1], k[i])
	}
	if sp.Closed {
		seg(k[len(k)-1], k[0])
		p.Close()
	}
	return p
}

// mulPlanes multiplies b into a, both over the same rectangle or with b
// sampled where they differ
func mulPlanes(a, b *Plane) *Plane {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := &Plane{Rect: a.Rect.Union(b.Rect), Outside: a.Outside * b.Outside}
	out.V = make([]float32, out.Rect.Dx()*out.Rect.Dy())
	i := 0
	for y := out.Rect.Min.Y; y < out.Rect.Max.Y; y++ {
		for x := out.Rect.Min.X; x < out.Rect.Max.X; x++ {
			out.V[i] = a.At(x, y) * b.At(x, y)
			i++
		}
	}
	return out
}
