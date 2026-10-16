package psd

import (
	"bytes"
	"errors"
	"image"
	"math"
	"os"
	"strings"
	"testing"
)

const docW, docH = 8, 6

func resolutionResource(ppi float64) []byte {
	var b bw
	b.u32(uint32(ppi * 65536))
	b.u16(1)
	b.u16(1)
	b.u32(uint32(ppi * 65536))
	b.u16(1)
	b.u16(1)
	return b.Bytes()
}

func emptyChannels() []testChannel {
	return []testChannel{{-1, nil}, {0, nil}, {1, nil}, {2, nil}}
}

func rgbaChannels(r image.Rectangle, seed byte) []testChannel {
	n := r.Dx() * r.Dy()
	return []testChannel{
		{-1, pattern(n, seed+3)},
		{0, pattern(n, seed)},
		{1, pattern(n, seed+1)},
		{2, pattern(n, seed+2)},
	}
}

// pathRecord writes one 26 byte path record
func pathRecord(b *bw, sel uint16, body func(*bw)) {
	b.u16(sel)
	var r bw
	if body != nil {
		body(&r)
	}
	for r.Len() < 24 {
		r.u8(0)
	}
	b.Write(r.Bytes())
}

func fixed(v float64) int32 { return int32(math.Round(v * (1 << 24))) }

func knot(b *bw, pts [3]Point) {
	for _, p := range pts {
		b.i32(fixed(p.Y / docH))
		b.i32(fixed(p.X / docW))
	}
}

func vectorMaskBlock(flags uint32) []byte {
	var b bw
	b.u32(3)
	b.u32(flags)
	pathRecord(&b, 6, nil)
	pathRecord(&b, 8, func(r *bw) { r.u16(1) })
	pathRecord(&b, 0, func(r *bw) { r.u16(3); r.i16(2) })
	pathRecord(&b, 2, func(r *bw) { knot(r, [3]Point{{1, 1}, {2, 1}, {3, 1}}) })
	pathRecord(&b, 1, func(r *bw) { knot(r, [3]Point{{6, 2}, {6, 3}, {6, 4}}) })
	pathRecord(&b, 2, func(r *bw) { knot(r, [3]Point{{2, 5}, {2, 5}, {2, 5}}) })
	pathRecord(&b, 3, func(r *bw) { r.u16(1); r.i16(1) })
	pathRecord(&b, 5, func(r *bw) { knot(r, [3]Point{{0, 0}, {4, 3}, {8, 6}}) })
	return b.Bytes()
}

