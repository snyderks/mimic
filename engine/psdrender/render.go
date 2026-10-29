package psdrender

import (
	"fmt"
	"image"
	"math"
	"sort"
	"sync"

	"github.com/odevine/mimic/engine/psd"
)

// Overrides change a document before it is drawn, the way Proxyshop changes a
// template while it renders a card: it shows and hides layers, switches masks on,
// recolors fills and adds fill layers
type Overrides struct {
	// Visible shows or hides a layer whatever the file says
	Visible map[*psd.Layer]bool
	// Mask and VectorMask switch a layer's masks on or off whatever the file says
	Mask       map[*psd.Layer]bool
	VectorMask map[*psd.Layer]bool
	// Fill replaces the content of a fill or shape layer
	Fill map[*psd.Layer]*psd.Fill
	// Insert adds layers at the top of a group, bottom first. A nil key is the
	// document's top level. An inserted layer that clips joins the clipping
	// group of the layer below it
	Insert map[*psd.Layer][]*Inserted
	// MaskFrom draws the key layer with the user mask of the value layer in
	// place of its own, switched on, as Proxyshop copies a mask at render time
	MaskFrom map[*psd.Layer]*psd.Layer
	// VectorMaskFrom does the same for vector masks
	VectorMaskFrom map[*psd.Layer]*psd.Layer
	// MaskHidesEffects sets Photoshop's Layer Mask Hides Effects: the user
	// mask then cuts the layer after its styles are drawn rather than shaping
	// them
	MaskHidesEffects map[*psd.Layer]bool
}

// Inserted is a fill layer added to a document
type Inserted struct {
	Name     string
	Fill     *psd.Fill
	Clipping bool
	Blend    psd.BlendMode
	// Opacity is from 0 to 1, and zero is 1
	Opacity float64
}

// Renderer draws one document. It keeps the pixels and masks it decodes, so
// drawing the same document several ways reads each layer once. Its methods
// may be called from several goroutines at once
type Renderer struct {
	f *psd.File

	mu      sync.Mutex
	cache   map[*psd.Layer]*cached
	masks   map[*psd.Layer]*cachedMask
	vectors map[vectorKey]*cachedMask
	notes   map[string]bool
}

type cached struct {
	once sync.Once
	img  *image.NRGBA
	err  error
}

type cachedMask struct {
	once  sync.Once
	plane *Plane
	err   error
}

type vectorKey struct {
	l *psd.Layer
	r image.Rectangle
}

// New is a renderer for f
func New(f *psd.File) *Renderer {
	r := &Renderer{f: f, notes: map[string]bool{}}
	r.Forget()
	return r
}

// Notes lists what the renderer met and could not draw as Photoshop does
func (r *Renderer) Notes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.notes))
	for n := range r.notes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *Renderer) note(format string, args ...any) {
	r.mu.Lock()
	r.notes[fmt.Sprintf(format, args...)] = true
	r.mu.Unlock()
}

// Forget drops the decoded pixels and masks of every layer
func (r *Renderer) Forget() {
	r.mu.Lock()
	r.cache = map[*psd.Layer]*cached{}
	r.masks = map[*psd.Layer]*cachedMask{}
	r.vectors = map[vectorKey]*cachedMask{}
	r.mu.Unlock()
}

func (r *Renderer) pixels(l *psd.Layer) (*image.NRGBA, error) {
	r.mu.Lock()
	c, ok := r.cache[l]
	if !ok {
		c = &cached{}
		r.cache[l] = c
	}
	r.mu.Unlock()
	c.once.Do(func() { c.img, c.err = l.Image() })
	return c.img, c.err
}

// userMask is the layer's pixel mask as a plane, decoded once
func (r *Renderer) userMask(l *psd.Layer) (*Plane, error) {
	r.mu.Lock()
	c, ok := r.masks[l]
	if !ok {
		c = &cachedMask{}
		r.masks[l] = c
	}
	r.mu.Unlock()
	c.once.Do(func() { c.plane, c.err = userMask(l) })
	return c.plane, c.err
}

