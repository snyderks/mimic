package proxyshop

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// PairFit is what fitting a part's pair images to blends of its single colors
// found
type PairFit struct {
	// Shape, Center, Width and End are the blend that matches best, in the
	// terms a part's blend is written in
	Shape         string
	Center, Width float64
	End           float64
	// Pairs is how many pair images were compared, MeanError the mean of each
	// pixel's largest channel difference from 0 to 255 between a pair image and
	// its blend, and P99 the difference 99 in 100 pixels stay within. Pixels
	// on a cut edge differ where the pair's art was cut a pixel apart, which
	// the percentile looks past
	Pairs     int
	MeanError float64
	P99       float64
	// Drop is true when the blend draws the pairs closely enough to leave the
	// pair images out
	Drop bool
}

// Thresholds for dropping pair images in favor of a blend
const (
	fitMaxMean = 1.5
	fitMaxP99  = 8
)

// flippedPairs are the pairs a frame runs with the second color in WUBRG order
// on the left, as the engine's frame package orders them
var flippedPairs = map[string]bool{"wg": true, "wr": true, "ug": true}

// pairRun is a pair key's colors left to right
func pairRun(key string) (left, right string) {
	if flippedPairs[key] {
		return key[1:], key[:1]
	}
	return key[:1], key[1:]
}

// premul reads a pixel as premultiplied values from 0 to 1
func premul(img *image.NRGBA, x, y int) [4]float64 {
	i := img.PixOffset(x, y)
	a := float64(img.Pix[i+3]) / 255
	return [4]float64{float64(img.Pix[i]) / 255 * a, float64(img.Pix[i+1]) / 255 * a, float64(img.Pix[i+2]) / 255 * a, a}
}

// fitPairs finds the blend of a part's single color images that best draws its
// pair images, which all cover the same rectangle of a canvas docW wide. It
// returns nil when the part has no pair whose two colors it also has
func fitPairs(imgs map[string]*image.NRGBA, docW int) *PairFit {
	type pair struct{ p, l, r *image.NRGBA }
	var pairs []pair
	var keys []string
	for k := range imgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(k) != 2 || !isColorCombo(k) {
			continue
		}
		l, r := pairRun(k)
		if imgs[l] == nil || imgs[r] == nil {
			continue
		}
		pairs = append(pairs, pair{imgs[k], imgs[l], imgs[r]})
	}
	if len(pairs) == 0 {
		return nil
	}
	rect := pairs[0].p.Rect
	// Each column's best weight of the left color, and how much the colors
	// differ there, which is how much the column says about the weight
	n := rect.Dx()
	num := make([]float64, n)
	den := make([]float64, n)
	for _, pr := range pairs {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				p, a, b := premul(pr.p, x, y), premul(pr.l, x, y), premul(pr.r, x, y)
				for c := 0; c < 4; c++ {
					d := a[c] - b[c]
					num[x-rect.Min.X] += (p[c] - b[c]) * d
					den[x-rect.Min.X] += d * d
				}
			}
		}
	}
	var samples []weightSample
	for i := range n {
		if den[i] < 1e-3 {
			continue
		}
		samples = append(samples, weightSample{f: (float64(rect.Min.X+i) + 0.5) / float64(docW), w: num[i] / den[i], k: den[i]})
	}
	if len(samples) == 0 {
		return nil
	}
	fit := fitWeights(samples, docW, float64(rect.Min.X), float64(rect.Max.X))
	best := *fit
	W := float64(docW)

	// The fit is judged on the images the engine would draw
	lo := (W/2 + best.Center - best.Width/2) / W
	hi := lo + best.Width/W
	var total, count float64
	// A histogram of differences in quarter steps finds the percentile
	var hist [1024]float64
	for _, pr := range pairs {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			f := (float64(x) + 0.5) / W
			g := math.Max(0, math.Min(1, (f-lo)/(hi-lo)))
			if best.Shape == "smooth" {
				g = g * g * (3 - 2*g)
			}
			wr := g * best.End
			wl := 1 - wr
			for y := rect.Min.Y; y < rect.Max.Y; y++ {
				p, a, b := premul(pr.p, x, y), premul(pr.l, x, y), premul(pr.r, x, y)
				if p[3] == 0 && a[3] == 0 && b[3] == 0 {
					continue
				}
				var worst float64
				for c := 0; c < 4; c++ {
					worst = math.Max(worst, math.Abs(p[c]-(wl*a[c]+wr*b[c]))*255)
				}
				total += worst
				count++
				hist[min(int(worst*4), len(hist)-1)]++
			}
		}
	}
	best.Pairs = len(pairs)
	if count > 0 {
		best.MeanError = total / count
		var seen float64
		for i, h := range hist {
			seen += h
			if seen >= 0.99*count {
				best.P99 = float64(i) / 4
				break
			}
		}
	}
	best.Drop = best.MeanError <= fitMaxMean && best.P99 <= fitMaxP99
	return &best
}