func effectsBlock() []byte {
	contour := &desc{class: "ShpC", items: []kv{
		{"Nm  ", "Linear"},
		{"Crv ", []any{
			&desc{class: "CrPt", items: []kv{{"Hrzn", 0.0}, {"Vrtc", 0.0}}},
			&desc{class: "CrPt", items: []kv{{"Hrzn", 128.0}, {"Vrtc", 200.0}, {"Cnty", false}}},
			&desc{class: "CrPt", items: []kv{{"Hrzn", 255.0}, {"Vrtc", 255.0}}},
		}},
	}}
	shadow := &desc{class: "DrSh", items: []kv{
		{"enab", true}, {"present", true}, {"showInDialog", true},
		{"Md  ", Enum{"BlnM", "Mltp"}},
		{"Clr ", rgb(10, 20, 30)},
		{"Opct", pct(75)},
		{"uglg", true},
		{"lagl", ang(90)},
		{"Dstn", px(5)},
		{"Ckmt", px(10)},
		{"blur", px(7)},
		{"Nose", pct(2)},
		{"AntA", false},
		{"TrnS", contour},
		{"layerConceals", true},
	}}
	second := &desc{class: "DrSh", items: []kv{
		{"enab", false}, {"present", true},
		{"Md  ", Enum{"BlnM", "Nrml"}},
		{"Clr ", &desc{class: "Grsc", items: []kv{{"Gry ", 100.0}}}},
		{"Opct", pct(20)},
		{"uglg", false},
		{"lagl", ang(-45)},
	}}
	stroke := &desc{class: "FrFX", items: []kv{
		{"enab", true}, {"present", true},
		{"Styl", Enum{"FStl", "InsF"}},
		{"PntT", Enum{"FrFl", "SClr"}},
		{"Md  ", Enum{"BlnM", "Nrml"}},
		{"Opct", pct(100)},
		{"Sz  ", px(3)},
		{"Clr ", rgb(255, 0, 0)},
	}}
	bevel := &desc{class: "ebbl", items: []kv{
		{"enab", true}, {"present", true},
		{"hglM", Enum{"BlnM", "Scrn"}},
		{"hglC", rgb(255, 255, 255)},
		{"hglO", pct(60)},
		{"sdwM", Enum{"BlnM", "Mltp"}},
		{"sdwC", rgb(0, 0, 0)},
		{"sdwO", pct(40)},
		{"bvlT", Enum{"bvlT", "PrBL"}},
		{"bvlS", Enum{"BESl", "Embs"}},
		{"uglg", true},
		{"lagl", ang(10)},
		{"Lald", ang(10)},
		{"srgR", pct(150)},
		{"blur", px(4)},
		{"bvlD", Enum{"BESs", "Out "}},
		{"TrnS", contour},
		{"Sftn", px(1)},
		{"useShape", true},
		{"MpgS", contour},
		{"Inpr", pct(70)},
		{"useTexture", false},
	}}
	gradient := &desc{class: "Grdn", items: []kv{
		{"Nm  ", "Custom"},
		{"GrdF", Enum{"GrdF", "CstS"}},
		{"Intr", 4096.0},
		{"Clrs", []any{
			&desc{class: "Clrt", items: []kv{{"Clr ", rgb(0, 0, 0)}, {"Type", Enum{"Clry", "UsrS"}}, {"Lctn", int32(0)}, {"Mdpn", int32(50)}}},
			&desc{class: "Clrt", items: []kv{{"Type", Enum{"Clry", "BckC"}}, {"Lctn", int32(4096)}, {"Mdpn", int32(30)}}},
		}},
		{"Trns", []any{
			&desc{class: "TrnS", items: []kv{{"Opct", pct(100)}, {"Lctn", int32(0)}, {"Mdpn", int32(50)}}},
			&desc{class: "TrnS", items: []kv{{"Opct", pct(25)}, {"Lctn", int32(2048)}, {"Mdpn", int32(50)}}},
		}},
	}}
	overlay := &desc{class: "GrFl", items: []kv{
		{"enab", false}, {"present", true},
		{"Md  ", Enum{"BlnM", "Ovrl"}},
		{"Opct", pct(50)},
		{"Grad", gradient},
		{"Angl", ang(45)},
		{"Type", Enum{"GrdT", "Rdl "}},
		{"Rvrs", true},
		{"Dthr", false},
		{"Algn", false},
		{"Scl ", pct(80)},
		{"Ofst", &desc{class: "Pnt ", items: []kv{{"Hrzn", pct(10)}, {"Vrtc", pct(-5)}}}},
	}}
	inner := &desc{class: "IrSh", items: []kv{{"enab", true}, {"present", false}}}
	glow := &desc{class: "IrGl", items: []kv{
		{"enab", true}, {"present", true},
		{"Md  ", Enum{"BlnM", "Scrn"}},
		{"Clr ", rgb(255, 255, 190)},
		{"Opct", pct(35)},
		{"GlwT", Enum{"BETE", "PrBL"}},
		{"Ckmt", pct(12)},
		{"blur", px(9)},
		{"ShdN", pct(3)},
		{"Inpr", pct(55)},
		{"glwS", Enum{"IGSr", "SrcC"}},
	}}
	root := &desc{class: "null", items: []kv{
		{"Scl ", pct(100)},
		{"masterFXSwitch", true},
		{"DrSh", &desc{class: "DrSh", items: []kv{{"enab", true}, {"present", true}}}},
		{"dropShadowMulti", []any{shadow, second}},
		{"FrFX", stroke},
		{"ebbl", bevel},
		{"GrFl", overlay},
		{"IrSh", inner},
		{"IrGl", glow},
	}}
	return descBlock([]uint32{0, 16}, root)
}

// mainDoc is a document with a group, pixels in every compression, a mask, a
// shape and a merged image with transparency
func mainDoc() testDoc {
	bottom := image.Rect(-2, -1, 5, 4)
	middle := image.Rect(1, 1, 4, 3)
	top := image.Rect(0, 0, docW, docH)
	maskRect := image.Rect(2, 1, 4, 2)
	n := docW * docH
	return testDoc{
		w: docW, h: docH, channels: 4, negative: true,
		resources: map[int][]byte{
			1005: resolutionResource(300),
			1037: u32b(120),
			1049: u32b(45),
		},
		layers: []testLayer{
			{name: "</Layer group>", channels: emptyChannels(), opacity: 255, flags: 2, blocks: []testBlock{{"lsct", sectionBlock(3, "")}}},
			{name: "Bottom", rect: bottom, comp: 1, opacity: 255, channels: rgbaChannels(bottom, 10),
				blocks: []testBlock{{"luni", unicodeBlock("Bottom ü")}, {"lyid", u32b(7)}}},
			{name: "Middle", rect: middle, comp: 0, opacity: 128, clipping: true, flags: 2, blend: "mul ",
				channels: []testChannel{
					{0, pattern(6, 40)}, {1, pattern(6, 41)}, {2, pattern(6, 42)},
					{-2, []byte{9, 200}},
				},
				mask:   &testMask{rect: maskRect, def: 255, flags: 2},
				ranges: []byte{0, 0, 255, 255, 0, 0, 255, 255, 1, 2, 3, 4, 5, 6, 7, 8},
				blocks: []testBlock{{"iOpa", []byte{64, 0, 0, 0}}, {"lyid", u32b(8)}, {"clbl", []byte{0, 0, 0, 0}}, {"knko", []byte{2, 0, 0, 0}}}},
			{name: "Group", channels: emptyChannels(), opacity: 200,
				blocks: []testBlock{{"lsct", sectionBlock(1, "pass")}, {"lyid", u32b(9)}}},
			{name: "Top", rect: top, comp: 3, opacity: 255, channels: rgbaChannels(top, 70),
				blocks: []testBlock{{"lyid", u32b(10)}, {"lfx2", effectsBlock()}}},
			{name: "Empty", comp: 2, opacity: 255, channels: emptyChannels(), blend: "scrn"},
			{name: "Shape", rect: top, comp: 2, opacity: 255, flags: 0x18, channels: rgbaChannels(top, 90),
				blocks: []testBlock{
					{"SoCo", descBlock([]uint32{16}, &desc{class: "null", items: []kv{{"Clr ", rgb(12, 34, 56)}}})},
					{"vmsk", vectorMaskBlock(1)},
					{"vogk", descBlock([]uint32{1, 16}, &desc{class: "null", items: []kv{{"keyDescriptorList", []any{}}}})},
				}},
		},
		merged:    [][]byte{pattern(n, 1), pattern(n, 2), pattern(n, 3), pattern(n, 4)},
		mergedRLE: true,
		global:    []testBlock{{"Patt", []byte{1, 2, 3}}, {"Txt2", []byte("text")}},
	}
}