// vectorMask is the layer's vector mask rasterized over rect, made once for
// each rectangle
func (r *Renderer) vectorMask(l *psd.Layer, rect image.Rectangle) *Plane {
	k := vectorKey{l, rect}
	r.mu.Lock()
	c, ok := r.vectors[k]
	if !ok {
		c = &cachedMask{}
		r.vectors[k] = c
	}
	r.mu.Unlock()
	c.once.Do(func() {
		c.plane = vectorCoverage(l.VectorMask, rect)
		if m := l.Mask; m != nil {
			if m.VectorFeather > 0 {
				blurPlane(c.plane, featherSigma(m.VectorFeather))
			}
			if m.VectorDensity != 0 && m.VectorDensity != 255 {
				d := float32(m.VectorDensity) / 255
				for i, v := range c.plane.V {
					c.plane.V[i] = 1 - d*(1-v)
				}
				c.plane.Outside = 1 - d*(1-c.plane.Outside)
			}
		}
	})
	return c.plane
}

// pass is one drawing of a document with one set of overrides
type pass struct {
	r   *Renderer
	o   *Overrides
	ext map[*psd.Layer]image.Rectangle
}

func (r *Renderer) newPass(o *Overrides) *pass {
	if o == nil {
		o = &Overrides{}
	}
	return &pass{r: r, o: o, ext: map[*psd.Layer]image.Rectangle{}}
}

// Document draws the whole document over region, as Photoshop shows it with
// the overrides applied
func (r *Renderer) Document(region image.Rectangle, o *Overrides) (*Buffer, error) {
	p := r.newPass(o)
	dst := NewBuffer(region)
	if err := p.drawChildren(r.f.Root, dst); err != nil {
		return nil, err
	}
	return dst, nil
}

// Layer draws one layer or group on its own over region, with its masks,
// styles, fill opacity and opacity, as if it were alone on a transparent
// document in normal mode. The layer is drawn even when it is hidden, and the
// layers inside a group follow their own visibility
func (r *Renderer) Layer(l *psd.Layer, region image.Rectangle, o *Overrides) (*Buffer, error) {
	p := r.newPass(o)
	dst := NewBuffer(region)
	if err := p.drawNode(node{l: l}, nil, dst, true); err != nil {
		return nil, err
	}
	return dst, nil
}

// node is a layer of the file or one an override inserts
type node struct {
	l   *psd.Layer
	ins *Inserted
}

func (n node) clipping() bool {
	if n.ins != nil {
		return n.ins.Clipping
	}
	return n.l.Clipping
}

func (p *pass) visible(l *psd.Layer) bool {
	if v, ok := p.o.Visible[l]; ok {
		return v
	}
	return l.Visible
}

func (p *pass) shown(n node) bool { return n.ins != nil || p.visible(n.l) }

// children lists a group's layers bottom to top, with any inserted ones on top
func (p *pass) children(g *psd.Layer) []node {
	var out []node
	for i := len(g.Children) - 1; i >= 0; i-- {
		out = append(out, node{l: g.Children[i]})
	}
	key := g
	if g == p.r.f.Root {
		key = nil
	}
	for _, ins := range p.o.Insert[key] {
		out = append(out, node{ins: ins})
	}
	return out
}

// drawChildren draws a group's layers onto dst bottom to top, each base layer
// together with the layers clipped to it
func (p *pass) drawChildren(g *psd.Layer, dst *Buffer) error {
	nodes := p.children(g)
	for i := 0; i < len(nodes); {
		j := i + 1
		for j < len(nodes) && nodes[j].clipping() {
			j++
		}
		if err := p.drawNode(nodes[i], nodes[i+1:j], dst, false); err != nil {
			return err
		}
		i = j
	}
	return nil
}

// userMaskOf is the layer whose user mask cuts l in this pass, l itself or
// the layer an override copies the mask from, or nil when none applies
func (p *pass) userMaskOf(l *psd.Layer) *psd.Layer {
	if src, ok := p.o.MaskFrom[l]; ok {
		if src == nil || src.Mask == nil {
			return nil
		}
		return src
	}
	m := l.Mask
	if m == nil || m.Rect.Empty() && m.DefaultColor == 255 {
		return nil
	}
	// A mask Photoshop rendered from the vector mask repeats it
	if m.FromRender && l.VectorMask != nil {
		return nil
	}
	on := !m.Disabled
	if v, ok := p.o.Mask[l]; ok {
		on = v
	}
	if !on {
		return nil
	}
	return l
}

