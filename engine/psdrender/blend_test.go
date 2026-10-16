package psdrender

import (
	"image"
	"math"
	"testing"

	"github.com/odevine/mimic/engine/psd"
)

// TestSeparableModes checks each per channel mode against the values
// Photoshop gives for a backdrop b and a source s
func TestSeparableModes(t *testing.T) {
	cases := []struct {
		mode    psd.BlendMode
		b, s, w float32
	}{
		{psd.BlendMultiply, 0.5, 0.5, 0.25},
		{psd.BlendScreen, 0.5, 0.5, 0.75},
		{psd.BlendDarken, 0.3, 0.6, 0.3},
		{psd.BlendLighten, 0.3, 0.6, 0.6},
		{psd.BlendOverlay, 0.25, 0.75, 0.375},
		{psd.BlendOverlay, 0.75, 0.25, 0.625},
		{psd.BlendHardLight, 0.75, 0.25, 0.375},
		// Photoshop's soft light takes the square root of the backdrop for
		// every light source, unlike the W3C curve
		{psd.BlendSoftLight, 0.25, 0.75, 0.375},
		{psd.BlendSoftLight, 0.25, 0.25, 0.15625},
		{psd.BlendColorDodge, 0.5, 0.5, 1},
		{psd.BlendColorDodge, 0.25, 0.5, 0.5},
		{psd.BlendColorDodge, 0, 1, 0},
		{psd.BlendColorBurn, 0.5, 0.5, 0},
		{psd.BlendColorBurn, 0.75, 0.5, 0.5},
		{psd.BlendColorBurn, 1, 0, 1},
		{psd.BlendLinearDodge, 0.5, 0.75, 1},
		{psd.BlendLinearBurn, 0.5, 0.75, 0.25},
		{psd.BlendLinearLight, 0.5, 0.75, 1},
		{psd.BlendLinearLight, 0.5, 0.375, 0.25},
		{psd.BlendVividLight, 0.25, 0.75, 0.5},
		{psd.BlendVividLight, 0.75, 0.25, 0.5},
		{psd.BlendPinLight, 0.8, 0.25, 0.5},
		{psd.BlendPinLight, 0.2, 0.75, 0.5},
		{psd.BlendHardMix, 0.6, 0.5, 1},
		{psd.BlendHardMix, 0.4, 0.5, 0},
		{psd.BlendDifference, 0.25, 0.75, 0.5},
		{psd.BlendExclusion, 0.5, 0.5, 0.5},
		{psd.BlendExclusion, 0.25, 1, 0.75},
		{psd.BlendSubtract, 0.75, 0.25, 0.5},
		{psd.BlendSubtract, 0.25, 0.75, 0},
		{psd.BlendDivide, 0.25, 0.5, 0.5},
		{psd.BlendDivide, 0.75, 0.5, 1},
	}
	for _, c := range cases {
		f := separable(c.mode)
		if f == nil {
			t.Errorf("%v has no channel function", c.mode)
			continue
		}
		if got := clamp01(f(c.b, c.s)); math.Abs(float64(got-c.w)) > 1e-5 {
			t.Errorf("%v(%v, %v) = %v, want %v", c.mode, c.b, c.s, got, c.w)
		}
	}
}

// TestNonSeparableModes checks the modes that mix hue, saturation and
// luminosity, which Photoshop weighs as 0.3, 0.59 and 0.11
func TestNonSeparableModes(t *testing.T) {
	gray := [3]float32{0.5, 0.5, 0.5}
	red := [3]float32{1, 0, 0}
	cases := []struct {
		mode psd.BlendMode
		b, s [3]float32
		w    [3]float32
	}{
		// Red at the luminosity of mid gray, clipped back into range
		{psd.BlendColor, gray, red, [3]float32{1, 0.2857, 0.2857}},
		{psd.BlendHue, gray, red, gray},
		{psd.BlendSaturation, red, gray, [3]float32{0.3, 0.3, 0.3}},
		{psd.BlendLuminosity, red, gray, [3]float32{1, 0.2857, 0.2857}},
		{psd.BlendDarkerColor, gray, red, red},
		{psd.BlendLighterColor, gray, red, gray},
	}
	for _, c := range cases {
		got := nonSeparable(c.mode, c.b, c.s)
		for i := range got {
			if math.Abs(float64(got[i]-c.w[i])) > 1e-3 {
				t.Errorf("%v(%v, %v) = %v, want %v", c.mode, c.b, c.s, got, c.w)
				break
			}
		}
	}
}

