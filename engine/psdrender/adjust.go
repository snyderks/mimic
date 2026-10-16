package psdrender

import (
	"math"
	"sort"

	"github.com/odevine/mimic/engine/psd"
)

// adjust applies an adjustment layer to what is below it in dst, blended by
// the layer's mode and mixed in by opacity and the layer's masks
func (p *pass) adjust(l *psd.Layer, dst *Buffer, mode psd.BlendMode, opacity float32, mask *Plane) {
	a := l.Adjustment
	if a == nil || opacity <= 0 {
		return
	}
	f := p.adjustment(a)
	if f == nil {
		p.r.note("%s adjustments are not drawn", a.Type)
		return
	}
	r := dst.Rect
	if mask != nil && mask.Outside == 0 {
		r = r.Intersect(mask.Rect)
	}
	sep := separable(mode)
	plain := mode == psd.BlendNormal || mode == psd.BlendPassThrough || mode == psd.BlendDissolve
	rows(r, func(y int) {
		o := dst.off(r.Min.X, y)
		var m []float32
		if mask != nil {
			m = make([]float32, r.Dx())
			mask.row(m, r.Min.X, y)
		}
		for i := 0; i < r.Dx(); i++ {
			al := dst.Pix[o+3]
			k := opacity
			if m != nil {
				k *= m[i]
			}
			if al > 0 && k > 0 {
				c := rgb{dst.Pix[o] / al, dst.Pix[o+1] / al, dst.Pix[o+2] / al}
				n := f(c)
				for j := range n {
					n[j] = clamp01(n[j])
				}
				switch {
				case plain:
				case sep != nil:
					for j := range n {
						n[j] = clamp01(sep(c[j], n[j]))
					}
				default:
					n = nonSeparable(mode, c, n)
				}
				for j := 0; j < 3; j++ {
					dst.Pix[o+j] = (c[j] + (n[j]-c[j])*k) * al
				}
			}
			o += 4
		}
	})
}

// adjustment returns the color function of an adjustment, nil for a kind this
// package does not draw
func (p *pass) adjustment(a *psd.Adjustment) func(rgb) rgb {
	switch a.Type {
	case "levels":
		return levelsFunc(a.Levels)
	case "curves":
		return curvesFunc(a.Curves)
	case "huesaturation":
		if a.HueSaturation != nil {
			return hueSatFunc(a.HueSaturation)
		}
	case "brightnesscontrast":
		if a.BrightnessContrast != nil {
			return brightnessContrastFunc(a.BrightnessContrast)
		}
	case "selectivecolor":
		if a.SelectiveColor != nil {
			return selectiveColorFunc(a.SelectiveColor)
		}
	case "invert":
		return func(c rgb) rgb { return rgb{1 - c[0], 1 - c[1], 1 - c[2]} }
	case "threshold":
		t := float32(a.Threshold) / 255
		return func(c rgb) rgb {
			v := float32(0)
			if lum(c) >= t {
				v = 1
			}
			return rgb{v, v, v}
		}
	case "posterize":
		n := float32(max(a.Posterize, 2) - 1)
		return func(c rgb) rgb {
			for i := range c {
				c[i] = float32(math.Round(float64(c[i]*n))) / n
			}
			return c
		}
	}
	return nil
}

// lut is a table of 256 output values for the 256 input values of a channel
type lut [256]float32

func (t *lut) apply(v float32) float32 {
	f := clamp01(v) * 255
	i := int(f)
	if i >= 255 {
		return t[255]
	}
	u := f - float32(i)
	return t[i] + (t[i+1]-t[i])*u
}

func identityLUT() *lut {
	var t lut
	for i := range t {
		t[i] = float32(i) / 255
	}
	return &t
}