func TestHeaderAndResources(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	if f.Width != docW || f.Height != docH || f.Depth != 8 || f.ColorMode != 3 || f.Channels != 4 {
		t.Fatalf("header %dx%d depth %d mode %d channels %d", f.Width, f.Height, f.Depth, f.ColorMode, f.Channels)
	}
	if f.Resolution != 300 {
		t.Errorf("resolution %v, want 300", f.Resolution)
	}
	if f.GlobalAngle != 120 || f.GlobalAltitude != 45 {
		t.Errorf("global light %v %v, want 120 45", f.GlobalAngle, f.GlobalAltitude)
	}
	if !f.MergedAlpha {
		t.Error("a negative layer count with a fourth channel should mark merged alpha")
	}
	if string(f.Info["Txt2"]) != "text" || !bytes.Equal(f.Info["Patt"], []byte{1, 2, 3}) {
		t.Errorf("global info %q", f.Info)
	}
	if len(f.Blocks) != 2 || f.Blocks[0].Key != "Patt" || f.Blocks[1].Key != "Txt2" {
		t.Errorf("blocks %+v", f.Blocks)
	}
	if b, err := f.ReadBlock(f.Blocks[1]); err != nil || string(b) != "text" {
		t.Errorf("ReadBlock %q %v", b, err)
	}
}

func TestDefaultsWithoutResources(t *testing.T) {
	d := mainDoc()
	d.resources = nil
	f := decodeDoc(t, d)
	if f.Resolution != 0 || f.GlobalAngle != 30 || f.GlobalAltitude != 30 {
		t.Errorf("defaults %v %v %v", f.Resolution, f.GlobalAngle, f.GlobalAltitude)
	}
}

func names(ls []*Layer) string {
	var s []string
	for _, l := range ls {
		s = append(s, l.Name)
	}
	return strings.Join(s, ",")
}

func TestTree(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	if got := names(f.Root.Children); got != "Shape,Empty,Top,Group" {
		t.Errorf("root children %q", got)
	}
	g := f.Find("Group")
	if g == nil || !g.IsGroup() || !g.GroupOpen {
		t.Fatalf("group %+v", g)
	}
	if got := names(g.Children); got != "Middle,Bottom ü" {
		t.Errorf("group children %q", got)
	}
	if got := names(f.Layers); got != "Bottom ü,Middle,Group,Top,Empty,Shape" {
		t.Errorf("flat layers %q", got)
	}
	for i, l := range f.Layers {
		if l.Index() != i {
			t.Errorf("layer %q index %d, want %d", l.Name, l.Index(), i)
		}
	}
	b := f.Find("Group", "Bottom ü")
	if b == nil || b.Parent != g || b.ID != 7 {
		t.Fatalf("bottom %+v", b)
	}
	if p := strings.Join(b.Path(), "/"); p != "Group/Bottom ü" {
		t.Errorf("path %q", p)
	}
	if f.Find("Group", "Nope") != nil || f.Find() != nil {
		t.Error("Find should return nil for a missing path and for the root")
	}
	if g.Blend != BlendPassThrough || g.Blend.String() != "pass-through" || g.Opacity != 200 || g.ID != 9 {
		t.Errorf("group blend %v opacity %d id %d", g.Blend, g.Opacity, g.ID)
	}
	var walked []string
	f.Walk(func(l *Layer) bool { walked = append(walked, l.Name); return true })
	if got := strings.Join(walked, ","); got != "Shape,Empty,Top,Group,Middle,Bottom ü" {
		t.Errorf("walk %q", got)
	}
}