// blend is the part blend the fit describes, leaving out a seam the engine
// draws by default
func (f *PairFit) blend() Object {
	o := obj("shape", f.Shape)
	if f.End != 1 {
		o = append(o, obj("end", f.End)...)
	}
	if f.Center != 0 || f.Width != 650 {
		o = append(o, obj("seams", obj("2", []Object{obj("center", f.Center, "width", f.Width)}))...)
	}
	return o
}

func (f *PairFit) String() string {
	return fmt.Sprintf("%d pairs, %s seam at %.1f wide %.1f ending at %.3f, mean error %.2f, 99th percentile %.2f", f.Pairs, f.Shape, f.Center, f.Width, f.End, f.MeanError, f.P99)
}

// weightSample is how much of the left color shows at a fraction f of the
// canvas's width, and how much that sample counts
type weightSample struct{ f, w, k float64 }

// fitWeights finds the blend whose left color weight best follows samples
// taken between x0 and x1 on a canvas docW wide
func fitWeights(samples []weightSample, docW int, x0, x1 float64) *PairFit {
	// For a seam from lo to hi, the left weight is 1 - end * ramp, and the best
	// end has a closed form, so the search runs over the seam alone
	W := float64(docW)
	score := func(smooth bool, center, width float64) (float64, float64) {
		lo := (W/2 + center - width/2) / W
		hi := lo + width/W
		var sg, sgg float64
		for _, s := range samples {
			g := math.Max(0, math.Min(1, (s.f-lo)/(hi-lo)))
			if smooth {
				g = g * g * (3 - 2*g)
			}
			sg += s.k * (1 - s.w) * g
			sgg += s.k * g * g
		}
		end := 1.0
		if sgg > 0 {
			end = math.Max(0.5, math.Min(1, sg/sgg))
		}
		var e float64
		for _, s := range samples {
			g := math.Max(0, math.Min(1, (s.f-lo)/(hi-lo)))
			if smooth {
				g = g * g * (3 - 2*g)
			}
			d := s.w - (1 - end*g)
			e += s.k * d * d
		}
		return e, end
	}
	best := PairFit{End: 1, Width: 650}
	bestErr := math.Inf(1)
	span := x1 - x0
	mid := (x0+x1)/2 - W/2
	try := func(smooth bool, c, w float64) {
		if w < 2 {
			return
		}
		e, end := score(smooth, c, w)
		if e < bestErr {
			bestErr = e
			shape := "linear"
			if smooth {
				shape = "smooth"
			}
			best = PairFit{Shape: shape, Center: c, Width: w, End: end}
		}
	}
	for _, smooth := range []bool{true, false} {
		for c := mid - span/2; c <= mid+span/2; c += 8 {
			for w := 16.0; w <= span*1.5; w += 16 {
				try(smooth, c, w)
			}
		}
	}
	for step := 4.0; step >= 0.5; step /= 2 {
		c0, w0, smooth := best.Center, best.Width, best.Shape == "smooth"
		for dc := -4.0; dc <= 4; dc++ {
			for dw := -4.0; dw <= 4; dw++ {
				try(smooth, c0+dc*step, w0+dw*step)
			}
		}
	}
	best.Center = math.Round(best.Center*10) / 10
	best.Width = math.Round(best.Width*10) / 10
	best.End = math.Round(best.End*1000) / 1000
	return &best
}

// MaskBlend fits a blend to a Proxyshop blending mask such as Masks/1/2, whose
// gray ramp is how much of a pair's first color shows over its second. It
// returns nil when the mask does not run from one color to the other
func MaskBlend(mask func(x int) float64, docW int) *PairFit {
	var samples []weightSample
	lo, hi := 1.0, 0.0
	for x := 0; x < docW; x++ {
		v := mask(x)
		lo, hi = math.Min(lo, v), math.Max(hi, v)
		samples = append(samples, weightSample{f: (float64(x) + 0.5) / float64(docW), w: v, k: 1})
	}
	if hi-lo < 0.5 {
		return nil
	}
	fit := fitWeights(samples, docW, 0, float64(docW))
	fit.Drop = true
	return fit
}