// vectorMaskOf is the layer whose vector mask cuts l in this pass, or nil. A
// shape layer's own outline draws its fill rather than masking it
func (p *pass) vectorMaskOf(l *psd.Layer) *psd.Layer {
	if src, ok := p.o.VectorMaskFrom[l]; ok {
		if src == nil || src.VectorMask == nil {
			return nil
		}
		return src
	}
	if l.VectorMask == nil || l.Kind == psd.KindShape {
		return nil
	}
	on := !l.VectorMask.Disabled
	if v, ok := p.o.VectorMask[l]; ok {
		on = v
	}
	if !on {
		return nil
	}
	return l
}

// maskHidesEffects reports whether l's user mask cuts its styles after they
// are drawn rather than shaping them
func (p *pass) maskHidesEffects(l *psd.Layer) bool {
	if v, ok := p.o.MaskHidesEffects[l]; ok {
		return v
	}
	src := p.userMaskOf(l)
	return src == l && l.LayerMaskHidesEffects
}

// masks is a layer's pixel and vector masks multiplied over region, or nil
// when it has none switched on
func (p *pass) masks(l *psd.Layer, region image.Rectangle) (*Plane, error) {
	var out *Plane
	if src := p.userMaskOf(l); src != nil {
		m, err := p.r.userMask(src)
		if err != nil {
			return nil, err
		}
		out = m
	}
	if src := p.vectorMaskOf(l); src != nil {
		out = mulPlanes(out, p.r.vectorMask(src, region))
	}
	return out, nil
}

// extent is where a layer's own content can be, before its styles. A group's
// is where its layers can paint, leaving out adjustments unless through is set,
// as they change only what a pass through group lies over
func (p *pass) extent(l *psd.Layer, through bool) image.Rectangle {
	if !through {
		if r, ok := p.ext[l]; ok {
			return r
		}
	}
	var r image.Rectangle
	switch {
	case l.IsGroup():
		for _, n := range p.children(l) {
			if n.clipping() || !p.shown(n) {
				continue
			}
			if n.ins != nil {
				r = everywhere
				continue
			}
			if n.l.Kind == psd.KindAdjustment {
				if through {
					r = r.Union(p.maskBounds(n.l, everywhere))
				}
				continue
			}
			r = r.Union(p.reach(n.l, through && n.l.IsGroup() && n.l.Blend == psd.BlendPassThrough))
		}
	case l.Kind == psd.KindAdjustment:
		r = image.Rectangle{}
	case p.o.Fill[l] != nil && l.Kind == psd.KindShape:
		r = l.Rect
		if l.VectorMask != nil {
			r = r.Union(pathBounds(l.VectorMask))
		}
	case p.o.Fill[l] != nil || isFill(l):
		r = everywhere
	default:
		r = l.Rect
	}
	r = p.maskBounds(l, r)
	if !through {
		p.ext[l] = r
	}
	return r
}

func isFill(l *psd.Layer) bool {
	switch l.Kind {
	case psd.KindSolidFill, psd.KindGradientFill, psd.KindPatternFill:
		return l.Fill != nil
	}
	return false
}

// maskBounds limits r to where the layer's masks let anything through
func (p *pass) maskBounds(l *psd.Layer, r image.Rectangle) image.Rectangle {
	if src := p.userMaskOf(l); src != nil && src.Mask.DefaultColor == 0 {
		r = r.Intersect(grow(src.Mask.Rect, int(math.Ceil(3*float64(featherSigma(src.Mask.Feather))))+1))
	}
	if src := p.vectorMaskOf(l); src != nil {
		vm := src.VectorMask
		if !vm.Inverted && vm.InitialFill == 0 && len(vm.Subpaths) > 0 && vm.Subpaths[0].Operation != 2 && vm.Subpaths[0].Operation != 0 {
			feather := 0.0
			if src.Mask != nil {
				feather = src.Mask.VectorFeather
			}
			r = r.Intersect(grow(pathBounds(vm), int(math.Ceil(3*float64(featherSigma(feather))))+1))
		}
	}
	return r
}

// reach is where a layer can paint, its styles included
func (p *pass) reach(l *psd.Layer, through bool) image.Rectangle {
	r := p.extent(l, through)
	if fx := p.effects(l); fx != nil {
		r = grow(r, fxMargin(fx))
	}
	return r
}

// pathBounds is the box around every point of a vector mask
func pathBounds(vm *psd.VectorMask) image.Rectangle {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, sp := range vm.Subpaths {
		for _, k := range sp.Knots {
			for _, q := range []psd.Point{k.Anchor, k.In, k.Out} {
				x0, y0 = math.Min(x0, q.X), math.Min(y0, q.Y)
				x1, y1 = math.Max(x1, q.X), math.Max(y1, q.Y)
			}
		}
	}
	if x0 > x1 {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(x0))-1, int(math.Floor(y0))-1, int(math.Ceil(x1))+1, int(math.Ceil(y1))+1)
}

