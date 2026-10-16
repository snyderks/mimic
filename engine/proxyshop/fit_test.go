package proxyshop

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func solid(r image.Rectangle, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// blended draws a pair the way the engine blends a part's two colors
func blended(left, right *image.NRGBA, docW int, smooth bool, center, width, end float64) *image.NRGBA {
	r := left.Rect
	out := image.NewNRGBA(r)
	W := float64(docW)
	lo := (W/2 + center - width/2) / W
	hi := lo + width/W
	for x := r.Min.X; x < r.Max.X; x++ {
		g := math.Max(0, math.Min(1, ((float64(x)+0.5)/W-lo)/(hi-lo)))
		if smooth {
			g = g * g * (3 - 2*g)
		}
		wr := g * end
		for y := r.Min.Y; y < r.Max.Y; y++ {
			a, b := premul(left, x, y), premul(right, x, y)
			var m [4]float64
			for c := range m {
				m[c] = (1-wr)*a[c] + wr*b[c]
			}
			px := color.NRGBA{A: uint8(math.Round(m[3] * 255))}
			if m[3] > 0 {
				px.R = uint8(math.Round(m[0] / m[3] * 255))
				px.G = uint8(math.Round(m[1] / m[3] * 255))
				px.B = uint8(math.Round(m[2] / m[3] * 255))
			}
			out.SetNRGBA(x, y, px)
		}
	}
	return out
}

func TestFitPairsRecoversABlend(t *testing.T) {
	r := image.Rect(20, 0, 380, 6)
	w := solid(r, color.NRGBA{240, 240, 230, 255})
	u := solid(r, color.NRGBA{0, 110, 180, 255})
	g := solid(r, color.NRGBA{0, 120, 60, 255})
	imgs := map[string]*image.NRGBA{
		"w": w, "u": u, "g": g,
		"wu": blended(w, u, 400, true, 12, 90, 0.94),
		// A green and white frame runs green on the left
		"wg": blended(g, w, 400, true, 12, 90, 0.94),
	}
	fit := fitPairs(imgs, 400)
	if fit == nil {
		t.Fatal("no fit")
	}
	if fit.Shape != "smooth" || math.Abs(fit.Center-12) > 1 || math.Abs(fit.Width-90) > 2 || math.Abs(fit.End-0.94) > 0.01 {
		t.Errorf("fit %v, want smooth at 12 wide 90 ending at 0.94", fit)
	}
	if !fit.Drop || fit.Pairs != 2 {
		t.Errorf("fit %v should drop both pairs", fit)
	}
	if b := string(mustRaw(fit.blend())); b == "" {
		t.Error("no blend")
	}
}

func TestFitPairsKeepsPairsThatAreNotBlends(t *testing.T) {
	r := image.Rect(0, 0, 200, 4)
	w := solid(r, color.NRGBA{240, 240, 230, 255})
	u := solid(r, color.NRGBA{0, 110, 180, 255})
	imgs := map[string]*image.NRGBA{"w": w, "u": u, "wu": solid(r, color.NRGBA{200, 30, 30, 255})}
	fit := fitPairs(imgs, 200)
	if fit == nil || fit.Drop {
		t.Errorf("fit %v, want one that keeps the pair", fit)
	}
}

func TestFitPairsNeedsBothColors(t *testing.T) {
	r := image.Rect(0, 0, 10, 2)
	if fit := fitPairs(map[string]*image.NRGBA{"wu": solid(r, color.NRGBA{A: 255}), "w": solid(r, color.NRGBA{A: 255})}, 10); fit != nil {
		t.Errorf("fit %v for a pair without its second color", fit)
	}
}

func TestDefaultSeamIsLeftOut(t *testing.T) {
	f := &PairFit{Shape: "smooth", Center: 0, Width: 650, End: 0.94}
	if got := string(mustRaw(f.blend())); got != `{"shape":"smooth","end":0.94}` {
		t.Errorf("blend %s", got)
	}
}
