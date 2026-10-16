package psdrender

import (
	"math"

	"github.com/odevine/mimic/engine/psd"
)

// blendOp is how one buffer is laid over another
type blendOp struct {
	mode psd.BlendMode
	// opacity scales the source's coverage
	opacity float32
	// fill is the layer's fill opacity, which for most modes scales coverage
	// like opacity but for the special eight weakens the source's color
	// toward the mode's neutral color instead
	fill float32
	// mask scales the source's coverage at each pixel when it is not nil
	mask *Plane
	// gamma mixes the result into the backdrop in the space Photoshop uses
	// for text, gamma 1.45 over linear light
	gamma bool
}

// composite lays src over dst with a blend mode and an opacity, where they
// overlap. A non nil mask scales src's coverage at each pixel. The result has
// Photoshop's alpha, as + ab(1 - as), and its color mixes the blend of the two
// where both are present with each alone where only it is
func composite(dst, src *Buffer, mode psd.BlendMode, opacity float32, mask *Plane) {
	compositeOp(dst, src, blendOp{mode: mode, opacity: opacity, fill: 1, mask: mask})
}

func compositeOp(dst, src *Buffer, op blendOp) {
	if op.fill == 0 && !specialFill(op.mode) || op.opacity <= 0 {
		return
	}
	r := dst.Rect.Intersect(src.Rect)
	if op.mask != nil && op.mask.Outside == 0 {
		r = r.Intersect(op.mask.Rect)
	}
	if r.Empty() {
		return
	}
	special := specialFill(op.mode) && op.fill < 1
	k0 := op.opacity
	if !special {
		k0 *= op.fill
	}
	f := separable(op.mode)
	simple := !op.gamma && !special && (op.mode == psd.BlendNormal || op.mode == psd.BlendDissolve || op.mode == psd.BlendPassThrough)
	rows(r, func(y int) {
		d := dst.off(r.Min.X, y)
		s := src.off(r.Min.X, y)
		var m []float32
		if op.mask != nil {
			m = make([]float32, r.Dx())
			op.mask.row(m, r.Min.X, y)
		}
		for i := 0; i < r.Dx(); i++ {
			k := k0
			if m != nil {
				k *= m[i]
			}
			as := src.Pix[s+3]
			if t := as * k; t > 0 {
				if simple {
					inv := 1 - t
					dst.Pix[d] = src.Pix[s]*k + dst.Pix[d]*inv
					dst.Pix[d+1] = src.Pix[s+1]*k + dst.Pix[d+1]*inv
					dst.Pix[d+2] = src.Pix[s+2]*k + dst.Pix[d+2]*inv
					dst.Pix[d+3] = t + dst.Pix[d+3]*inv
				} else {
					blendPixel(dst.Pix[d:d+4], src.Pix[s:s+4], t, op, special, f)
				}
			}
			d += 4
			s += 4
		}
	})
}

// specialFill reports whether fill opacity weakens a mode's source color
// rather than its coverage, which Photoshop does for eight modes
func specialFill(m psd.BlendMode) bool {
	switch m {
	case psd.BlendColorBurn, psd.BlendLinearBurn, psd.BlendColorDodge, psd.BlendLinearDodge,
		psd.BlendVividLight, psd.BlendLinearLight, psd.BlendHardMix, psd.BlendDifference:
		return true
	}
	return false
}

// weaken moves a source channel toward the color that leaves the backdrop
// unchanged in mode m, as fill opacity does in the special eight modes
func weaken(m psd.BlendMode, cs, fill float32) float32 {
	switch m {
	case psd.BlendColorBurn, psd.BlendLinearBurn:
		return 1 - (1-cs)*fill
	case psd.BlendVividLight, psd.BlendLinearLight:
		return 0.5 + (cs-0.5)*fill
	}
	return cs * fill
}

