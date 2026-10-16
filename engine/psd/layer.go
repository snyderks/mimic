package psd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"sort"
)

// Kind is what a layer is
type Kind int

// The layer kinds. A shape layer is a fill whose outline is a vector mask, and a
// fill layer is one without such an outline
const (
	KindPixel Kind = iota
	KindGroup
	KindShape
	KindText
	KindSmartObject
	KindSolidFill
	KindGradientFill
	KindPatternFill
	KindAdjustment
)

var kindNames = [...]string{
	KindPixel:        "pixel",
	KindGroup:        "group",
	KindShape:        "shape",
	KindText:         "text",
	KindSmartObject:  "smartobject",
	KindSolidFill:    "solidfill",
	KindGradientFill: "gradientfill",
	KindPatternFill:  "patternfill",
	KindAdjustment:   "adjustment",
}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Layer is a layer or a group of a document
type Layer struct {
	// Name is the layer's unicode name when it has one and its legacy name otherwise
	Name string
	ID   int
	Kind Kind
	// Rect is the layer's pixel bounds in document coordinates. It may be empty
	// and may reach past the canvas
	Rect    image.Rectangle
	Visible bool
	Opacity uint8
	// FillOpacity scales the layer's pixels but not its effects, 255 when unset
	FillOpacity uint8
	// Blend is the layer's mode. A group's comes from its section divider when
	// that names one, which is how pass through is stored
	Blend BlendMode
	// BlendKey is the four character key the mode was read from
	BlendKey string
	// Clipping clips the layer to the layer below it
	Clipping bool
	Parent   *Layer
	// Children are a group's layers from the top of the panel to the bottom
	Children  []*Layer
	GroupOpen bool

	Mask *Mask
	// RenderedMask is set when the layer has both a user mask and a vector mask.
	// Photoshop then keeps the user mask in channel -3 and a mask it rendered
	// from other data, such as the vector mask, in channel -2, which this
	// describes
	RenderedMask *Mask
	VectorMask   *VectorMask
	Effects      *Effects
	Fill         *Fill
	Adjustment   *Adjustment
	Text         *Text
	SmartObject  *SmartObject

	// BlendClippedAsGroup blends clipped layers as a group, the clbl block, true when unset
	BlendClippedAsGroup bool
	// BlendInteriorAsGroup blends interior effects with the layer, the infx block
	BlendInteriorAsGroup bool
	// TransparencyShapesLayer lets transparency shape the layer, the tsly block,
	// true when unset
	TransparencyShapesLayer bool
	// Knockout is 0 for none, 1 shallow and 2 deep, from the knko block
	Knockout int
	// LayerMaskHidesEffects and VectorMaskHidesEffects come from lmgm and vmgm
	LayerMaskHidesEffects  bool
	VectorMaskHidesEffects bool
	// TransparencyProtected locks the layer's transparent pixels
	TransparencyProtected bool
	// PixelDataIrrelevant marks a layer whose pixels Photoshop renders from other
	// data, such as a shape's outline
	PixelDataIrrelevant bool
	// BlendingRanges is the blend if setting, gray first then each channel
	BlendingRanges []BlendRange
	// Info holds every additional layer information block by key, and InfoKeys
	// their order in the file
	Info     map[string][]byte
	InfoKeys []string
	// Channels lists the layer's channels: 0, 1 and 2 for red, green and blue,
	// -1 for transparency, -2 for the user mask and -3 for the real user mask
	Channels []ChannelInfo
	// Problems lists blocks that did not parse. The layer is still usable
	// without what they hold
	Problems []error

	file     *File
	channels []channel
	index    int
}

// ChannelInfo is a channel and how many bytes it takes in the file
type ChannelInfo struct {
	ID     int
	Length int64
}

// BlendRange is a blend if range as black low, black high, white low and white
// high, for the layer itself and for what lies under it
type BlendRange struct {
	Source, Dest [4]uint8
}

// Mask is a layer's user mask, the one painted in pixels
type Mask struct {
	// Rect is where the mask's pixels lie in document coordinates
	Rect image.Rectangle
	// DefaultColor is the mask's value outside Rect
	DefaultColor     uint8
	Disabled         bool
	Inverted         bool
	PositionRelative bool
	FromRender       bool
	// Density runs from 0 to 255 and Feather is in pixels
	Density uint8
	Feather float64
	// VectorDensity and VectorFeather are the vector mask's properties, which
	// Photoshop stores here
	VectorDensity uint8
	VectorFeather float64

	channel int
}

