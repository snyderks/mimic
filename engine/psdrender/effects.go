package psdrender

import (
	"image"
	"math"
	"os"
	"strconv"

	"github.com/odevine/mimic/engine/psd"
)

// Shadow and glow sizes are Photoshop's Size, the reach of the blur in pixels.
// sizeToSigma turns one into the Gaussian that spreads about as far
var sizeToSigma = 1.0 / 2.5

func init() {
	if v := os.Getenv("PSDRENDER_SIGMA"); v != "" {
		sizeToSigma, _ = strconv.ParseFloat(v, 64)
	}
}

// effects returns the layer's styles when any of them draws
func (p *pass) effects(l *psd.Layer) *psd.Effects {
	fx := l.Effects
	if fx == nil || !fx.Visible {
		return nil
	}
	for _, e := range fx.List() {
		if e != nil && e.Base().Active() {
			return fx
		}
	}
	return nil
}

// fxMargin is how far beyond the layer's shape its styles can paint
func fxMargin(fx *psd.Effects) int {
	m := 0.0
	for _, e := range fx.DropShadows {
		if e.Active() {
			m = math.Max(m, e.Distance+e.Size*1.5)
		}
	}
	for _, e := range fx.OuterGlows {
		if e.Active() {
			m = math.Max(m, e.Size*1.5)
		}
	}
	for _, e := range fx.Strokes {
		if e.Active() && e.Position != "inside" {
			m = math.Max(m, e.Size)
		}
	}
	for _, e := range fx.Bevels {
		if e.Active() && e.Style != "inner" {
			m = math.Max(m, e.Size*1.5+e.Soften)
		}
	}
	return int(math.Ceil(m)) + 3
}

// styled is everything one layer's styles need, measured once
type styled struct {
	p      *pass
	l      *psd.Layer
	fx     *psd.Effects
	r      image.Rectangle
	w, h   int
	alpha  []float32
	dist   []float32
	bounds image.Rectangle
}

func newStyled(p *pass, l *psd.Layer, content *Buffer) *styled {
	s := &styled{p: p, l: l, fx: l.Effects, r: content.Rect, w: content.Rect.Dx(), h: content.Rect.Dy()}
	s.alpha = content.Alpha(content.Rect)
	s.bounds = opaqueBounds(s.alpha, s.r)
	return s
}

func (s *styled) plane(v []float32) *Plane { return &Plane{Rect: s.r, V: v} }

// signed is the distance from each pixel to the shape's edge, positive
// outside it and negative inside
func (s *styled) signed() []float32 {
	if s.dist == nil {
		s.dist = signedDistance(s.alpha, s.w, s.h)
	}
	return s.dist
}

// drawOuter draws the styles that lie outside the shape onto dst, bottom to
// top: drop shadows, outer glows, and the outer parts of strokes and bevels.
// A drop shadow the layer does not knock out also goes onto work, the
// backdrop the layer's inside is drawn over
func (s *styled) drawOuter(dst, work *Buffer) {
	fx := s.fx
	for i := len(fx.DropShadows) - 1; i >= 0; i-- {
		e := fx.DropShadows[i]
		if !e.Active() {
			continue
		}
		cov := s.shadow(e, false)
		s.put(dst, colorOf(e.Color), cov, effectMode(e.Blend), e.Opacity)
		if !e.LayerKnocksOut {
			s.put(work, colorOf(e.Color), cov, effectMode(e.Blend), e.Opacity)
		}
	}
	for _, e := range fx.OuterGlows {
		if e.Active() {
			s.glow(dst, e, false)
		}
	}
	for i := len(fx.Strokes) - 1; i >= 0; i-- {
		e := fx.Strokes[i]
		if e.Active() && e.Position != "inside" {
			s.stroke(dst, e, true)
		}
	}
	for _, e := range fx.Bevels {
		if e.Active() && e.Style != "inner" {
			s.bevel(dst, e, true)
		}
	}
}