func TestLayerProperties(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	m := f.Find("Group", "Middle")
	if m.Visible || m.Opacity != 128 || m.FillOpacity != 64 || !m.Clipping || m.Blend != BlendMultiply || m.Blend.String() != "multiply" {
		t.Errorf("middle visible %v opacity %d fill %d clip %v blend %v", m.Visible, m.Opacity, m.FillOpacity, m.Clipping, m.Blend)
	}
	if m.BlendClippedAsGroup || m.Knockout != 2 || !m.TransparencyShapesLayer {
		t.Errorf("middle clbl %v knko %d tsly %v", m.BlendClippedAsGroup, m.Knockout, m.TransparencyShapesLayer)
	}
	if len(m.BlendingRanges) != 2 || m.BlendingRanges[1].Source != [4]uint8{1, 2, 3, 4} || m.BlendingRanges[1].Dest != [4]uint8{5, 6, 7, 8} {
		t.Errorf("blending ranges %+v", m.BlendingRanges)
	}
	if m.VisibleInDocument() {
		t.Error("a hidden layer is not visible in the document")
	}
	if !f.Find("Top").VisibleInDocument() {
		t.Error("top is visible")
	}
	if e := f.Find("Empty"); e.Blend != BlendScreen || !e.Rect.Empty() {
		t.Errorf("empty blend %v rect %v", e.Blend, e.Rect)
	}
	b := f.Find("Group", "Bottom ü")
	if b.Rect != image.Rect(-2, -1, 5, 4) || !b.BlendClippedAsGroup || b.Kind != KindPixel {
		t.Errorf("bottom rect %v clbl %v kind %v", b.Rect, b.BlendClippedAsGroup, b.Kind)
	}
	if len(b.InfoKeys) != 2 || b.InfoKeys[0] != "luni" || b.Info["lyid"] == nil {
		t.Errorf("info keys %v", b.InfoKeys)
	}
	for _, l := range f.Layers {
		if len(l.Problems) > 0 {
			t.Errorf("layer %q problems %v", l.Name, l.Problems)
		}
	}
}

func planesToNRGBA(r image.Rectangle, chans []testChannel) []byte {
	n := r.Dx() * r.Dy()
	pix := make([]byte, 4*n)
	for i := range n {
		pix[4*i+3] = 255
	}
	for _, c := range chans {
		off := map[int]int{0: 0, 1: 1, 2: 2, -1: 3}
		o, ok := off[c.id]
		if !ok {
			continue
		}
		for i := range n {
			pix[4*i+o] = c.plane[i]
		}
	}
	return pix
}

func TestLayerImages(t *testing.T) {
	d := mainDoc()
	f := decodeDoc(t, d)
	for _, tl := range d.layers {
		if tl.rect.Empty() {
			continue
		}
		l := f.Find(tl.name)
		if l == nil {
			l = f.Find("Group", tl.name)
		}
		if l == nil {
			l = f.Find("Group", "Bottom ü")
		}
		img, err := l.Image()
		if err != nil {
			t.Fatalf("%s: %v", tl.name, err)
		}
		if img.Rect != tl.rect {
			t.Errorf("%s: bounds %v, want %v", tl.name, img.Rect, tl.rect)
		}
		if want := planesToNRGBA(tl.rect, tl.channels); !bytes.Equal(img.Pix, want) {
			t.Errorf("%s: pixels differ in compression %d", tl.name, tl.comp)
		}
	}
	mid, _ := f.Find("Group", "Middle").Image()
	if mid.NRGBAAt(1, 1).A != 255 {
		t.Error("a layer without a transparency channel is opaque")
	}
	if c := mid.NRGBAAt(1, 1); c.R != d.layers[2].channels[0].plane[0] {
		t.Errorf("document coordinates do not address the layer's first pixel: %v", c)
	}
	img, err := f.Find("Empty").Image()
	if err != nil || len(img.Pix) != 0 {
		t.Errorf("empty layer gives %v %v", img, err)
	}
}

func TestMask(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	m := f.Find("Group", "Middle")
	if m.Mask == nil {
		t.Fatal("no mask")
	}
	if m.Mask.Rect != image.Rect(2, 1, 4, 2) || m.Mask.DefaultColor != 255 || !m.Mask.Disabled || m.Mask.Inverted {
		t.Errorf("mask %+v", m.Mask)
	}
	img, err := m.MaskImage()
	if err != nil {
		t.Fatal(err)
	}
	if img.Rect != m.Mask.Rect || img.GrayAt(2, 1).Y != 9 || img.GrayAt(3, 1).Y != 200 {
		t.Errorf("mask image %v %v", img.Rect, img.Pix)
	}
	if _, err := f.Find("Top").MaskImage(); !errors.Is(err, ErrNoMask) {
		t.Errorf("no mask error %v", err)
	}
}