// blendPixel moves the premultiplied pixel dp toward the result of blending
// the source color over it at full strength, by t
func blendPixel(dp, sp []float32, t float32, op blendOp, special bool, f func(cb, cs float32) float32) {
	ab := dp[3]
	sa := sp[3]
	cs := [3]float32{sp[0] / sa, sp[1] / sa, sp[2] / sa}
	var cb [3]float32
	if ab > 0 {
		cb = [3]float32{dp[0] / ab, dp[1] / ab, dp[2] / ab}
	}
	var bl [3]float32
	srcAlpha := float32(1)
	switch {
	case ab == 0:
	case special:
		for c := 0; c < 3; c++ {
			if op.mode == psd.BlendHardMix {
				bl[c] = hardMixFill(cb[c], cs[c], op.fill)
			} else {
				bl[c] = clamp01(f(cb[c], weaken(op.mode, cs[c], op.fill)))
			}
		}
		srcAlpha = op.fill
	case f != nil:
		for c := 0; c < 3; c++ {
			bl[c] = clamp01(f(cb[c], cs[c]))
		}
	default:
		bl = nonSeparable(op.mode, cb, cs)
	}
	ao := t*((1-ab)*srcAlpha+ab) + (1-t)*ab
	if op.gamma {
		var out [3]float32
		for c := 0; c < 3; c++ {
			v := t*((1-ab)*srcAlpha*toText(cs[c])+ab*toText(bl[c])) + (1-t)*ab*toText(cb[c])
			if ao > 0 {
				out[c] = fromText(v/ao) * ao
			}
		}
		dp[0], dp[1], dp[2] = out[0], out[1], out[2]
		dp[3] = ao
		return
	}
	for c := 0; c < 3; c++ {
		dp[c] = t*((1-ab)*srcAlpha*cs[c]+ab*bl[c]) + (1-t)*dp[c]
	}
	dp[3] = ao
}

// hardMixFill is hard mix at a fill below full, which Photoshop draws as a
// steep linear ramp that becomes a threshold as fill reaches full
func hardMixFill(cb, cs, fill float32) float32 {
	if fill >= 1 {
		if cb+cs >= 1 {
			return 1
		}
		return 0
	}
	return clamp01((cb - (1-cs)*fill) / (1 - fill))
}

// textGamma is the gamma Photoshop blends text in by default
const textGamma = 1.45

var textLUT, textInvLUT = func() (to, from [4097]float32) {
	for i := range to {
		v := float64(i) / 4096
		lin := v / 12.92
		if v > 0.04045 {
			lin = math.Pow((v+0.055)/1.055, 2.4)
		}
		to[i] = float32(math.Pow(lin, 1/textGamma))
		g := math.Pow(v, textGamma)
		enc := g * 12.92
		if g > 0.0031308 {
			enc = 1.055*math.Pow(g, 1/2.4) - 0.055
		}
		from[i] = float32(enc)
	}
	return
}()

func lookup(t *[4097]float32, v float32) float32 {
	f := clamp01(v) * 4096
	i := int(f)
	if i >= 4096 {
		return t[4096]
	}
	u := f - float32(i)
	return t[i] + (t[i+1]-t[i])*u
}

// toText and fromText move a channel into and out of the text blending space
func toText(v float32) float32   { return lookup(&textLUT, v) }
func fromText(v float32) float32 { return lookup(&textInvLUT, v) }

// separable returns the per channel blend function of a mode, nil for the
// modes that look at all three channels together
func separable(mode psd.BlendMode) func(cb, cs float32) float32 {
	switch mode {
	case psd.BlendNormal, psd.BlendDissolve, psd.BlendPassThrough:
		return func(cb, cs float32) float32 { return cs }
	case psd.BlendMultiply:
		return func(cb, cs float32) float32 { return cb * cs }
	case psd.BlendScreen:
		return screen
	case psd.BlendOverlay:
		return func(cb, cs float32) float32 { return hardLight(cs, cb) }
	case psd.BlendHardLight:
		return hardLight
	case psd.BlendSoftLight:
		return softLight
	case psd.BlendDarken:
		return func(cb, cs float32) float32 { return min(cb, cs) }
	case psd.BlendLighten:
		return func(cb, cs float32) float32 { return max(cb, cs) }
	case psd.BlendColorDodge:
		return colorDodge
	case psd.BlendColorBurn:
		return colorBurn
	case psd.BlendLinearDodge:
		return func(cb, cs float32) float32 { return min(cb+cs, 1) }
	case psd.BlendLinearBurn:
		return func(cb, cs float32) float32 { return max(cb+cs-1, 0) }
	case psd.BlendLinearLight:
		return func(cb, cs float32) float32 { return clamp01(cb + 2*cs - 1) }
	case psd.BlendVividLight:
		return func(cb, cs float32) float32 {
			if cs <= 0.5 {
				return colorBurn(cb, 2*cs)
			}
			return colorDodge(cb, 2*cs-1)
		}
	case psd.BlendPinLight:
		return func(cb, cs float32) float32 {
			if cs <= 0.5 {
				return min(cb, 2*cs)
			}
			return max(cb, 2*cs-1)
		}
	case psd.BlendHardMix:
		return func(cb, cs float32) float32 {
			if cb+cs >= 1 {
				return 1
			}
			return 0
		}
	case psd.BlendDifference:
		return func(cb, cs float32) float32 { return float32(math.Abs(float64(cb - cs))) }
	case psd.BlendExclusion:
		return func(cb, cs float32) float32 { return cb + cs - 2*cb*cs }
	case psd.BlendSubtract:
		return func(cb, cs float32) float32 { return max(cb-cs, 0) }
	case psd.BlendDivide:
		return func(cb, cs float32) float32 {
			if cs <= 0 {
				if cb <= 0 {
					return 0
				}
				return 1
			}
			return min(cb/cs, 1)
		}
	}
	return nil
}