// drawInner draws the styles that fill the shape onto work, which holds the
// layer drawn as if its shape were solid: overlays, satins, inner glows and
// inner shadows. Their coverage is a share of the shape
func (s *styled) drawInner(work *Buffer) {
	fx := s.fx
	for _, e := range fx.PatternOverlays {
		if e.Active() {
			s.p.r.note("pattern overlays are drawn as mid gray")
			s.put(work, rgb{0.5, 0.5, 0.5}, nil, effectMode(e.Blend), e.Opacity)
		}
	}
	for i := len(fx.GradientOverlays) - 1; i >= 0; i-- {
		e := fx.GradientOverlays[i]
		if e.Active() {
			b := NewBuffer(work.Rect)
			box := s.bounds
			if !e.Align {
				box = image.Rect(0, 0, s.p.r.f.Width, s.p.r.f.Height)
			}
			gs := e.GradientSettings
			paintGradient(b, &gs, box, nil)
			compositeOp(work, b, blendOp{mode: effectMode(e.Blend), opacity: float32(e.Opacity), fill: 1})
		}
	}
	for i := len(fx.ColorOverlays) - 1; i >= 0; i-- {
		e := fx.ColorOverlays[i]
		if e.Active() {
			s.put(work, colorOf(e.Color), nil, effectMode(e.Blend), e.Opacity)
		}
	}
	for _, e := range fx.Satins {
		if e.Active() {
			s.put(work, colorOf(e.Color), s.satin(e), effectMode(e.Blend), e.Opacity)
		}
	}
	for _, e := range fx.InnerGlows {
		if e.Active() {
			s.glow(work, e, true)
		}
	}
	for i := len(fx.InnerShadows) - 1; i >= 0; i-- {
		e := fx.InnerShadows[i]
		if e.Active() {
			s.put(work, colorOf(e.Color), s.shadow(e, true), effectMode(e.Blend), e.Opacity)
		}
	}
}

// drawTop draws the parts of strokes and bevels inside the shape onto work
func (s *styled) drawTop(work *Buffer) {
	fx := s.fx
	for i := len(fx.Strokes) - 1; i >= 0; i-- {
		e := fx.Strokes[i]
		if e.Active() && e.Position != "outside" {
			s.stroke(work, e, false)
		}
	}
	for _, e := range fx.Bevels {
		if e.Active() && e.Style != "outer" {
			s.bevel(work, e, false)
		}
	}
}

// put lays a color over dst with the given coverage, or everywhere when cov
// is nil
func (s *styled) put(dst *Buffer, c rgb, cov []float32, mode psd.BlendMode, opacity float64) {
	var b *Buffer
	if cov == nil {
		b = NewBuffer(dst.Rect.Intersect(s.r))
		fillColor(b, c)
	} else {
		b = colored(s.r, c, cov)
	}
	compositeOp(dst, b, blendOp{mode: mode, opacity: float32(opacity), fill: 1})
}

// effectMode is an effect's blend mode
func effectMode(m psd.BlendMode) psd.BlendMode {
	if m == psd.BlendPassThrough {
		return psd.BlendNormal
	}
	return m
}

func opaqueBounds(alpha []float32, r image.Rectangle) image.Rectangle {
	w := r.Dx()
	x0, y0, x1, y1 := math.MaxInt, math.MaxInt, math.MinInt, math.MinInt
	for i, a := range alpha {
		if a > 0 {
			x, y := i%w, i/w
			x0, y0 = min(x0, x), min(y0, y)
			x1, y1 = max(x1, x), max(y1, y)
		}
	}
	if x0 > x1 {
		return image.Rectangle{}
	}
	return image.Rect(r.Min.X+x0, r.Min.Y+y0, r.Min.X+x1+1, r.Min.Y+y1+1)
}

// colored is a buffer of one color with the given coverage
func colored(r image.Rectangle, c rgb, cover []float32) *Buffer {
	b := NewBuffer(r)
	for i, a := range cover {
		if a > 0 {
			a = clamp01(a)
			b.Pix[4*i], b.Pix[4*i+1], b.Pix[4*i+2], b.Pix[4*i+3] = c[0]*a, c[1]*a, c[2]*a, a
		}
	}
	return b
}

// spreadAndBlur grows a coverage by a fraction of size, then blurs it by the
// rest, the way a shadow's spread or a glow's spread hardens part of its size
func (s *styled) spreadAndBlur(cov []float32, size, spread float64, outside float32) []float32 {
	grow := size * spread / 100
	out := cov
	if grow > 0 {
		d := signedDistance(cov, s.w, s.h)
		out = make([]float32, len(cov))
		for i := range out {
			out[i] = clamp01(float32(grow) + 0.5 - d[i])
		}
	} else {
		out = append([]float32(nil), cov...)
	}
	blurValues(out, s.w, s.h, float32((size-grow)*sizeToSigma), outside)
	return out
}