func TestRealUserMask(t *testing.T) {
	r := image.Rect(0, 0, 4, 2)
	rendered := image.Rect(1, 0, 3, 1)
	user := image.Rect(0, 1, 2, 2)
	d := testDoc{w: 4, h: 2, merged: [][]byte{make([]byte, 8), make([]byte, 8), make([]byte, 8)}, layers: []testLayer{{
		name: "Both", rect: r, opacity: 255, comp: 1,
		channels: append(rgbaChannels(r, 5), testChannel{-2, []byte{1, 2}}, testChannel{-3, []byte{3, 4}}),
		mask:     &testMask{rect: rendered, def: 0, flags: 8, real: &testMask{rect: user, def: 255, flags: 2}},
		blocks:   []testBlock{{"vmsk", vectorMaskBlock(0)}},
	}}}
	f := decodeDoc(t, d)
	l := f.Find("Both")
	if l.Mask == nil || l.RenderedMask == nil {
		t.Fatalf("masks %+v %+v", l.Mask, l.RenderedMask)
	}
	if l.Mask.Rect != user || l.Mask.DefaultColor != 255 || !l.Mask.Disabled || l.Mask.FromRender {
		t.Errorf("user mask %+v", l.Mask)
	}
	if l.RenderedMask.Rect != rendered || !l.RenderedMask.FromRender || l.RenderedMask.Disabled {
		t.Errorf("rendered mask %+v", l.RenderedMask)
	}
	um, err := l.MaskImage()
	if err != nil || !bytes.Equal(um.Pix, []byte{3, 4}) {
		t.Errorf("user mask pixels %v %v", um, err)
	}
	rm, err := l.RenderedMaskImage()
	if err != nil || !bytes.Equal(rm.Pix, []byte{1, 2}) {
		t.Errorf("rendered mask pixels %v %v", rm, err)
	}
}

func TestComposite(t *testing.T) {
	d := mainDoc()
	f := decodeDoc(t, d)
	img, err := f.Composite()
	if err != nil {
		t.Fatal(err)
	}
	if img.Rect != image.Rect(0, 0, docW, docH) {
		t.Fatalf("bounds %v", img.Rect)
	}
	for i := range docW * docH {
		r, a := d.merged[0][i], d.merged[3][i]
		want := r
		if a != 0 && a != 255 {
			v := float32(int(r)+int(a)-255) * 255 / float32(a)
			want = byte(min(max(v, 0), 255))
		}
		if got := img.Pix[4*i]; got != want || img.Pix[4*i+3] != a {
			t.Fatalf("pixel %d: got %d alpha %d, want %d alpha %d", i, got, img.Pix[4*i+3], want, a)
		}
	}

	d.channels = 3
	d.negative = false
	d.merged = d.merged[:3]
	d.mergedRLE = false
	f = decodeDoc(t, d)
	img, err = f.Composite()
	if err != nil {
		t.Fatal(err)
	}
	if f.MergedAlpha || img.Pix[3] != 255 || img.Pix[0] != d.merged[0][0] || img.Pix[4*7+2] != d.merged[2][7] {
		t.Errorf("raw merged image without alpha: alpha flag %v first pixel %v", f.MergedAlpha, img.Pix[:4])
	}
}

func TestUnmatte(t *testing.T) {
	pix := []byte{255, 128, 0, 0, 191, 255, 64, 128, 10, 20, 30, 255}
	unmatte(pix)
	want := []byte{255, 128, 0, 0, 127, 255, 0, 128, 10, 20, 30, 255}
	if !bytes.Equal(pix, want) {
		t.Errorf("unmatte %v, want %v", pix, want)
	}
}

func TestShapeAndVectorMask(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	s := f.Find("Shape")
	if s.Kind != KindShape || !s.PixelDataIrrelevant {
		t.Fatalf("kind %v irrelevant %v", s.Kind, s.PixelDataIrrelevant)
	}
	if s.Fill == nil || s.Fill.Type != "solid" || s.Fill.Color.R != 12 || s.Fill.Color.G != 34 || s.Fill.Color.B != 56 {
		t.Errorf("fill %+v", s.Fill)
	}
	vm := s.VectorMask
	if vm == nil || !vm.Inverted || vm.NotLinked || vm.Disabled || vm.InitialFill != 1 || !vm.HasFillRule {
		t.Fatalf("vector mask %+v", vm)
	}
	if len(vm.Subpaths) != 2 {
		t.Fatalf("subpaths %d", len(vm.Subpaths))
	}
	sp := vm.Subpaths[0]
	if !sp.Closed || sp.Operation != 2 || len(sp.Knots) != 3 {
		t.Fatalf("subpath %+v", sp)
	}
	k := sp.Knots[1]
	near := func(a, b Point) bool { return math.Abs(a.X-b.X) < 1e-6 && math.Abs(a.Y-b.Y) < 1e-6 }
	if !near(k.In, Point{6, 2}) || !near(k.Anchor, Point{6, 3}) || !near(k.Out, Point{6, 4}) || !k.Linked || sp.Knots[0].Linked {
		t.Errorf("knot %+v", k)
	}
	if o := vm.Subpaths[1]; o.Closed || o.Operation != 1 || len(o.Knots) != 1 || !near(o.Knots[0].Anchor, Point{4, 3}) {
		t.Errorf("open subpath %+v", o)
	}
}