// drawNode draws a layer and the layers clipped to it onto dst. alone draws it
// as if it were the only layer, in normal mode
func (p *pass) drawNode(n node, clipped []node, dst *Buffer, alone bool) error {
	if n.ins != nil {
		return p.drawInserted(n.ins, clipped, dst)
	}
	l := n.l
	if !alone && !p.visible(l) {
		return nil
	}
	opacity := float32(l.Opacity) / 255
	fill := float32(l.FillOpacity) / 255
	mode := l.Blend
	if alone || mode == psd.BlendPassThrough && !l.IsGroup() {
		mode = psd.BlendNormal
	}
	var shown []node
	for _, c := range clipped {
		if p.shown(c) {
			shown = append(shown, c)
		}
	}
	if l.Kind == psd.KindAdjustment {
		m, err := p.masks(l, dst.Rect)
		if err != nil {
			return err
		}
		p.adjust(l, dst, mode, opacity*fill, m)
		if len(shown) > 0 {
			p.r.note("layers clipped to an adjustment layer are drawn unclipped")
			for _, c := range shown {
				if err := p.drawNode(c, nil, dst, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	fx := p.effects(l)
	if l.IsGroup() && mode == psd.BlendPassThrough && len(shown) == 0 && fx == nil {
		return p.drawThrough(l, dst, opacity)
	}
	if mode == psd.BlendPassThrough {
		mode = psd.BlendNormal
	}
	if mode == psd.BlendDissolve {
		p.r.note("dissolve is drawn as normal")
	}
	margin := 0
	if fx != nil {
		margin = fxMargin(fx)
	}
	ext := p.extent(l, false)
	area := grow(ext, margin).Intersect(dst.Rect)
	if area.Empty() {
		return nil
	}
	crect := grow(ext, margin).Intersect(grow(area, margin))
	if fx == nil {
		crect = ext.Intersect(area)
	} else if l.IsGroup() {
		// Photoshop composites a group only on the canvas, so its styles see
		// nothing beyond it
		crect = crect.Intersect(image.Rect(0, 0, p.r.f.Width, p.r.f.Height))
	}
	content, err := p.content(l, crect)
	if err != nil || content == nil {
		return err
	}
	var shapeBy, hideBy *Plane
	if src := p.userMaskOf(l); src != nil {
		m, err := p.r.userMask(src)
		if err != nil {
			return err
		}
		if fx != nil && p.maskHidesEffects(l) {
			hideBy = m
		} else {
			shapeBy = m
		}
	}
	if src := p.vectorMaskOf(l); src != nil {
		m := p.r.vectorMask(src, crect)
		if fx != nil && src == l && l.VectorMaskHidesEffects {
			hideBy = mulPlanes(hideBy, m)
		} else {
			shapeBy = mulPlanes(shapeBy, m)
		}
	}
	content.mulPlane(shapeBy)
	text := l.Kind == psd.KindText && mode == psd.BlendNormal
	if len(shown) == 0 && fx == nil {
		compositeOp(dst, content, blendOp{mode: mode, opacity: opacity, fill: fill, gamma: text})
		return nil
	}
	if hideBy != nil {
		area = area.Intersect(hideBy.Rect)
		if hideBy.Outside != 0 {
			area = grow(ext, margin).Intersect(dst.Rect)
		}
	}
	return p.drawStack(stack{l: l, content: content, fx: fx, clipped: shown, mode: mode, opacity: opacity, fill: fill, text: text, clbl: l.BlendClippedAsGroup, hide: hideBy}, dst, ext.Intersect(area), area)
}

// drawThrough draws a pass through group's layers straight onto dst, then
// fades what they did by the group's opacity and masks
func (p *pass) drawThrough(l *psd.Layer, dst *Buffer, opacity float32) error {
	area := p.extent(l, true).Intersect(dst.Rect)
	if area.Empty() {
		return nil
	}
	m, err := p.masks(l, area)
	if err != nil {
		return err
	}
	var before *Buffer
	if opacity < 1 || m != nil {
		before = dst.crop(area)
	}
	if err := p.drawChildren(l, dst); err != nil {
		return err
	}
	if before != nil {
		lerpInto(dst, before, opacity, m)
	}
	return nil
}

// stack is a layer with what draws inside its shape: its styles and the
// layers clipped to it
type stack struct {
	l       *psd.Layer
	content *Buffer
	fx      *psd.Effects
	clipped []node
	mode    psd.BlendMode
	opacity float32
	fill    float32
	text    bool
	clbl    bool
	// hide is a mask that cuts the layer and its styles after they are drawn
	hide *Plane
}

// drawStack draws a layer whose styles or clipped layers need it drawn in
// steps. Photoshop draws such a layer as if its shape were solid: its fill, the
// styles inside it and the layers clipped to it are laid over the backdrop at
// full coverage, and the result is then mixed into the backdrop by the shape's
// coverage and the layer's opacity. Styles outside the shape go on first, and
// only a drop shadow the layer does not knock out shows through it
func (p *pass) drawStack(s stack, dst *Buffer, inside, area image.Rectangle) error {
	var save *Buffer
	if s.opacity < 1 || s.hide != nil {
		save = dst.crop(area)
	}
	var st *styled
	if s.fx != nil {
		st = newStyled(p, s.l, s.content)
	}
	work := dst.crop(inside)
	if st != nil {
		st.drawOuter(dst, work)
	}
	solid := solidOf(s.content, inside)
	isolate := s.clbl && s.mode != psd.BlendNormal && len(s.clipped) > 0
	if isolate {
		iso := NewBuffer(inside)
		compositeOp(iso, solid, blendOp{mode: psd.BlendNormal, opacity: 1, fill: s.fill})
		if st != nil {
			st.drawInner(iso)
		}
		for _, c := range s.clipped {
			if err := p.drawNode(c, nil, iso, false); err != nil {
				return err
			}
		}
		if st != nil {
			st.drawTop(iso)
		}
		compositeOp(work, iso, blendOp{mode: s.mode, opacity: 1, fill: 1})
	} else {
		compositeOp(work, solid, blendOp{mode: s.mode, opacity: 1, fill: s.fill})
		if st != nil {
			st.drawInner(work)
		}
		for _, c := range s.clipped {
			if err := p.drawNode(c, nil, work, false); err != nil {
				return err
			}
		}
		if st != nil {
			st.drawTop(work)
		}
	}
	mixInto(dst, work, s.content, s.text)
	if save != nil {
		lerpInto(dst, save, s.opacity, s.hide)
	}
	return nil
}

// solidOf is content's color at full coverage wherever it has any, over r
func solidOf(content *Buffer, r image.Rectangle) *Buffer {
	out := content.crop(r)
	for i := 0; i < len(out.Pix); i += 4 {
		if a := out.Pix[i+3]; a > 0 {
			out.Pix[i] /= a
			out.Pix[i+1] /= a
			out.Pix[i+2] /= a
			out.Pix[i+3] = 1
		}
	}
	return out
}

// mixInto moves dst toward work by the coverage of shape at each pixel, inside
// work's rectangle
func mixInto(dst, work, shape *Buffer, gamma bool) {
	r := dst.Rect.Intersect(work.Rect).Intersect(shape.Rect)
	rows(r, func(y int) {
		d := dst.off(r.Min.X, y)
		w := work.off(r.Min.X, y)
		s := shape.off(r.Min.X, y)
		for i := 0; i < r.Dx(); i++ {
			t := shape.Pix[s+3]
			if t > 0 {
				if gamma && t < 1 {
					mixGamma(dst.Pix[d:d+4], work.Pix[w:w+4], t)
				} else {
					for c := 0; c < 4; c++ {
						dst.Pix[d+c] += (work.Pix[w+c] - dst.Pix[d+c]) * t
					}
				}
			}
			d += 4
			w += 4
			s += 4
		}
	})
}

// mixGamma mixes x into d by t in the text blending space
func mixGamma(d, x []float32, t float32) {
	ad, ax := d[3], x[3]
	ao := ad + (ax-ad)*t
	if ao <= 0 {
		d[0], d[1], d[2], d[3] = 0, 0, 0, 0
		return
	}
	for c := 0; c < 3; c++ {
		var cd, cx float32
		if ad > 0 {
			cd = toText(d[c] / ad)
		}
		if ax > 0 {
			cx = toText(x[c] / ax)
		}
		v := ((1-t)*ad*cd + t*ax*cx) / ao
		d[c] = fromText(v) * ao
	}
	d[3] = ao
}

func insOpacity(ins *Inserted) float32 {
	if ins.Opacity == 0 {
		return 1
	}
	return float32(ins.Opacity)
}

// drawInserted draws a fill layer an override adds, with any layers clipped
// to it
func (p *pass) drawInserted(ins *Inserted, clipped []node, dst *Buffer) error {
	content := p.fillBuffer(ins.Fill, dst.Rect, nil)
	mode := ins.Blend
	if mode == psd.BlendPassThrough {
		mode = psd.BlendNormal
	}
	if len(clipped) == 0 {
		compositeOp(dst, content, blendOp{mode: mode, opacity: insOpacity(ins), fill: 1})
		return nil
	}
	return p.drawStack(stack{content: content, clipped: clipped, mode: mode, opacity: insOpacity(ins), fill: 1, clbl: true}, dst, dst.Rect, dst.Rect)
}

// content is what a layer draws over region before its masks, styles and
// opacity: its pixels, its generated fill, or its children drawn on their own
func (p *pass) content(l *psd.Layer, region image.Rectangle) (*Buffer, error) {
	if region.Empty() {
		return nil, nil
	}
	if l.IsGroup() {
		buf := NewBuffer(region)
		if err := p.drawChildren(l, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	if f, ok := p.o.Fill[l]; ok {
		return p.shapeFill(l, f, region), nil
	}
	if isFill(l) {
		return p.fillBuffer(l.Fill, region, l), nil
	}
	if l.Rect.Empty() {
		return nil, nil
	}
	img, err := p.r.pixels(l)
	if err != nil {
		return nil, fmt.Errorf("layer %q: %w", l.Name, err)
	}
	buf := FromNRGBA(img, region)
	if l.Kind == psd.KindShape {
		extendShape(buf, l.Rect, image.Rect(0, 0, p.r.f.Width, p.r.f.Height))
	}
	return buf, nil
}

// extendShape continues a shape's pixels past each canvas edge its pixels
// stop at, since Photoshop keeps only the part of a shape on the canvas but
// draws its styles from the whole outline
func extendShape(buf *Buffer, rect, canvas image.Rectangle) {
	in := rect.Intersect(canvas).Intersect(buf.Rect)
	if in.Empty() {
		return
	}
	x0, y0, x1, y1 := buf.Rect.Min.X, buf.Rect.Min.Y, buf.Rect.Max.X, buf.Rect.Max.Y
	if rect.Min.X > canvas.Min.X || in.Min.X > canvas.Min.X {
		x0 = in.Min.X
	}
	if rect.Min.Y > canvas.Min.Y || in.Min.Y > canvas.Min.Y {
		y0 = in.Min.Y
	}
	if rect.Max.X < canvas.Max.X || in.Max.X < canvas.Max.X {
		x1 = in.Max.X
	}
	if rect.Max.Y < canvas.Max.Y || in.Max.Y < canvas.Max.Y {
		y1 = in.Max.Y
	}
	out := image.Rect(x0, y0, x1, y1)
	if out == in {
		return
	}
	for y := out.Min.Y; y < out.Max.Y; y++ {
		sy := min(max(y, in.Min.Y), in.Max.Y-1)
		for x := out.Min.X; x < out.Max.X; x++ {
			if image.Pt(x, y).In(in) {
				continue
			}
			sx := min(max(x, in.Min.X), in.Max.X-1)
			copy(buf.Pix[buf.off(x, y):buf.off(x, y)+4], buf.Pix[buf.off(sx, sy):buf.off(sx, sy)+4])
		}
	}
}

// shapeFill fills a shape layer's outline with f, or the layer's own pixels'
// coverage when it has no outline
func (p *pass) shapeFill(l *psd.Layer, f *psd.Fill, region image.Rectangle) *Buffer {
	buf := p.fillBuffer(f, region, l)
	if l.Kind == psd.KindShape && l.VectorMask != nil {
		buf.mulPlane(p.r.vectorMask(l, region))
		return buf
	}
	if l.Kind == psd.KindShape || l.Kind == psd.KindPixel || l.Kind == psd.KindText || l.Kind == psd.KindSmartObject {
		if img, err := p.r.pixels(l); err == nil {
			a := FromNRGBA(img, region)
			buf.mulPlane(&Plane{Rect: region, V: a.Alpha(region)})
		}
	}
	return buf
}
