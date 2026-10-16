package proxyshop

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// The operations in this file cut and shade layers the way the first mimic
// extraction tool did, number for number, so a recipe that asks for them
// reproduces the frames that tool made exactly. Their shading approximates
// Photoshop's layer styles with a few measured numbers, where Render follows the
// PSD's own styles

// Tint recolors the united alpha of shape layers, one variant per color
type Tint struct {
	// From names the layers inside the part's group whose shapes are united
	From []string `json:"from"`
	// Colors maps a variant key to "#RRGGBB", or "#RRGGBB>#RRGGBB" for a
	// gradient across the cut from left to right
	Colors Object `json:"colors"`
	// Stroke draws only a ring along the united shape's edge
	Stroke *TintStroke `json:"stroke,omitempty"`
}

// TintStroke is a ring Width pixels wide along a shape's edge, inside it unless
// Outside is set
type TintStroke struct {
	Width   int  `json:"width"`
	Outside bool `json:"outside,omitempty"`
}

// Bake lays shading measured from one shape in the part's group over every
// variant
type Bake struct {
	// Shape names the layer whose alpha the shading follows
	Shape       string            `json:"shape"`
	Bevel       *LegacyBevel      `json:"bevel,omitempty"`
	InnerShadow *LegacyInnerShade `json:"innerShadow,omitempty"`
	Outline     *LegacyOutline    `json:"outline,omitempty"`
}

// LegacyBevel is an inner bevel. Angle is the light's direction in degrees
// counterclockwise from the right and Altitude its height above the surface.
// Depth is a fraction, Highlight and Shadow the opacity of each face
type LegacyBevel struct {
	Size      int     `json:"size"`
	Depth     float64 `json:"depth"`
	Angle     float64 `json:"angle"`
	Altitude  float64 `json:"altitude"`
	Highlight float64 `json:"highlight"`
	Shadow    float64 `json:"shadow"`
	Down      bool    `json:"down,omitempty"`
}

// LegacyInnerShade darkens the inside of a shape along the edges facing the
// light. Sigma is the blur and Opacity the darkest it gets
type LegacyInnerShade struct {
	Distance float64 `json:"distance"`
	Sigma    float64 `json:"sigma"`
	Angle    float64 `json:"angle"`
	Opacity  float64 `json:"opacity"`
}

// LegacyOutline is a stroke of Width pixels around the outside of a shape
type LegacyOutline struct {
	Width int    `json:"width"`
	Color string `json:"color"`
}

// gradientFrom and gradientTo are the fractions of the cut's width a two color
// tint holds its first color up to and its second color from
const (
	gradientFrom = 0.35
	gradientTo   = 0.65
)

// pixelSource is a layer whose pixels can be read at document coordinates
type pixelSource interface {
	bounds() image.Rectangle
	pixels() (*image.NRGBA, error)
}

// cutLayer copies a layer's stored pixels into a transparent image covering
// crop, ignoring its visibility, blend mode, opacity and masks. A non nil base
// is the layer it is clipped to, whose alpha scales its own
func cutLayer(l pixelSource, crop image.Rectangle, base pixelSource) (*image.NRGBA, error) {
	out := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	src, err := l.pixels()
	if err != nil {
		return nil, err
	}
	var baseImg *image.NRGBA
	if base != nil {
		if baseImg, err = base.pixels(); err != nil {
			return nil, err
		}
	}
	r := l.bounds().Intersect(crop)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := src.NRGBAAt(x, y)
			if baseImg != nil {
				c.A = uint8(uint32(c.A) * uint32(alphaIn(baseImg, x, y)) / 255)
			}
			out.SetNRGBA(x-crop.Min.X, y-crop.Min.Y, c)
		}
	}
	return out, nil
}

// alphaIn is an image's alpha at a document point, zero outside it
func alphaIn(img *image.NRGBA, x, y int) uint8 {
	if !image.Pt(x, y).In(img.Rect) {
		return 0
	}
	return img.Pix[img.PixOffset(x, y)+3]
}

// unionAlpha is the alpha of several layers drawn over one another, within crop
func unionAlpha(layers []pixelSource, crop image.Rectangle) ([]uint8, error) {
	w, h := crop.Dx(), crop.Dy()
	plane := make([]uint8, w*h)
	for _, l := range layers {
		img, err := l.pixels()
		if err != nil {
			return nil, err
		}
		r := l.bounds().Intersect(crop)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				a := alphaIn(img, x, y)
				if a == 0 {
					continue
				}
				i := (y-crop.Min.Y)*w + (x - crop.Min.X)
				plane[i] = uint8(uint32(plane[i]) + uint32(a) - uint32(plane[i])*uint32(a)/255)
			}
		}
	}
	return plane, nil
}