func TestEffects(t *testing.T) {
	f := decodeDoc(t, mainDoc())
	fx := f.Find("Top").Effects
	if fx == nil {
		t.Fatal("no effects")
	}
	if fx.Scale != 100 || !fx.Visible {
		t.Errorf("scale %v visible %v", fx.Scale, fx.Visible)
	}
	if len(fx.DropShadows) != 2 {
		t.Fatalf("a multi list replaces the single entry of its kind, got %d drop shadows", len(fx.DropShadows))
	}
	ds := fx.DropShadows[0]
	if !ds.Active() || !ds.ShowInDialog || ds.Blend != BlendMultiply || ds.Opacity != 0.75 {
		t.Errorf("drop shadow base %+v", ds.EffectBase)
	}
	if !ds.UseGlobalLight || ds.Angle != 120 || ds.Distance != 5 || ds.Choke != 10 || ds.Size != 7 || ds.Noise != 2 || !ds.LayerKnocksOut {
		t.Errorf("drop shadow %+v", ds)
	}
	if ds.Color.R != 10 || ds.Color.G != 20 || ds.Color.B != 30 || ds.Color.Model != "rgb" {
		t.Errorf("drop shadow color %+v", ds.Color)
	}
	if ds.Contour == nil || ds.Contour.Name != "Linear" || len(ds.Contour.Points) != 3 || !ds.Contour.Points[1].Corner || ds.Contour.Points[0].Corner || ds.Contour.Points[1].Y != 200 {
		t.Errorf("contour %+v", ds.Contour)
	}
	ds2 := fx.DropShadows[1]
	if ds2.Enabled || ds2.Angle != -45 || ds2.Color.R != 0 || ds2.Color.Model != "gray" || ds2.Opacity != 0.2 {
		t.Errorf("second drop shadow %+v", ds2)
	}
	if len(fx.Strokes) != 1 {
		t.Fatalf("strokes %d", len(fx.Strokes))
	}
	st := fx.Strokes[0]
	if st.Size != 3 || st.Position != "inside" || st.FillType != "color" || st.Color.R != 255 || st.Opacity != 1 {
		t.Errorf("stroke %+v", st)
	}
	b := fx.Bevels[0]
	if b.Style != "emboss" || b.Technique != "chisel-hard" || b.Up || b.Depth != 150 || b.Size != 4 || b.Soften != 1 {
		t.Errorf("bevel %+v", b)
	}
	if b.Angle != 120 || b.Altitude != 45 {
		t.Errorf("bevel light %v %v, want the global 120 and 45", b.Angle, b.Altitude)
	}
	if b.HighlightBlend != BlendScreen || b.HighlightOpacity != 0.6 || b.ShadowBlend != BlendMultiply || b.ShadowOpacity != 0.4 || !b.UseShape || b.Range != 70 || b.Contour == nil {
		t.Errorf("bevel shading %+v", b)
	}
	g := fx.GradientOverlays[0]
	if g.Enabled || !g.Present || g.Blend != BlendOverlay || g.Opacity != 0.5 {
		t.Errorf("gradient overlay base %+v", g.EffectBase)
	}
	if g.Angle != 45 || g.Style != "radial" || !g.Reverse || g.Align || g.Scale != 80 || g.Offset != (Point{10, -5}) {
		t.Errorf("gradient settings %+v", g.GradientSettings)
	}
	gr := g.Gradient
	if gr == nil || gr.Name != "Custom" || gr.Form != "custom" || len(gr.Stops) != 2 || len(gr.Transparency) != 2 {
		t.Fatalf("gradient %+v", gr)
	}
	if gr.Stops[1].Location != 4096 || gr.Stops[1].Midpoint != 30 || gr.Stops[1].Type != "background" || gr.Transparency[1].Opacity != 0.25 || gr.Transparency[1].Location != 2048 {
		t.Errorf("stops %+v %+v", gr.Stops, gr.Transparency)
	}
	if len(fx.InnerShadows) != 1 || fx.InnerShadows[0].Present || fx.InnerShadows[0].Active() {
		t.Errorf("an effect that is not present is listed but not active: %+v", fx.InnerShadows)
	}
	ig := fx.InnerGlows[0]
	if ig.Technique != "precise" || ig.Source != "center" || ig.Choke != 12 || ig.Size != 9 || ig.Jitter != 3 || ig.Range != 55 || ig.Color.B != 190 {
		t.Errorf("inner glow %+v", ig)
	}
	var kinds []string
	for _, e := range fx.List() {
		kinds = append(kinds, e.Base().Kind.String())
	}
	if got := strings.Join(kinds, ","); got != "drop-shadow,drop-shadow,stroke,bevel-emboss,gradient-overlay,inner-shadow,inner-glow" {
		t.Errorf("list %q", got)
	}
}