func onePixel(r, g, b, a float32) *Buffer {
	buf := NewBuffer(image.Rect(0, 0, 1, 1))
	buf.Pix[0], buf.Pix[1], buf.Pix[2], buf.Pix[3] = r*a, g*a, b*a, a
	return buf
}

// TestCompositeAlpha checks Photoshop's alpha rule and that a mode blends only
// where both layers are present
func TestCompositeAlpha(t *testing.T) {
	dst := onePixel(1, 1, 1, 0.5)
	composite(dst, onePixel(0, 0, 0, 0.5), psd.BlendMultiply, 1, nil)
	// as + ab(1 - as) is 0.75. Where both are present multiply gives black,
	// where only the backdrop is it stays white
	if a := dst.Pix[3]; math.Abs(float64(a-0.75)) > 1e-6 {
		t.Errorf("alpha %v, want 0.75", a)
	}
	if c := dst.Pix[0] / dst.Pix[3]; math.Abs(float64(c-1.0/3)) > 1e-5 {
		t.Errorf("color %v, want 1/3", c)
	}
	// Over a transparent backdrop any mode shows the source as it is
	dst = onePixel(0, 0, 0, 0)
	composite(dst, onePixel(0.2, 0.4, 0.6, 1), psd.BlendScreen, 0.5, nil)
	if dst.Pix[3] != 0.5 || math.Abs(float64(dst.Pix[1]/dst.Pix[3]-0.4)) > 1e-6 {
		t.Errorf("over transparent got %v", dst.Pix)
	}
}

// TestSpecialEightFill checks that fill opacity weakens the source of the
// special eight modes toward their neutral color, which differs from opacity
// once the blend clips
func TestSpecialEightFill(t *testing.T) {
	src := onePixel(0.8, 0.8, 0.8, 1)
	byOpacity := onePixel(0.6, 0.6, 0.6, 1)
	compositeOp(byOpacity, src, blendOp{mode: psd.BlendLinearDodge, opacity: 0.5, fill: 1})
	byFill := onePixel(0.6, 0.6, 0.6, 1)
	compositeOp(byFill, src, blendOp{mode: psd.BlendLinearDodge, opacity: 1, fill: 0.5})
	// Opacity mixes 0.6 with the clipped 1, fill adds half of 0.8
	if math.Abs(float64(byOpacity.Pix[0]-0.8)) > 1e-5 {
		t.Errorf("linear dodge at half opacity %v, want 0.8", byOpacity.Pix[0])
	}
	if math.Abs(float64(byFill.Pix[0]-1)) > 1e-5 {
		t.Errorf("linear dodge at half fill %v, want 1", byFill.Pix[0])
	}
	burn := onePixel(0.5, 0.5, 0.5, 1)
	compositeOp(burn, onePixel(0.2, 0.2, 0.2, 1), blendOp{mode: psd.BlendLinearBurn, opacity: 1, fill: 0.5})
	// The source becomes 1 - 0.8 * 0.5 = 0.6, and 0.5 + 0.6 - 1 = 0.1
	if math.Abs(float64(burn.Pix[0]-0.1)) > 1e-5 {
		t.Errorf("linear burn at half fill %v, want 0.1", burn.Pix[0])
	}
	// Fill in a mode outside the eight is plain coverage
	mul := onePixel(1, 1, 1, 1)
	compositeOp(mul, onePixel(0, 0, 0, 1), blendOp{mode: psd.BlendMultiply, opacity: 1, fill: 0.25})
	if math.Abs(float64(mul.Pix[0]-0.75)) > 1e-5 {
		t.Errorf("multiply at quarter fill %v, want 0.75", mul.Pix[0])
	}
	if got := hardMixFill(0.5, 0.75, 0.5); math.Abs(float64(got-0.75)) > 1e-5 {
		t.Errorf("hard mix at half fill %v, want 0.75", got)
	}
}

// TestTextGamma checks that text blends in gamma 1.45 over linear light, so
// a quarter covered white edge over black comes out lighter than a quarter
func TestTextGamma(t *testing.T) {
	dst := onePixel(0, 0, 0, 1)
	compositeOp(dst, onePixel(1, 1, 1, 136.0/255), blendOp{mode: psd.BlendNormal, opacity: 1, fill: 1, gamma: true})
	if got := to8(dst.Pix[0]); got < 169 || got > 171 {
		t.Errorf("text edge %d, want 170 as Photoshop draws it", got)
	}
	for _, v := range []float32{0, 0.1, 0.5, 0.9, 1} {
		if got := fromText(toText(v)); math.Abs(float64(got-v)) > 2e-3 {
			t.Errorf("text space round trip of %v gives %v", v, got)
		}
	}
}