// channel is where a channel's data lies in the file. offset points at the
// compression field unless bare is set, in which case compression is known and
// counts holds any row counts
type channel struct {
	ID          int
	offset      int64
	length      int64
	compression int
	bare        bool
	counts      []byte
}

// ErrNoMask is returned by MaskImage for a layer without a user mask
var ErrNoMask = errors.New("psd: layer has no user mask")

// IsGroup reports whether the layer is a group
func (l *Layer) IsGroup() bool { return l.Kind == KindGroup }

// Path returns the names from the root to the layer
func (l *Layer) Path() []string {
	var p []string
	for x := l; x != nil && x.Parent != nil; x = x.Parent {
		p = append(p, x.Name)
	}
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// Index is the layer's position in File.Layers
func (l *Layer) Index() int { return l.index }

// Find returns the descendant reached by following names, or nil
func (l *Layer) Find(path ...string) *Layer {
	x := l
	for _, name := range path {
		var next *Layer
		for _, c := range x.Children {
			if c.Name == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		x = next
	}
	return x
}

// VisibleInDocument reports whether the layer and every group above it are visible
func (l *Layer) VisibleInDocument() bool {
	for x := l; x != nil; x = x.Parent {
		if !x.Visible {
			return false
		}
	}
	return true
}

// parseLayerInfo reads the layer count, the layer records and the channel data
// offsets of a layer info section that starts at the stream's position
func (f *File) parseLayerInfo(s *stream) ([]*Layer, bool, error) {
	count := int(int16(s.u16()))
	negative := count < 0
	if negative {
		count = -count
	}
	layers := make([]*Layer, 0, count)
	for i := range count {
		l, err := f.parseRecord(s)
		if err != nil {
			return nil, false, fmt.Errorf("psd: layer record %d: %w", i, err)
		}
		l.index = i
		layers = append(layers, l)
	}
	off := s.pos
	for _, l := range layers {
		for i := range l.channels {
			l.channels[i].offset = off
			off += l.channels[i].length
		}
	}
	if off > s.end {
		return nil, false, fmt.Errorf("psd: channel data of %d bytes passes the end of the layer info section", off-s.pos)
	}
	return layers, negative, nil
}

func (f *File) parseRecord(s *stream) (*Layer, error) {
	l := &Layer{file: f, Info: map[string][]byte{}, FillOpacity: 255, BlendClippedAsGroup: true, TransparencyShapesLayer: true}
	top := int(int32(s.u32()))
	left := int(int32(s.u32()))
	bottom := int(int32(s.u32()))
	right := int(int32(s.u32()))
	l.Rect = image.Rect(left, top, right, bottom)
	if right < left || bottom < top {
		l.Rect = image.Rectangle{Min: image.Pt(left, top), Max: image.Pt(left, top)}
	}
	nch := int(s.u16())
	for range nch {
		id := int(int16(s.u16()))
		n := s.length(f.Large)
		l.channels = append(l.channels, channel{ID: id, length: n})
		l.Channels = append(l.Channels, ChannelInfo{ID: id, Length: n})
	}
	if sig := s.key4(); sig != "8BIM" && s.err == nil {
		return nil, fmt.Errorf("blend mode signature %q", sig)
	}
	l.BlendKey = s.key4()
	l.Opacity = s.u8()
	l.Clipping = s.u8() != 0
	flags := s.u8()
	s.u8()
	l.TransparencyProtected = flags&1 != 0
	l.Visible = flags&2 == 0
	l.PixelDataIrrelevant = flags&16 != 0
	extraLen := int64(s.u32())
	extra := s.copyN(extraLen)
	if s.err != nil {
		return nil, s.err
	}
	if m, ok := blendFromKey(l.BlendKey); ok {
		l.Blend = m
	}

	c := newCursor(extra)
	maskLen := int(c.u32())
	maskData := c.take(maskLen)
	brLen := int(c.u32())
	brData := c.take(brLen)
	l.Name = c.pascal(4)
	if c.err != nil {
		return nil, fmt.Errorf("extra data: %w", c.err)
	}
	if maskLen > 0 {
		l.Mask, l.RenderedMask = parseMask(maskData, l.hasChannel(-3))
	}
	for b := newCursor(brData); b.remaining() >= 8; {
		var r BlendRange
		copy(r.Source[:], b.take(4))
		copy(r.Dest[:], b.take(4))
		l.BlendingRanges = append(l.BlendingRanges, r)
	}
	for c.remaining() >= 12 {
		sig := c.key4()
		if sig != "8BIM" && sig != "8B64" {
			l.Problems = append(l.Problems, fmt.Errorf("psd: additional info signature %q with %d bytes left", sig, c.remaining()+4))
			break
		}
		key := c.key4()
		var n int
		if f.Large && bigKeys[key] {
			n = int(c.u64())
		} else {
			n = int(c.u32())
		}
		data := c.take(n)
		if c.err != nil {
			l.Problems = append(l.Problems, fmt.Errorf("psd: additional info %q of %d bytes passes the end of the record", key, n))
			break
		}
		if _, dup := l.Info[key]; !dup {
			l.InfoKeys = append(l.InfoKeys, key)
			l.Info[key] = data
		}
		if c.remaining() >= 4 && !isSig(c.b[c.p:]) {
			for pad := 1; pad < 4 && c.remaining() > pad; pad++ {
				if isSig(c.b[c.p+pad:]) {
					c.skip(pad)
					break
				}
			}
		}
	}
	return l, nil
}

func isSig(b []byte) bool {
	return len(b) >= 4 && (string(b[:4]) == "8BIM" || string(b[:4]) == "8B64")
}

func (l *Layer) hasChannel(id int) bool {
	for _, c := range l.channels {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (l *Layer) channel(id int) (channel, bool) {
	for _, c := range l.channels {
		if c.ID == id {
			return c, true
		}
	}
	return channel{}, false
}

// parseMask reads the layer mask data. When the layer stores a real user mask
// in channel -3, the first rectangle and flags describe the rendered mask in
// channel -2 and the second set describes the user mask
func parseMask(b []byte, hasReal bool) (user, rendered *Mask) {
	c := newCursor(b)
	rect := func() image.Rectangle {
		t, lf, bt, r := int(c.i32()), int(c.i32()), int(c.i32()), int(c.i32())
		if r < lf || bt < t {
			return image.Rectangle{Min: image.Pt(lf, t), Max: image.Pt(lf, t)}
		}
		return image.Rect(lf, t, r, bt)
	}
	setFlags := func(m *Mask, flags uint8) {
		m.PositionRelative = flags&1 != 0
		m.Disabled = flags&2 != 0
		m.Inverted = flags&4 != 0
		m.FromRender = flags&8 != 0
	}
	m := &Mask{Density: 255, VectorDensity: 255, channel: -2}
	m.Rect = rect()
	m.DefaultColor = c.u8()
	flags := c.u8()
	setFlags(m, flags)
	if hasReal && len(b) >= 36 {
		r := &Mask{Density: 255, VectorDensity: 255, channel: -3}
		realFlags := c.u8()
		r.DefaultColor = c.u8()
		r.Rect = rect()
		setFlags(r, realFlags)
		m, rendered = r, m
	}
	if flags&16 != 0 {
		p := c.u8()
		if p&1 != 0 {
			m.Density = c.u8()
		}
		if p&2 != 0 {
			m.Feather = c.f64()
		}
		if p&4 != 0 {
			m.VectorDensity = c.u8()
		}
		if p&8 != 0 {
			m.VectorFeather = c.f64()
		}
	}
	return m, rendered
}

// interpret reads the typed blocks of a record and decides its kind
func (f *File) interpret(l *Layer) {
	info := l.Info
	if d, ok := info["luni"]; ok {
		c := newCursor(d)
		if s := c.unicode(); c.err == nil {
			l.Name = s
		}
	}
	if d, ok := info["lyid"]; ok && len(d) >= 4 {
		l.ID = int(binary.BigEndian.Uint32(d))
	}
	if d, ok := info["iOpa"]; ok && len(d) >= 1 {
		l.FillOpacity = d[0]
	}
	flag := func(key string, def bool) bool {
		if d, ok := info[key]; ok && len(d) >= 1 {
			return d[0] != 0
		}
		return def
	}
	l.BlendClippedAsGroup = flag("clbl", true)
	l.BlendInteriorAsGroup = flag("infx", false)
	l.TransparencyShapesLayer = flag("tsly", true)
	l.LayerMaskHidesEffects = flag("lmgm", false)
	l.VectorMaskHidesEffects = flag("vmgm", false)
	if d, ok := info["knko"]; ok && len(d) >= 1 {
		l.Knockout = int(d[0])
	}
	problem := func(err error) {
		if err != nil {
			l.Problems = append(l.Problems, err)
		}
	}
	for _, key := range []string{"vmsk", "vsms"} {
		if d, ok := info[key]; ok {
			vm, err := parseVectorMask(d, f.Width, f.Height)
			problem(err)
			l.VectorMask = vm
			break
		}
	}
	for _, key := range []string{"lfx2", "lmfx", "lfxs"} {
		if d, ok := info[key]; ok {
			fx, err := parseEffects(d, f.GlobalAngle, f.GlobalAltitude)
			problem(err)
			l.Effects = fx
			break
		}
	}
	if d, ok := info["lrFX"]; ok && l.Effects == nil {
		fx, err := ParseLegacyEffects(d, f.GlobalAngle, f.GlobalAltitude)
		problem(err)
		l.Effects = fx
	}
	for _, key := range []string{"lsdk", "lsct"} {
		if d, ok := info[key]; ok && len(d) >= 4 {
			if len(d) >= 12 && string(d[4:8]) == "8BIM" {
				if m, ok := blendFromKey(string(d[8:12])); ok {
					l.Blend = m
					l.BlendKey = string(d[8:12])
				}
			}
			break
		}
	}

	var fillKey string
	for _, key := range []string{"SoCo", "PtFl", "GdFl"} {
		if d, ok := info[key]; ok {
			fill, err := parseFill(key, d)
			problem(err)
			l.Fill = fill
			fillKey = key
			break
		}
	}
	switch {
	case info["TySh"] != nil || info["tySh"] != nil:
		l.Kind = KindText
		if d, ok := info["TySh"]; ok {
			t, err := parseText(d)
			problem(err)
			l.Text = t
		}
	case info["SoLd"] != nil || info["SoLE"] != nil || info["PlLd"] != nil || info["plLd"] != nil:
		l.Kind = KindSmartObject
		for _, key := range []string{"SoLd", "SoLE"} {
			if d, ok := info[key]; ok {
				so, err := parseSmartObject(d)
				problem(err)
				l.SmartObject = so
				break
			}
		}
		if l.SmartObject == nil {
			for _, key := range []string{"PlLd", "plLd"} {
				if d, ok := info[key]; ok {
					so, err := parsePlaced(d)
					problem(err)
					l.SmartObject = so
					break
				}
			}
		}
	case fillKey != "":
		switch fillKey {
		case "SoCo":
			l.Kind = KindSolidFill
		case "GdFl":
			l.Kind = KindGradientFill
		case "PtFl":
			l.Kind = KindPatternFill
		}
	default:
		for _, a := range adjustmentKeys {
			if d, ok := info[a.key]; ok {
				l.Kind = KindAdjustment
				l.Adjustment = parseAdjustment(a.key, a.name, d)
				break
			}
		}
	}
	fillOrPixel := l.Kind == KindPixel || l.Kind == KindSolidFill || l.Kind == KindGradientFill || l.Kind == KindPatternFill
	if fillOrPixel && l.PixelDataIrrelevant {
		for _, key := range []string{"vogk", "vmsk", "vsms", "vstk", "vscg"} {
			if _, ok := info[key]; ok {
				l.Kind = KindShape
				break
			}
		}
	}
	if l.Kind == KindShape && l.Fill == nil {
		if d, ok := info["vscg"]; ok && len(d) >= 4 {
			fill, err := parseFill(string(d[:4]), d[4:])
			problem(err)
			l.Fill = fill
		}
	}
}

// sectionType returns a record's section divider type, or 0 for an ordinary layer
func sectionType(l *Layer) int {
	for _, key := range []string{"lsdk", "lsct"} {
		if d, ok := l.Info[key]; ok && len(d) >= 4 {
			return int(binary.BigEndian.Uint32(d))
		}
	}
	return 0
}

// buildTree turns the records, bottom to top with a hidden marker opening each
// group and the group's own record closing it, into the layer tree
func (f *File) buildTree(records []*Layer) error {
	unmatched := map[int]bool{}
	var open []int
	for i, r := range records {
		switch sectionType(r) {
		case 3:
			open = append(open, i)
		case 1, 2:
			if len(open) > 0 {
				open = open[:len(open)-1]
			} else {
				unmatched[i] = true
			}
		}
	}
	for _, i := range open {
		unmatched[i] = true
	}

	stack := []*Layer{f.Root}
	for i, r := range records {
		f.interpret(r)
		cur := stack[len(stack)-1]
		st := sectionType(r)
		if unmatched[i] {
			st = 0
		}
		switch st {
		case 3:
			g := &Layer{}
			stack = append(stack, g)
			continue
		case 1, 2:
			g := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			children := g.Children
			*g = *r
			g.Kind = KindGroup
			g.GroupOpen = st == 1
			g.Children = children
			parent := stack[len(stack)-1]
			g.Parent = parent
			for _, c := range g.Children {
				c.Parent = g
			}
			parent.Children = append(parent.Children, g)
			f.Layers = append(f.Layers, g)
			continue
		}
		r.Parent = cur
		cur.Children = append(cur.Children, r)
		f.Layers = append(f.Layers, r)
	}
	for len(stack) > 1 {
		g := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		parent := stack[len(stack)-1]
		for _, c := range g.Children {
			c.Parent = parent
		}
		parent.Children = append(parent.Children, g.Children...)
	}
	for i, l := range f.Layers {
		l.index = i
	}
	var reverse func(*Layer)
	reverse = func(l *Layer) {
		c := l.Children
		for i, j := 0, len(c)-1; i < j; i, j = i+1, j-1 {
			c[i], c[j] = c[j], c[i]
		}
		for _, x := range c {
			reverse(x)
		}
	}
	reverse(f.Root)
	return nil
}

// Image decodes the layer's color and transparency channels. The image's bounds
// are the layer's Rect, so the pixel at document position (x, y) is
// img.NRGBAAt(x, y). A layer without a transparency channel is opaque, and one
// with an empty Rect gives an empty image
func (l *Layer) Image() (*image.NRGBA, error) {
	f := l.file
	if err := f.checkPixels(); err != nil {
		return nil, err
	}
	r := l.Rect
	if r.Empty() {
		return image.NewNRGBA(image.Rectangle{Min: r.Min, Max: r.Min}), nil
	}
	w, h := r.Dx(), r.Dy()
	ids := [4]int{0, 1, 2, -1}
	planes := make([][]byte, 4)
	errs := make([]error, 4)
	done := make(chan struct{}, 4)
	pending := 0
	for i, id := range ids {
		ch, ok := l.channel(id)
		if !ok {
			continue
		}
		pending++
		go func() {
			planes[i], errs[i] = f.decodeChannel(ch, w, h)
			done <- struct{}{}
		}()
	}
	for range pending {
		<-done
	}
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("psd: layer %q channel %d: %w", l.Name, ids[i], err)
		}
	}
	img := image.NewNRGBA(r)
	interleave(img, planes[0], planes[1], planes[2], planes[3])
	return img, nil
}

// MaskImage decodes the user mask. The image's bounds are Mask.Rect, and outside
// them the mask's value is Mask.DefaultColor
func (l *Layer) MaskImage() (*image.Gray, error) {
	if l.Mask == nil {
		return nil, ErrNoMask
	}
	return l.maskImage(l.Mask)
}

// RenderedMaskImage decodes the mask in channel -2 of a layer that has both kinds
// of mask, with bounds RenderedMask.Rect
func (l *Layer) RenderedMaskImage() (*image.Gray, error) {
	if l.RenderedMask == nil {
		return nil, ErrNoMask
	}
	return l.maskImage(l.RenderedMask)
}

func (l *Layer) maskImage(m *Mask) (*image.Gray, error) {
	f := l.file
	if err := f.checkPixels(); err != nil {
		return nil, err
	}
	r := m.Rect
	img := image.NewGray(r)
	if r.Empty() {
		return img, nil
	}
	ch, ok := l.channel(m.channel)
	if !ok {
		for i := range img.Pix {
			img.Pix[i] = m.DefaultColor
		}
		return img, nil
	}
	p, err := f.decodeChannel(ch, r.Dx(), r.Dy())
	if err != nil {
		return nil, fmt.Errorf("psd: layer %q mask: %w", l.Name, err)
	}
	img.Pix = p
	return img, nil
}

// interleave writes four planes into an image, using zero for a missing color
// plane and opaque for a missing alpha plane
func interleave(img *image.NRGBA, r, g, b, a []byte) {
	pix := img.Pix
	n := len(pix) / 4
	set := func(off int, p []byte, def byte) {
		if p == nil {
			for i := range n {
				pix[4*i+off] = def
			}
			return
		}
		for i := range n {
			pix[4*i+off] = p[i]
		}
	}
	set(0, r, 0)
	set(1, g, 0)
	set(2, b, 0)
	set(3, a, 255)
}

// SortedInfoKeys returns the keys of Info in sorted order
func (l *Layer) SortedInfoKeys() []string {
	keys := make([]string, 0, len(l.Info))
	for k := range l.Info {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