func TestDecodeErrors(t *testing.T) {
	raw := mainDoc().build()
	if _, err := Decode(bytes.NewReader([]byte("8BPX")), 4); err == nil {
		t.Error("a bad signature should fail")
	}
	for _, n := range []int{10, 30, 60, 200} {
		if _, err := Decode(bytes.NewReader(raw[:n]), int64(n)); err == nil {
			t.Errorf("a document cut to %d bytes should fail", n)
		}
	}
	bad := append([]byte(nil), raw...)
	bad[4], bad[5] = 0, 9
	if _, err := Decode(bytes.NewReader(bad), int64(len(bad))); err == nil {
		t.Error("an unknown version should fail")
	}
}

func TestPixelFormatGuard(t *testing.T) {
	raw := mainDoc().build()
	raw[22], raw[23] = 0, 16
	f, err := Decode(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("structure of a 16 bit document should still parse: %v", err)
	}
	if _, err := f.Find("Top").Image(); !errors.Is(err, errPixelFormat) {
		t.Errorf("Image on 16 bit: %v", err)
	}
	if _, err := f.Composite(); !errors.Is(err, errPixelFormat) {
		t.Errorf("Composite on 16 bit: %v", err)
	}
}

func TestOpen(t *testing.T) {
	path := t.TempDir() + "/doc.psd"
	if err := os.WriteFile(path, mainDoc().build(), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Find("Top").Image(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("a second Close should do nothing: %v", err)
	}
	if _, err := Open(t.TempDir() + "/missing.psd"); err == nil {
		t.Error("a missing file should fail")
	}
}

func TestUnpackBits(t *testing.T) {
	row := []byte{1, 1, 1, 2, 3, 4, 4, 5}
	out := make([]byte, len(row))
	if err := unpackBits(packBits(row), out); err != nil || !bytes.Equal(out, row) {
		t.Errorf("round trip %v %v", out, err)
	}
	if err := unpackBits([]byte{0x80, 2, 9, 8, 7}, make([]byte, 3)); err != nil {
		t.Errorf("a 128 header is a no op: %v", err)
	}
	if err := unpackBits([]byte{0xFE, 5}, make([]byte, 2)); err == nil {
		t.Error("a run past the row should fail")
	}
	if err := unpackBits([]byte{0x00, 5}, make([]byte, 2)); err == nil {
		t.Error("a row that ends short should fail")
	}
}

func TestBlendModes(t *testing.T) {
	cases := map[string]string{
		"pass": "pass-through", "norm": "normal", "diss": "dissolve", "dark": "darken", "mul ": "multiply",
		"idiv": "color-burn", "lbrn": "linear-burn", "dkCl": "darker-color", "lite": "lighten", "scrn": "screen",
		"div ": "color-dodge", "lddg": "linear-dodge", "lgCl": "lighter-color", "over": "overlay", "sLit": "soft-light",
		"hLit": "hard-light", "vLit": "vivid-light", "lLit": "linear-light", "pLit": "pin-light", "hMix": "hard-mix",
		"diff": "difference", "smud": "exclusion", "fsub": "subtract", "fdiv": "divide", "hue ": "hue",
		"sat ": "saturation", "colr": "color", "lum ": "luminosity",
	}
	for k, want := range cases {
		m, ok := blendFromKey(k)
		if !ok || m.String() != want {
			t.Errorf("%q gives %v, want %s", k, m, want)
		}
	}
	if _, ok := blendFromKey("zzzz"); ok {
		t.Error("an unknown key should not map")
	}
	if blendFromEnum("linearDodge") != BlendLinearDodge || blendFromEnum("CBrn") != BlendColorBurn {
		t.Error("descriptor blend names")
	}
}

func TestRecordsAcrossBufferChunks(t *testing.T) {
	d := mainDoc()
	big := make([]byte, streamChunk+streamChunk/2)
	for i := range big {
		big[i] = byte(i)
	}
	d.layers[1].blocks = append(d.layers[1].blocks, testBlock{"shmd", big})
	d.global = append(d.global, testBlock{"Lnk2", big})
	f := decodeDoc(t, d)
	b := f.Find("Group", "Bottom ü")
	if !bytes.Equal(b.Info["shmd"], big) {
		t.Error("a block larger than the read buffer comes back whole")
	}
	if got := names(f.Layers); got != "Bottom ü,Middle,Group,Top,Empty,Shape" {
		t.Errorf("layers after a large block %q", got)
	}
	img, err := f.Find("Top").Image()
	if err != nil {
		t.Fatal(err)
	}
	if want := planesToNRGBA(d.layers[4].rect, d.layers[4].channels); !bytes.Equal(img.Pix, want) {
		t.Error("channel offsets after a large block")
	}
	if !bytes.Equal(f.Info["Lnk2"], big) {
		t.Error("global block larger than the read buffer")
	}
}

// TestLargeDocument reads the PSB form of mainDoc, whose section lengths, channel
// lengths, some block lengths and RLE row counts are wider, and expects what the
// PSD form gives
func TestLargeDocument(t *testing.T) {
	small := decodeDoc(t, mainDoc())
	d := mainDoc()
	d.large = true
	d.layers[2].blocks = append(d.layers[2].blocks, testBlock{"FXid", []byte{1, 2, 3, 4}})
	d.global = append(d.global, testBlock{"lnk2", []byte("linked")})
	f := decodeDoc(t, d)
	if !f.Large || small.Large {
		t.Fatalf("large %v and %v", f.Large, small.Large)
	}
	if got := names(f.Layers); got != names(small.Layers) {
		t.Fatalf("layers %q, want %q", got, names(small.Layers))
	}
	for i, l := range f.Layers {
		s := small.Layers[i]
		if l.Rect.Empty() {
			continue
		}
		a, err := l.Image()
		if err != nil {
			t.Fatalf("%s: %v", l.Name, err)
		}
		b, _ := s.Image()
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Errorf("%s: pixels differ from the PSD form", l.Name)
		}
	}
	m, err := f.Find("Group", "Middle").MaskImage()
	if err != nil || !bytes.Equal(m.Pix, []byte{9, 200}) {
		t.Errorf("mask %v %v", m, err)
	}
	if !bytes.Equal(f.Find("Group", "Middle").Info["FXid"], []byte{1, 2, 3, 4}) {
		t.Errorf("a block with an 8 byte length %v", f.Find("Group", "Middle").Info)
	}
	if string(f.Info["lnk2"]) != "linked" || string(f.Info["Txt2"]) != "text" {
		t.Errorf("global blocks %q", f.Info)
	}
	a, err := f.Composite()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := small.Composite()
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("composite differs from the PSD form")
	}
	if f.Find("Shape").VectorMask == nil || f.Find("Top").Effects == nil {
		t.Error("typed blocks of the PSB form")
	}
}