// shifted moves a coverage by dx, dy, reading outside where it comes from beyond
// the region
func shifted(v []float32, w, h int, dx, dy float64, outside float32) []float32 {
	out := make([]float32, len(v))
	ix, iy := int(math.Floor(dx)), int(math.Floor(dy))
	fx, fy := float32(dx-float64(ix)), float32(dy-float64(iy))
	at := func(x, y int) float32 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return outside
		}
		return v[y*w+x]
	}
	rows(image.Rect(0, 0, w, h), func(y int) {
		for x := 0; x < w; x++ {
			sx, sy := x-ix, y-iy
			a := at(sx, sy)*(1-fx) + at(sx-1, sy)*fx
			b := at(sx, sy-1)*(1-fx) + at(sx-1, sy-1)*fx
			out[y*w+x] = a*(1-fy) + b*fy
		}
	})
	return out
}

// shadow is the coverage of a drop shadow, or of an inner shadow as a share
// of the shape when inner is set
func (s *styled) shadow(e *psd.Shadow, inner bool) []float32 {
	a := e.Angle * math.Pi / 180
	dx, dy := -math.Cos(a)*e.Distance, math.Sin(a)*e.Distance
	src := s.alpha
	outside := float32(0)
	if inner {
		src = make([]float32, len(s.alpha))
		for i, v := range s.alpha {
			src[i] = 1 - v
		}
		outside = 1
	}
	cov := s.spreadAndBlur(shifted(src, s.w, s.h, dx, dy, outside), e.Size, e.Choke, outside)
	applyContour(cov, e.Contour)
	return cov
}

// glow draws an outer glow onto dst, or an inner glow as a share of the shape
// when inner is set
func (s *styled) glow(dst *Buffer, e *psd.Glow, inner bool) {
	src := s.alpha
	outside := float32(0)
	if inner {
		src = make([]float32, len(s.alpha))
		for i, v := range s.alpha {
			src[i] = 1 - v
		}
		outside = 1
	}
	cov := s.spreadAndBlur(src, e.Size, e.Choke, outside)
	if inner && e.Source == "center" {
		for i := range cov {
			cov[i] = 1 - cov[i]
		}
	}
	glowRange(cov, e.Contour, e.Range)
	if e.Gradient != nil && len(e.Gradient.Stops) > 0 {
		b := NewBuffer(s.r)
		rp := newRamp(e.Gradient, false)
		for i, v := range cov {
			if v <= 0 {
				continue
			}
			c := rp.at(1 - v)
			if inner {
				c = rp.at(v)
			}
			a := clamp01(c[3] * v)
			if e.Gradient.Transparency == nil {
				a = clamp01(v)
			}
			b.Pix[4*i], b.Pix[4*i+1], b.Pix[4*i+2], b.Pix[4*i+3] = c[0]*a, c[1]*a, c[2]*a, a
		}
		compositeOp(dst, b, blendOp{mode: effectMode(e.Blend), opacity: float32(e.Opacity), fill: 1})
		return
	}
	s.put(dst, colorOf(e.Color), cov, effectMode(e.Blend), e.Opacity)
}

// glowRange maps a glow's falloff through its contour. The range picks the
// part of the falloff the contour spans, measured from the glow's far end
func glowRange(v []float32, c *psd.Contour, rng float64) {
	r := float32(rng / 100)
	if r <= 0 || r > 1 {
		r = 1
	}
	linear := isLinear(c)
	if linear && r == 0.5 {
		return
	}
	var t *lut
	if !linear {
		t = contourLUT(c)
	}
	for i, x := range v {
		u := x
		if r != 0.5 {
			u = clamp01(1 - (1-x)*0.5/r)
		}
		if t != nil {
			u = t.apply(u)
		}
		v[i] = u
	}
}

// satin is a satin's coverage as a share of the shape: where two offset copies
// of it differ
func (s *styled) satin(e *psd.Satin) []float32 {
	a := e.Angle * math.Pi / 180
	dx, dy := math.Cos(a)*e.Distance/2, -math.Sin(a)*e.Distance/2
	p1 := shifted(s.alpha, s.w, s.h, dx, dy, 0)
	p2 := shifted(s.alpha, s.w, s.h, -dx, -dy, 0)
	cov := make([]float32, len(p1))
	for i := range cov {
		cov[i] = float32(math.Abs(float64(p1[i] - p2[i])))
	}
	blurValues(cov, s.w, s.h, float32(e.Size*sizeToSigma), 0)
	applyContour(cov, e.Contour)
	if e.Invert {
		for i := range cov {
			cov[i] = 1 - cov[i]
		}
	}
	return cov
}