func levelsLUT(r psd.LevelsRecord) *lut {
	var t lut
	lo, hi := float64(r.InputFloor), float64(r.InputCeiling)
	if hi <= lo {
		hi = lo + 1
	}
	g := r.Gamma
	if g <= 0 {
		g = 1
	}
	for i := range t {
		u := math.Min(math.Max((float64(i)-lo)/(hi-lo), 0), 1)
		u = math.Pow(u, 1/g)
		t[i] = float32((float64(r.OutputFloor) + u*float64(r.OutputCeiling-r.OutputFloor)) / 255)
	}
	return &t
}

func levelsFunc(recs []psd.LevelsRecord) func(rgb) rgb {
	if len(recs) == 0 {
		return func(c rgb) rgb { return c }
	}
	master := levelsLUT(recs[0])
	chans := [3]*lut{identityLUT(), identityLUT(), identityLUT()}
	for i := 1; i <= 3 && i < len(recs); i++ {
		chans[i-1] = levelsLUT(recs[i])
	}
	return func(c rgb) rgb {
		for i := range c {
			c[i] = master.apply(chans[i].apply(c[i]))
		}
		return c
	}
}

// curveLUT interpolates a curve's points with a natural cubic spline
func curveLUT(pts []psd.Point) *lut {
	if len(pts) < 2 {
		return identityLUT()
	}
	ps := append([]psd.Point(nil), pts...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].X < ps[j].X })
	n := len(ps)
	xs, ys := make([]float64, n), make([]float64, n)
	for i, p := range ps {
		xs[i], ys[i] = p.X, p.Y
	}
	// Second derivatives of the natural spline
	y2 := make([]float64, n)
	u := make([]float64, n)
	for i := 1; i < n-1; i++ {
		sig := (xs[i] - xs[i-1]) / (xs[i+1] - xs[i-1])
		q := sig*y2[i-1] + 2
		y2[i] = (sig - 1) / q
		d := (ys[i+1]-ys[i])/(xs[i+1]-xs[i]) - (ys[i]-ys[i-1])/(xs[i]-xs[i-1])
		u[i] = (6*d/(xs[i+1]-xs[i-1]) - sig*u[i-1]) / q
	}
	for k := n - 2; k >= 0; k-- {
		y2[k] = y2[k]*y2[k+1] + u[k]
	}
	var t lut
	for i := range t {
		x := float64(i)
		var y float64
		switch {
		case x <= xs[0]:
			y = ys[0]
		case x >= xs[n-1]:
			y = ys[n-1]
		default:
			k := sort.SearchFloat64s(xs, x)
			if k == 0 {
				k = 1
			}
			lo, hi := k-1, k
			h := xs[hi] - xs[lo]
			a := (xs[hi] - x) / h
			b := (x - xs[lo]) / h
			y = a*ys[lo] + b*ys[hi] + ((a*a*a-a)*y2[lo]+(b*b*b-b)*y2[hi])*h*h/6
		}
		t[i] = float32(math.Min(math.Max(y, 0), 255) / 255)
	}
	return &t
}

func curvesFunc(chs []psd.CurvesChannel) func(rgb) rgb {
	master := identityLUT()
	chans := [3]*lut{identityLUT(), identityLUT(), identityLUT()}
	for _, ch := range chs {
		switch {
		case ch.Channel == 0:
			master = curveLUT(ch.Points)
		case ch.Channel >= 1 && ch.Channel <= 3:
			chans[ch.Channel-1] = curveLUT(ch.Points)
		}
	}
	return func(c rgb) rgb {
		for i := range c {
			c[i] = chans[i].apply(master.apply(c[i]))
		}
		return c
	}
}

func rgbToHSL(c rgb) (h, s, l float32) {
	mx, mn := max(c[0], c[1], c[2]), min(c[0], c[1], c[2])
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case c[0]:
		h = (c[1] - c[2]) / d
		if c[1] < c[2] {
			h += 6
		}
	case c[1]:
		h = (c[2]-c[0])/d + 2
	default:
		h = (c[0]-c[1])/d + 4
	}
	return h * 60, s, l
}