// TestLayerInfoInLr16 reads a document that keeps its layers in an Lr16 block,
// as a 16 bit document does
func TestLayerInfoInLr16(t *testing.T) {
	d := mainDoc()
	d.lr16 = true
	f := decodeDoc(t, d)
	if got := names(f.Layers); got != "Bottom ü,Middle,Group,Top,Empty,Shape" {
		t.Fatalf("layers %q", got)
	}
	img, err := f.Find("Top").Image()
	if err != nil {
		t.Fatal(err)
	}
	if want := planesToNRGBA(d.layers[4].rect, d.layers[4].channels); !bytes.Equal(img.Pix, want) {
		t.Error("channel offsets inside the Lr16 block")
	}
}

func TestMaskParameters(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	var extra bw
	extra.u8(1 | 2 | 4 | 8)
	extra.u8(128)
	extra.f64(2.5)
	extra.u8(64)
	extra.f64(0.75)
	d := testDoc{w: 2, h: 2, merged: [][]byte{make([]byte, 4), make([]byte, 4), make([]byte, 4)}, layers: []testLayer{{
		name: "Feathered", rect: r, opacity: 255, channels: append(rgbaChannels(r, 2), testChannel{-2, []byte{1, 2, 3, 4}}),
		mask: &testMask{rect: r, def: 0, flags: 16 | 4 | 1, rawExtra: extra.Bytes()},
	}}}
	f := decodeDoc(t, d)
	m := f.Find("Feathered").Mask
	if m == nil || !m.Inverted || !m.PositionRelative || m.Disabled || m.Density != 128 || m.Feather != 2.5 || m.VectorDensity != 64 || m.VectorFeather != 0.75 {
		t.Errorf("mask %+v", m)
	}
	img, err := f.Find("Feathered").MaskImage()
	if err != nil || !bytes.Equal(img.Pix, []byte{1, 2, 3, 4}) {
		t.Errorf("mask pixels %v %v", img, err)
	}
}

// TestUnbalancedDividers keeps layers whose group markers do not pair up, and
// markers of type 0, as ordinary layers
func TestUnbalancedDividers(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	px := func(name string, blocks ...testBlock) testLayer {
		return testLayer{name: name, rect: r, opacity: 255, channels: rgbaChannels(r, 4), blocks: blocks}
	}
	d := testDoc{w: 2, h: 2, merged: [][]byte{make([]byte, 4), make([]byte, 4), make([]byte, 4)}, layers: []testLayer{
		px("Stray end", testBlock{"lsct", sectionBlock(1, "")}),
		px("A"),
		px("Stray start", testBlock{"lsct", sectionBlock(3, "")}),
		px("B"),
		px("Other", testBlock{"lsct", sectionBlock(0, "")}),
	}}
	f := decodeDoc(t, d)
	if got := names(f.Root.Children); got != "Other,B,Stray start,A,Stray end" {
		t.Errorf("root %q", got)
	}
	for _, l := range f.Layers {
		if l.IsGroup() {
			t.Errorf("%s became a group", l.Name)
		}
	}
}