// stroke draws a stroke's part outside the shape onto dst when outer is set,
// and its part inside the shape as a share of it otherwise
func (s *styled) stroke(dst *Buffer, e *psd.Stroke, outer bool) {
	d := s.signed()
	size := float32(e.Size)
	cov := make([]float32, len(d))
	for i, v := range d {
		var c float32
		switch e.Position {
		case "inside":
			c = clamp01(size + 0.5 + v)
		case "center":
			c = clamp01(size/2 + 0.5 - float32(math.Abs(float64(v))))
			if outer && v < 0 || !outer && v > 0 {
				c = 1
			}
		default:
			c = clamp01(size + 0.5 - v)
		}
		cov[i] = c
	}
	mode := effectMode(e.Blend)
	op := blendOp{mode: mode, opacity: float32(e.Opacity), fill: 1}
	switch e.FillType {
	case "gradient":
		if e.Gradient != nil {
			b := NewBuffer(s.r)
			box := s.bounds
			if !e.Gradient.Align {
				box = image.Rect(0, 0, s.p.r.f.Width, s.p.r.f.Height)
			}
			paintGradient(b, e.Gradient, box, s.plane(cov))
			compositeOp(dst, b, op)
			return
		}
	case "pattern":
		s.p.r.note("pattern strokes are drawn as mid gray")
		compositeOp(dst, colored(s.r, rgb{0.5, 0.5, 0.5}, cov), op)
		return
	}
	compositeOp(dst, colored(s.r, colorOf(e.Color), cov), op)
}

// bevel shades a bevel or emboss, its part outside the shape onto dst when
// outer is set and its part inside as a share of the shape otherwise. The
// distance from the shape's edge, shaped by the style and technique, is a
// height field, and each pixel is lit by how its slope faces the light
func (s *styled) bevel(dst *Buffer, e *psd.Bevel, outer bool) {
	w, h := s.w, s.h
	d := s.signed()
	size := float32(math.Max(e.Size, 1))
	height := make([]float32, len(d))
	for i, v := range d {
		var t float32
		switch e.Style {
		case "outer":
			t = 1 - clamp01(v/size)
		case "emboss":
			t = clamp01((-v + size/2) / size)
		case "pillow":
			t = 1 - clamp01(float32(math.Abs(float64(v)))/size)
		default:
			t = clamp01(-v / size)
		}
		height[i] = t
	}
	if e.Technique == "" || e.Technique == "smooth" {
		blurValues(height, w, h, size*0.25, 0)
	}
	az, alt := e.Angle*math.Pi/180, e.Altitude*math.Pi/180
	lx, ly, lz := math.Cos(az)*math.Cos(alt), -math.Sin(az)*math.Cos(alt), math.Sin(alt)
	depth := e.Depth / 100
	if !e.Up {
		depth = -depth
	}
	flat := lz
	hi, sh := make([]float32, len(d)), make([]float32, len(d))
	at := func(x, y int) float32 {
		x, y = min(max(x, 0), w-1), min(max(y, 0), h-1)
		return height[y*w+x]
	}
	scale := depth * float64(size) / 2
	rows(image.Rect(0, 0, w, h), func(y int) {
		for x := 0; x < w; x++ {
			i := y*w + x
			in := d[i] <= 0
			if outer == in {
				continue
			}
			gx := float64(at(x+1, y)-at(x-1, y)) / 2 * scale
			gy := float64(at(x, y+1)-at(x, y-1)) / 2 * scale
			nx, ny, nz := -gx, -gy, 1.0
			n := math.Sqrt(nx*nx + ny*ny + nz*nz)
			dot := (nx*lx + ny*ly + nz*lz) / n
			v := float32((dot - flat) / (1 - flat + 1e-6))
			if dot < flat {
				v = float32((dot - flat) / (flat + 1e-6))
			}
			if v > 0 {
				hi[i] = clamp01(v)
			} else {
				sh[i] = clamp01(-v)
			}
		}
	})
	if e.Soften > 0 {
		blurValues(hi, w, h, float32(e.Soften)/2, 0)
		blurValues(sh, w, h, float32(e.Soften)/2, 0)
	}
	s.put(dst, colorOf(e.ShadowColor), sh, effectMode(e.ShadowBlend), e.ShadowOpacity)
	s.put(dst, colorOf(e.HighlightColor), hi, effectMode(e.HighlightBlend), e.HighlightOpacity)
}

func isLinear(c *psd.Contour) bool {
	return c == nil || len(c.Points) < 2 || len(c.Points) == 2 && c.Points[0].X == 0 && c.Points[0].Y == 0 && c.Points[1].X == 255 && c.Points[1].Y == 255
}

func contourLUT(c *psd.Contour) *lut {
	pts := make([]psd.Point, len(c.Points))
	for i, p := range c.Points {
		pts[i] = psd.Point{X: p.X, Y: p.Y}
	}
	return curveLUT(pts)
}