func hslToRGB(h, s, l float32) rgb {
	if s == 0 {
		return rgb{l, l, l}
	}
	var q float32
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360
	f := func(t float32) float32 {
		t -= float32(math.Floor(float64(t)))
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return rgb{f(hk + 1.0/3), f(hk), f(hk - 1.0/3)}
}

func hueSatFunc(hs *psd.HueSaturation) func(rgb) rgb {
	return func(c rgb) rgb {
		h, s, l := rgbToHSL(c)
		if hs.Colorize {
			h = float32(hs.ColorizeHue)
			s = float32(hs.ColorizeSaturation) / 100
		} else {
			h += float32(hs.Hue)
			sv := float32(hs.Saturation) / 100
			if sv > 0 {
				s += (1 - s) * sv
			} else {
				s *= 1 + sv
			}
		}
		out := hslToRGB(h, clamp01(s), l)
		lv := float32(hs.Lightness) / 100
		if hs.Colorize {
			lv = float32(hs.ColorizeLightness) / 100
		}
		for i := range out {
			if lv > 0 {
				out[i] += (1 - out[i]) * lv
			} else {
				out[i] *= 1 + lv
			}
		}
		return out
	}
}

func brightnessContrastFunc(bc *psd.BrightnessContrast) func(rgb) rgb {
	b := float32(bc.Brightness) / 255
	k := float32(bc.Contrast) / 100
	if !bc.Legacy {
		b = float32(bc.Brightness) / 150 * 0.5
	}
	slope := float32(1)
	if k > 0 {
		slope = 1 / max(1-k*0.99, 0.01)
	} else {
		slope = 1 + k
	}
	return func(c rgb) rgb {
		for i := range c {
			v := c[i]
			if bc.Legacy {
				v += b
			} else if b > 0 {
				v += (1 - v) * b * 1.2 * (1 - v)
			} else {
				v += v * b * 1.2 * v
			}
			c[i] = (v-0.5)*slope + 0.5
		}
		return c
	}
}

// selectiveColorFunc follows how Photoshop weighs each color range by how
// strongly a pixel belongs to it, then moves its cyan, magenta and yellow and
// its black by that range's settings
func selectiveColorFunc(sc *psd.SelectiveColor) func(rgb) rgb {
	return func(c rgb) rgb {
		mx, mn := max(c[0], c[1], c[2]), min(c[0], c[1], c[2])
		mid := c[0] + c[1] + c[2] - mx - mn
		var w [9]float32
		// The primary range is the largest channel, the secondary the color
		// the two largest make
		imax, imin := 0, 0
		for i := 1; i < 3; i++ {
			if c[i] > c[imax] {
				imax = i
			}
			if c[i] < c[imin] {
				imin = i
			}
		}
		if mx > mn {
			primary := [3]int{0, 2, 4}[imax]
			w[primary] = mx - mid
			// cyans 3, magentas 5, yellows 1 are each missing one channel
			secondary := [3]int{3, 5, 1}[imin]
			w[secondary] = mid - mn
		}
		if mn > 0.5 {
			w[6] = (mn - 0.5) * 2
		}
		if mx < 0.5 {
			w[8] = (0.5 - mx) * 2
		}
		w[7] = max(0, 1-(float32(math.Abs(float64(mx-0.5)))+float32(math.Abs(float64(mn-0.5)))))
		out := c
		for ch := 0; ch < 3; ch++ {
			cmy := 1 - c[ch]
			var delta float32
			for r := 0; r < 9; r++ {
				if w[r] == 0 {
					continue
				}
				a := float32(sc.Colors[r][ch]) / 100
				k := float32(sc.Colors[r][3]) / 100
				d := a + k + a*k
				if !sc.Absolute {
					d *= cmy
				}
				delta += w[r] * d
			}
			out[ch] = clamp01(1 - clamp01(cmy+delta))
		}
		return out
	}
}