func screen(cb, cs float32) float32 { return cb + cs - cb*cs }

func hardLight(cb, cs float32) float32 {
	if cs <= 0.5 {
		return cb * 2 * cs
	}
	return screen(cb, 2*cs-1)
}

// softLight is Photoshop's soft light curve, which differs from the W3C one
func softLight(cb, cs float32) float32 {
	if cs <= 0.5 {
		return 2*cb*cs + cb*cb*(1-2*cs)
	}
	return 2*cb*(1-cs) + float32(math.Sqrt(float64(cb)))*(2*cs-1)
}

func colorDodge(cb, cs float32) float32 {
	if cb <= 0 {
		return 0
	}
	if cs >= 1 {
		return 1
	}
	return min(1, cb/(1-cs))
}

func colorBurn(cb, cs float32) float32 {
	if cb >= 1 {
		return 1
	}
	if cs <= 0 {
		return 0
	}
	return 1 - min(1, (1-cb)/cs)
}

func lum(c [3]float32) float32 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }

func clipColor(c [3]float32) [3]float32 {
	l := lum(c)
	n := min(c[0], c[1], c[2])
	x := max(c[0], c[1], c[2])
	if n < 0 {
		for i := range c {
			c[i] = l + (c[i]-l)*l/(l-n)
		}
	}
	if x > 1 {
		for i := range c {
			c[i] = l + (c[i]-l)*(1-l)/(x-l)
		}
	}
	return c
}

func setLum(c [3]float32, l float32) [3]float32 {
	d := l - lum(c)
	return clipColor([3]float32{c[0] + d, c[1] + d, c[2] + d})
}

func sat(c [3]float32) float32 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }

func setSat(c [3]float32, s float32) [3]float32 {
	imax, imin := 0, 0
	for i := 1; i < 3; i++ {
		if c[i] > c[imax] {
			imax = i
		}
		if c[i] < c[imin] {
			imin = i
		}
	}
	if imax == imin {
		return [3]float32{}
	}
	imid := 3 - imax - imin
	var out [3]float32
	out[imid] = (c[imid] - c[imin]) * s / (c[imax] - c[imin])
	out[imax] = s
	return out
}

func nonSeparable(mode psd.BlendMode, cb, cs [3]float32) [3]float32 {
	switch mode {
	case psd.BlendHue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case psd.BlendSaturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case psd.BlendColor:
		return setLum(cs, lum(cb))
	case psd.BlendLuminosity:
		return setLum(cb, lum(cs))
	case psd.BlendDarkerColor:
		if lum(cs) < lum(cb) {
			return cs
		}
		return cb
	case psd.BlendLighterColor:
		if lum(cs) > lum(cb) {
			return cs
		}
		return cb
	}
	return cs
}

// lerpInto moves dst toward its present value from before by k times the
// plane's value at each pixel, inside before's rectangle. It is how a pass
// through group's opacity and mask apply to what its layers did to the
// backdrop, and how a layer's shape cuts what its stack drew
func lerpInto(dst, before *Buffer, k float32, mask *Plane) {
	r := dst.Rect.Intersect(before.Rect)
	rows(r, func(y int) {
		d := dst.off(r.Min.X, y)
		b := before.off(r.Min.X, y)
		var m []float32
		if mask != nil {
			m = make([]float32, r.Dx())
			mask.row(m, r.Min.X, y)
		}
		for i := 0; i < r.Dx(); i++ {
			t := k
			if m != nil {
				t *= m[i]
			}
			if t < 1 {
				for c := 0; c < 4; c++ {
					dst.Pix[d+c] = before.Pix[b+c] + (dst.Pix[d+c]-before.Pix[b+c])*t
				}
			}
			d += 4
			b += 4
		}
	})
}
