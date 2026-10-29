package psdrender

import (
	"image"
	"math"
	"sort"

	"github.com/odevine/mimic/engine/psd"
)

// rgb is a color with channels from 0 to 1
type rgb [3]float32

func colorOf(c psd.Color) rgb {
	return rgb{float32(c.R / 255), float32(c.G / 255), float32(c.B / 255)}
}

// fillBuffer paints a fill over region, opaque except where a gradient's own
// transparency says otherwise. A gradient aligned with the layer spans the box
// of l's outline, or of the document when it has none
func (p *pass) fillBuffer(f *psd.Fill, region image.Rectangle, l *psd.Layer) *Buffer {
	buf := NewBuffer(region)
	switch {
	case f == nil:
		return buf
	case f.Type == "gradient" && f.Gradient != nil:
		box := image.Rect(0, 0, p.r.f.Width, p.r.f.Height)
		if f.Gradient.Align && l != nil {
			if l.VectorMask != nil {
				if b := pathBounds(l.VectorMask); !b.Empty() {
					box = grow(b, -1)
				}
			} else if l.Kind == psd.KindShape && !l.Rect.Empty() {
				box = l.Rect
			}
		}
		paintGradient(buf, f.Gradient, box, nil)
	case f.Type == "pattern":
		p.r.note("pattern fills are drawn as mid gray")
		fillColor(buf, rgb{0.5, 0.5, 0.5})
	default:
		fillColor(buf, colorOf(f.Color))
	}
	return buf
}

func fillColor(buf *Buffer, c rgb) {
	for i := 0; i < len(buf.Pix); i += 4 {
		buf.Pix[i], buf.Pix[i+1], buf.Pix[i+2], buf.Pix[i+3] = c[0], c[1], c[2], 1
	}
}

// ramp is a gradient's colors and opacity sampled finely from 0 to 1
type ramp struct {
	c [][4]float32
}

const rampSteps = 4096

func newRamp(g *psd.Gradient, reverse bool) *ramp {
	r := &ramp{c: make([][4]float32, rampSteps+1)}
	colors := append([]psd.ColorStop(nil), g.Stops...)
	sort.SliceStable(colors, func(i, j int) bool { return colors[i].Location < colors[j].Location })
	alphas := append([]psd.OpacityStop(nil), g.Transparency...)
	sort.SliceStable(alphas, func(i, j int) bool { return alphas[i].Location < alphas[j].Location })
	for i := 0; i <= rampSteps; i++ {
		t := float64(i) / rampSteps
		if reverse {
			t = 1 - t
		}
		pos := t * 4096
		c := sampleColors(colors, pos)
		a := sampleAlphas(alphas, pos)
		r.c[i] = [4]float32{c[0], c[1], c[2], a}
	}
	return r
}

func (r *ramp) at(t float32) [4]float32 {
	t = clamp01(t)
	return r.c[int(t*rampSteps+0.5)]
}

// between is how far pos is from a stop at lo to one at hi, bent so the
// halfway color sits at the midpoint, which is a percent of the way
func between(pos float64, lo, hi int, mid int) float64 {
	if hi <= lo {
		return 1
	}
	u := (pos - float64(lo)) / float64(hi-lo)
	m := float64(mid) / 100
	if m <= 0 || m >= 1 || m == 0.5 {
		return u
	}
	if u < m {
		return 0.5 * u / m
	}
	return 0.5 + 0.5*(u-m)/(1-m)
}

func stopColor(s psd.ColorStop) rgb {
	switch s.Type {
	case "foreground":
		return rgb{0, 0, 0}
	case "background":
		return rgb{1, 1, 1}
	}
	return colorOf(s.Color)
}

func sampleColors(stops []psd.ColorStop, pos float64) rgb {
	if len(stops) == 0 {
		return rgb{}
	}
	if pos <= float64(stops[0].Location) {
		return stopColor(stops[0])
	}
	for i := 1; i < len(stops); i++ {
		if pos <= float64(stops[i].Location) {
			u := float32(between(pos, stops[i-1].Location, stops[i].Location, stops[i].Midpoint))
			a, b := stopColor(stops[i-1]), stopColor(stops[i])
			return rgb{a[0] + (b[0]-a[0])*u, a[1] + (b[1]-a[1])*u, a[2] + (b[2]-a[2])*u}
		}
	}
	return stopColor(stops[len(stops)-1])
}

func sampleAlphas(stops []psd.OpacityStop, pos float64) float32 {
	if len(stops) == 0 {
		return 1
	}
	if pos <= float64(stops[0].Location) {
		return float32(stops[0].Opacity)
	}
	for i := 1; i < len(stops); i++ {
		if pos <= float64(stops[i].Location) {
			u := float32(between(pos, stops[i-1].Location, stops[i].Location, stops[i].Midpoint))
			a, b := float32(stops[i-1].Opacity), float32(stops[i].Opacity)
			return a + (b-a)*u
		}
	}
	return float32(stops[len(stops)-1].Opacity)
}

// paintGradient paints a placed gradient over buf, spanning box. A non nil
// cover scales each pixel's coverage
func paintGradient(buf *Buffer, gs *psd.GradientSettings, box image.Rectangle, cover *Plane) {
	if gs.Gradient == nil {
		return
	}
	rp := newRamp(gs.Gradient, gs.Reverse)
	scale := gs.Scale / 100
	if scale <= 0 {
		scale = 1
	}
	w, h := float64(box.Dx()), float64(box.Dy())
	cx := float64(box.Min.X) + w/2 + gs.Offset.X/100*w
	cy := float64(box.Min.Y) + h/2 + gs.Offset.Y/100*h
	th := gs.Angle * math.Pi / 180
	dx, dy := math.Cos(th), -math.Sin(th)
	length := (math.Abs(w*dx) + math.Abs(h*dy)) * scale
	if length <= 0 {
		length = 1
	}
	half := length / 2
	for y := buf.Rect.Min.Y; y < buf.Rect.Max.Y; y++ {
		o := buf.off(buf.Rect.Min.X, y)
		for x := buf.Rect.Min.X; x < buf.Rect.Max.X; x++ {
			px, py := float64(x)+0.5-cx, float64(y)+0.5-cy
			along := px*dx + py*dy
			across := -px*dy + py*dx
			var t float64
			switch gs.Style {
			case "radial":
				t = math.Hypot(along, across) / half
			case "angle":
				a := math.Atan2(-across, along)
				if a < 0 {
					a += 2 * math.Pi
				}
				t = a / (2 * math.Pi)
			case "reflected":
				t = math.Abs(along) / half
			case "diamond":
				t = (math.Abs(along) + math.Abs(across)) / half
			default:
				t = along/length + 0.5
			}
			c := rp.at(float32(t))
			a := c[3]
			if cover != nil {
				a *= cover.At(x, y)
			}
			buf.Pix[o], buf.Pix[o+1], buf.Pix[o+2], buf.Pix[o+3] = c[0]*a, c[1]*a, c[2]*a, a
			o += 4
		}
	}
}
