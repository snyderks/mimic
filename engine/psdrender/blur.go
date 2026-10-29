package psdrender

import "math"

// boxSizes are the widths of three box blurs that together approximate a
// Gaussian of the given sigma
func boxSizes(sigma float32) [3]int {
	ideal := math.Sqrt(12*float64(sigma)*float64(sigma)/3 + 1)
	wl := int(math.Floor(ideal))
	if wl%2 == 0 {
		wl--
	}
	wu := wl + 2
	m := int(math.Round((12*float64(sigma)*float64(sigma) - 3*float64(wl*wl) - 12*float64(wl) - 9) / (-4*float64(wl) - 4)))
	var out [3]int
	for i := range out {
		if i < m {
			out[i] = wl
		} else {
			out[i] = wu
		}
	}
	return out
}

// blurValues blurs a w by h grid of values in place with three box blurs
// along each axis, so a pixel's cost does not grow with sigma. Values beyond the
// grid's edge take the given outside value
func blurValues(v []float32, w, h int, sigma, outside float32) {
	if sigma <= 0.05 || w == 0 || h == 0 {
		return
	}
	tmp := make([]float32, len(v))
	for _, size := range boxSizes(sigma) {
		r := (size - 1) / 2
		if r <= 0 {
			continue
		}
		boxPass(v, tmp, w, h, r, outside, true)
		boxPass(tmp, v, w, h, r, outside, false)
	}
}

// boxPass averages each value with its r neighbors on each side along rows
// when horizontal is set, and along columns otherwise
func boxPass(src, dst []float32, w, h, r int, outside float32, horizontal bool) {
	n, lines, step, lineStep := w, h, 1, w
	if !horizontal {
		n, lines, step, lineStep = h, w, w, 1
	}
	norm := 1 / float32(2*r+1)
	at := func(base, i int) float32 {
		if i < 0 || i >= n {
			return outside
		}
		return src[base+i*step]
	}
	for l := 0; l < lines; l++ {
		base := l * lineStep
		var sum float32
		for i := -r; i <= r; i++ {
			sum += at(base, i)
		}
		for i := 0; i < n; i++ {
			dst[base+i*step] = sum * norm
			sum += at(base, i+r+1) - at(base, i-r)
		}
	}
}

// blurPlane blurs a plane in place
func blurPlane(p *Plane, sigma float32) {
	blurValues(p.V, p.Rect.Dx(), p.Rect.Dy(), sigma, p.Outside)
}