// applyContour maps each value through a contour curve
func applyContour(v []float32, c *psd.Contour) {
	if isLinear(c) {
		return
	}
	t := contourLUT(c)
	for i := range v {
		v[i] = t.apply(v[i])
	}
}

// signedDistance is the distance from each pixel of a coverage grid to the
// edge of what it covers, positive outside and negative inside. A partly
// covered pixel places the edge by its coverage, and other pixels measure to
// the nearest pixel across the edge, shifted by where that pixel puts it
func signedDistance(cov []float32, w, h int) []float32 {
	in := make([]bool, w*h)
	for i, a := range cov {
		in[i] = a >= 0.5
	}
	toIn, siteIn := edt(in, w, h, true)
	toOut, siteOut := edt(in, w, h, false)
	d := make([]float32, w*h)
	for i, a := range cov {
		switch {
		case a > 0 && a < 1:
			d[i] = 0.5 - a
		case in[i]:
			if siteOut[i] < 0 {
				d[i] = -1e6
			} else {
				d[i] = -(toOut[i] - (0.5 - cov[siteOut[i]]))
			}
		default:
			if siteIn[i] < 0 {
				d[i] = 1e6
			} else {
				d[i] = toIn[i] - (cov[siteIn[i]] - 0.5)
			}
		}
	}
	return d
}

// edt is each pixel's Euclidean distance to the nearest pixel whose membership
// equals target, with that pixel's index, or -1 when there is none
func edt(mask []bool, w, h int, target bool) ([]float32, []int32) {
	const far = 1e20
	sq := make([]float32, w*h)
	row := make([]int32, w*h)
	for i, in := range mask {
		if in != target {
			sq[i] = far
		}
	}
	colPass := func(x0, x1 int) {
		n := h
		f := make([]float32, n)
		v := make([]int, n)
		z := make([]float32, n+1)
		out := make([]float32, n)
		arg := make([]int, n)
		for x := x0; x < x1; x++ {
			for y := 0; y < h; y++ {
				f[y] = sq[y*w+x]
			}
			envelope(f, v, z, out, arg)
			for y := 0; y < h; y++ {
				sq[y*w+x] = out[y]
				row[y*w+x] = int32(arg[y])
			}
		}
	}
	parallelRange(w, colPass)
	site := make([]int32, w*h)
	rowPass := func(y0, y1 int) {
		f := make([]float32, w)
		v := make([]int, w)
		z := make([]float32, w+1)
		out := make([]float32, w)
		arg := make([]int, w)
		for y := y0; y < y1; y++ {
			copy(f, sq[y*w:(y+1)*w])
			envelope(f, v, z, out, arg)
			for x := 0; x < w; x++ {
				sq[y*w+x] = out[x]
				if out[x] >= far {
					site[y*w+x] = -1
				} else {
					sx := arg[x]
					site[y*w+x] = row[y*w+sx]*int32(w) + int32(sx)
				}
			}
		}
	}
	parallelRange(h, rowPass)
	dist := make([]float32, w*h)
	for i, v := range sq {
		dist[i] = float32(math.Sqrt(float64(v)))
	}
	return dist, site
}

// parallelRange splits 0 to n into pieces and calls fn on each from several
// goroutines
func parallelRange(n int, fn func(a, b int)) {
	if n < 64 {
		fn(0, n)
		return
	}
	pieces := 16
	step := (n + pieces - 1) / pieces
	starts := make([]int, 0, pieces)
	for a := 0; a < n; a += step {
		starts = append(starts, a)
	}
	done := make(chan struct{}, len(starts))
	for _, a := range starts {
		go func() {
			fn(a, min(a+step, n))
			done <- struct{}{}
		}()
	}
	for range starts {
		<-done
	}
}

// envelope computes, for a row of squared distances f, the lower envelope of
// the parabolas rooted at each element, giving each position's squared
// distance in out and the element it comes from in arg
func envelope(f []float32, v []int, z, out []float32, arg []int) {
	n := len(f)
	if n == 0 {
		return
	}
	const far = 1e20
	k := -1
	for q := 0; q < n; q++ {
		if f[q] >= far {
			continue
		}
		if k < 0 {
			k = 0
			v[0] = q
			z[0], z[1] = float32(math.Inf(-1)), float32(math.Inf(1))
			continue
		}
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
	if k < 0 {
		for q := range out {
			out[q] = far
			arg[q] = -1
		}
		return
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float32(q) {
			k++
		}
		d := float32(q - v[k])
		out[q] = d*d + f[v[k]]
		arg[q] = v[k]
	}
}