// tintImage colors an alpha plane with a flat color or a left to right gradient
func tintImage(plane []uint8, w, h int, spec string) (*image.NRGBA, error) {
	from, to, err := parseTint(spec)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		t := (float64(x)+0.5)/float64(w) - gradientFrom
		t = min(max(t/(gradientTo-gradientFrom), 0), 1)
		c := color.NRGBA{R: lerp8(from.R, to.R, t), G: lerp8(from.G, to.G, t), B: lerp8(from.B, to.B, t)}
		for y := 0; y < h; y++ {
			if a := plane[y*w+x]; a != 0 {
				c.A = a
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out, nil
}

func lerp8(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5)
}

// parseTint reads a tint spec, returning the same color twice for a flat one
func parseTint(spec string) (from, to color.NRGBA, err error) {
	first, second, gradient := strings.Cut(spec, ">")
	if from, err = parseHex(first); err != nil {
		return from, to, err
	}
	if !gradient {
		return from, from, nil
	}
	to, err = parseHex(second)
	return from, to, err
}

func parseHex(s string) (color.NRGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return color.NRGBA{}, fmt.Errorf("color %q is not #RRGGBB", s)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}

// strokePlane turns a shape's alpha into the ring a stroke draws along its edge,
// width pixels thick, leaving the shape itself empty
func strokePlane(shape []uint8, w, h, width int, outside bool) []uint8 {
	inShape := make([]bool, w*h)
	for i, a := range shape {
		inShape[i] = a >= 128
	}
	dist := distanceTo(inShape, w, h, outside)
	out := make([]uint8, w*h)
	for i := range out {
		if inShape[i] == outside {
			continue
		}
		cover := min(max(float64(width)+1-float64(dist[i]), 0), 1)
		if outside {
			cover *= 1 - float64(shape[i])/255
		} else {
			cover *= float64(shape[i]) / 255
		}
		out[i] = uint8(cover*255 + 0.5)
	}
	return out
}

// distanceTo is each pixel's Euclidean distance to the nearest pixel whose
// membership of mask equals target
func distanceTo(mask []bool, w, h int, target bool) []float32 {
	const far = 1e20
	sq := make([]float32, w*h)
	for i, in := range mask {
		if in != target {
			sq[i] = far
		}
	}
	col := make([]float32, max(w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			col[y] = sq[y*w+x]
		}
		squaredTransform(col[:h])
		for y := 0; y < h; y++ {
			sq[y*w+x] = col[y]
		}
	}
	for y := 0; y < h; y++ {
		squaredTransform(sq[y*w : (y+1)*w])
	}
	for i, v := range sq {
		sq[i] = float32(math.Sqrt(float64(v)))
	}
	return sq
}

// squaredTransform replaces a row of squared distances with the lower envelope
// of the parabolas they define, the squared distance along that row
func squaredTransform(f []float32) {
	n := len(f)
	v := make([]int, n)
	z := make([]float32, n+1)
	k := 0
	z[0], z[1] = float32(math.Inf(-1)), float32(math.Inf(1))
	for q := 1; q < n; q++ {
		var s float32
		for {
			p := v[k]
			s = ((f[q] + float32(q*q)) - (f[p] + float32(p*p))) / float32(2*q-2*p)
			if s > z[k] || k == 0 {
				break
			}
			k--
		}
		k++
		v[k] = q
		z[k], z[k+1] = s, float32(math.Inf(1))
	}
	out := make([]float32, n)
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float32(q) {
			k++
		}
		d := float32(q - v[k])
		out[q] = d*d + f[v[k]]
	}
	copy(f, out)
}

// bakePlanes is the shading and outline of one shape, ready to lay onto each
// of its variants
type bakePlanes struct {
	w, h      int
	highlight []uint8
	shadow    []uint8
	inner     []uint8
	ring      []uint8
	ringColor color.NRGBA
}

func prepareBake(shape []uint8, w, h int, b *Bake) (*bakePlanes, error) {
	p := &bakePlanes{w: w, h: h}
	if b.Bevel != nil {
		p.highlight, p.shadow = bevelPlanes(shape, w, h, *b.Bevel)
	}
	if s := b.InnerShadow; s != nil {
		p.inner = innerShadowPlane(shape, w, h, *s)
	}
	if o := b.Outline; o != nil {
		c, err := parseHex(o.Color)
		if err != nil {
			return nil, err
		}
		p.ring, p.ringColor = strokePlane(shape, w, h, o.Width, true), c
	}
	return p, nil
}

