package psd

import (
	"image"
	"testing"
)

func kindsDoc() testDoc {
	r := image.Rect(0, 0, 2, 2)
	px := func(name string, blocks ...testBlock) testLayer {
		return testLayer{name: name, rect: r, opacity: 255, channels: rgbaChannels(r, 3), blocks: blocks}
	}
	gradFill := &desc{class: "null", items: []kv{
		{"Angl", ang(-90)},
		{"Type", Enum{"GrdT", "Lnr "}},
		{"Grad", &desc{class: "Grdn", items: []kv{
			{"Nm  ", "Noise"},
			{"GrdF", Enum{"GrdF", "ClNs"}},
		}}},
	}}
	pattFill := &desc{class: "null", items: []kv{
		{"Ptrn", &desc{class: "Ptrn", items: []kv{{"Nm  ", "Grain"}, {"Idnt", "abc-123"}}}},
		{"Scl ", pct(50)},
	}}
	so := &desc{class: "null", items: []kv{
		{"Idnt", "uuid-1"},
		{"placed", "uuid-2"},
		{"PgNm", int32(2)},
		{"totalPages", int32(3)},
		{"Trnf", []any{0.0, 0.0, 10.0, 0.0, 10.0, 5.0, 0.0, 5.0}},
		{"nonAffineTransform", []any{0.0, 0.0, 11.0, 0.0, 10.0, 5.0, 0.0, 5.0}},
		{"Sz  ", &desc{class: "Pnt ", items: []kv{{"Wdth", 100.0}, {"Hght", 50.0}}}},
	}}
	var soLd bw
	soLd.str("soLD")
	soLd.u32(4)
	soLd.u32(16)
	soLd.descriptor(so)

	var plLd bw
	plLd.str("plcL")
	plLd.u32(3)
	plLd.pascal("uuid-3", 1)
	plLd.u32(1)
	plLd.u32(1)
	plLd.u32(16)
	plLd.u32(2)
	for _, v := range []float64{1, 2, 3, 4, 5, 6, 7, 8} {
		plLd.f64(v)
	}
	plLd.u32(0)
	plLd.u32(16)
	plLd.descriptor(&desc{class: "warp", items: []kv{{"warpStyle", Enum{"warpStyle", "warpNone"}}}})

	return testDoc{w: 2, h: 2, merged: [][]byte{make([]byte, 4), make([]byte, 4), make([]byte, 4)}, layers: []testLayer{
		px("Solid", testBlock{"SoCo", descBlock([]uint32{16}, &desc{class: "null", items: []kv{{"Clr ", rgb(1, 2, 3)}}})}),
		px("Gradient", testBlock{"GdFl", descBlock([]uint32{16}, gradFill)}),
		px("Pattern", testBlock{"PtFl", descBlock([]uint32{16}, pattFill)}),
		px("Smart", testBlock{"SoLd", soLd.Bytes()}, testBlock{"PlLd", plLd.Bytes()}),
		px("Placed", testBlock{"PlLd", plLd.Bytes()}),
	}}
}

func TestFillAndSmartObjectLayers(t *testing.T) {
	f := decodeDoc(t, kindsDoc())
	s := f.Find("Solid")
	if s.Kind != KindSolidFill || s.Fill.Type != "solid" || s.Fill.Color.B != 3 || s.VectorMask != nil {
		t.Errorf("solid %v %+v", s.Kind, s.Fill)
	}
	g := f.Find("Gradient")
	if g.Kind != KindGradientFill || g.Fill.Gradient.Angle != -90 || g.Fill.Gradient.Style != "linear" || g.Fill.Gradient.Gradient.Form != "noise" || !g.Fill.Gradient.Align || g.Fill.Gradient.Scale != 100 {
		t.Errorf("gradient %v %+v", g.Kind, g.Fill.Gradient)
	}
	p := f.Find("Pattern")
	if p.Kind != KindPatternFill || p.Fill.Pattern.Name != "Grain" || p.Fill.Pattern.ID != "abc-123" || p.Fill.Scale != 50 {
		t.Errorf("pattern %v %+v", p.Kind, p.Fill)
	}
	so := f.Find("Smart").SmartObject
	if f.Find("Smart").Kind != KindSmartObject || so == nil {
		t.Fatal("smart object")
	}
	if so.UniqueID != "uuid-1" || so.Transform[4] != 10 || so.NonAffineTransform[2] != 11 || so.Width != 100 || so.Height != 50 || so.Page != 2 || so.Pages != 3 {
		t.Errorf("smart object %+v", so)
	}
	pl := f.Find("Placed").SmartObject
	if pl == nil || pl.UniqueID != "uuid-3" || pl.Transform != [8]float64{1, 2, 3, 4, 5, 6, 7, 8} || pl.Warp == nil {
		t.Errorf("placed %+v", pl)
	}
	for _, l := range f.Layers {
		if len(l.Problems) > 0 {
			t.Errorf("%s: %v", l.Name, l.Problems)
		}
	}
}