// bevelPlanes shades the band just inside a shape's edge, lighter where the
// face turns toward the light and darker where it turns away
func bevelPlanes(shape []uint8, w, h int, b LegacyBevel) (highlight, shadow []uint8) {
	inShape := make([]bool, w*h)
	for i, a := range shape {
		inShape[i] = a >= 128
	}
	dist := distanceTo(inShape, w, h, false)

	az, alt := b.Angle*math.Pi/180, b.Altitude*math.Pi/180
	lx, ly := math.Cos(az), -math.Sin(az)
	flat := math.Sin(alt)
	slope := b.Depth
	best := (slope*math.Cos(alt)+flat)/math.Sqrt(1+slope*slope) - flat

	highlight, shadow = make([]uint8, w*h), make([]uint8, w*h)
	at := func(x, y int) float64 {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		return float64(dist[y*w+x])
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			d := float64(dist[i])
			if !inShape[i] || d > float64(b.Size) {
				continue
			}
			gx, gy := at(x+1, y)-at(x-1, y), at(x, y+1)-at(x, y-1)
			norm := math.Hypot(gx, gy)
			if norm == 0 {
				continue
			}
			facing := -(gx*lx + gy*ly) / norm
			if b.Down {
				facing = -facing
			}
			delta := (slope*facing*math.Cos(alt)+flat)/math.Sqrt(1+slope*slope) - flat
			edge := float64(shape[i]) / 255
			if delta > 0 {
				highlight[i] = uint8(min(delta/best, 1)*b.Highlight*edge*255 + 0.5)
			} else {
				shadow[i] = uint8(min(-delta/best, 1)*b.Shadow*edge*255 + 0.5)
			}
		}
	}
	return highlight, shadow
}

// apply lays the shading over a variant, then the outline beneath it where the
// variant leaves a pixel partly empty
func (p *bakePlanes) apply(img *image.NRGBA) {
	for y := 0; y < p.h; y++ {
		for x := 0; x < p.w; x++ {
			i := y*p.w + x
			c := img.NRGBAAt(x, y)
			if c.A > 0 {
				if p.inner != nil && p.inner[i] > 0 {
					m := 1 - float64(p.inner[i])/255
					c.R, c.G, c.B = uint8(float64(c.R)*m+0.5), uint8(float64(c.G)*m+0.5), uint8(float64(c.B)*m+0.5)
				}
				if p.shadow != nil && p.shadow[i] > 0 {
					m := 1 - float64(p.shadow[i])/255
					c.R, c.G, c.B = uint8(float64(c.R)*m+0.5), uint8(float64(c.G)*m+0.5), uint8(float64(c.B)*m+0.5)
				}
				if p.highlight != nil && p.highlight[i] > 0 {
					s := float64(p.highlight[i]) / 255
					c.R, c.G, c.B = uint8(float64(c.R)+(255-float64(c.R))*s+0.5), uint8(float64(c.G)+(255-float64(c.G))*s+0.5), uint8(float64(c.B)+(255-float64(c.B))*s+0.5)
				}
				img.SetNRGBA(x, y, c)
			}
			if p.ring != nil && p.ring[i] > 0 && c.A < 255 {
				ring := p.ringColor
				ring.A = p.ring[i]
				img.SetNRGBA(x, y, over(ring, c))
			}
		}
	}
}

// over draws top over bottom, both not premultiplied
func over(bottom, top color.NRGBA) color.NRGBA {
	ta, ba := float64(top.A)/255, float64(bottom.A)/255
	a := ta + ba*(1-ta)
	if a == 0 {
		return color.NRGBA{}
	}
	mix := func(t, b uint8) uint8 {
		return uint8((float64(t)*ta+float64(b)*ba*(1-ta))/a + 0.5)
	}
	return color.NRGBA{R: mix(top.R, bottom.R), G: mix(top.G, bottom.G), B: mix(top.B, bottom.B), A: uint8(a*255 + 0.5)}
}

// innerShadowPlane is the shadow's opacity at each pixel of a shape: the area
// outside it slid away from the light, blurred, and kept where it reaches in
func innerShadowPlane(shape []uint8, w, h int, s LegacyInnerShade) []uint8 {
	az := s.Angle * math.Pi / 180
	ox := int(math.Round(-math.Cos(az) * s.Distance))
	oy := int(math.Round(math.Sin(az) * s.Distance))

	outside := make([]float32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := x-ox, y-oy
			v := float32(1)
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				v = 1 - float32(shape[sy*w+sx])/255
			}
			outside[y*w+x] = v
		}
	}
	blurPlane(outside, w, h, s.Sigma)

	out := make([]uint8, w*h)
	for i, a := range shape {
		if a == 0 {
			continue
		}
		out[i] = uint8(float64(outside[i])*s.Opacity*float64(a) + 0.5)
	}
	return out
}

// blurPlane blurs a plane in place with a Gaussian, one axis at a time, the
// edge repeating
func blurPlane(p []float32, w, h int, sigma float64) {
	if sigma <= 0 {
		return
	}
	radius := int(math.Ceil(sigma * 3))
	kernel := make([]float32, 2*radius+1)
	var sum float32
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = float32(math.Exp(-d * d / (2 * sigma * sigma)))
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	tmp := make([]float32, len(p))
	pass := func(src, dst []float32, n, stride, lines, lineStride int) {
		for l := 0; l < lines; l++ {
			base := l * lineStride
			for i := 0; i < n; i++ {
				var v float32
				for k, wt := range kernel {
					j := min(max(i+k-radius, 0), n-1)
					v += wt * src[base+j*stride]
				}
				dst[base+i*stride] = v
			}
		}
	}
	pass(p, tmp, w, 1, h, w)
	pass(tmp, p, h, w, w, 1)
}
