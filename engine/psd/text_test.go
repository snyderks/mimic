package psd

import (
	"image"
	"strings"
	"testing"
)

func textBlock(engine []byte) []byte {
	var b bw
	b.u16(1)
	for _, v := range []float64{2, 0, 0, 2, 10, 20} {
		b.f64(v)
	}
	b.u16(50)
	b.u32(16)
	b.descriptor(&desc{class: "TxLr", items: []kv{
		{"Txt ", "Hello\rWorld"},
		{"textGridding", Enum{"textGridding", "None"}},
		{"Ornt", Enum{"Ornt", "Hrzn"}},
		{"AntA", Enum{"Annt", "AnCr"}},
		{"bounds", &desc{class: "bounds", items: []kv{{"Left", UnitFloat{"#Pnt", -1}}, {"Top ", UnitFloat{"#Pnt", -10}}, {"Rght", UnitFloat{"#Pnt", 100}}, {"Btom", UnitFloat{"#Pnt", 5}}}}},
		{"boundingBox", &desc{class: "boundingBox", items: []kv{{"Left", UnitFloat{"#Pnt", 0}}, {"Top ", UnitFloat{"#Pnt", -9}}, {"Rght", UnitFloat{"#Pnt", 98}}, {"Btom", UnitFloat{"#Pnt", 4}}}}},
		{"EngineData", RawData(engine)},
	}})
	b.u16(1)
	b.u32(16)
	b.descriptor(&desc{class: "warp", items: []kv{{"warpStyle", Enum{"warpStyle", "warpNone"}}}})
	for range 4 {
		b.i32(0)
	}
	return b.Bytes()
}

func TestText(t *testing.T) {
	r := image.Rect(1, 1, 3, 2)
	d := testDoc{w: 4, h: 2, merged: [][]byte{make([]byte, 8), make([]byte, 8), make([]byte, 8)}, layers: []testLayer{{
		name: "Rules", rect: r, opacity: 255, channels: rgbaChannels(r, 1),
		blocks: []testBlock{{"TySh", textBlock(engineFixture())}},
	}}}
	f := decodeDoc(t, d)
	l := f.Find("Rules")
	if l.Kind != KindText || l.Text == nil {
		t.Fatalf("kind %v text %v problems %v", l.Kind, l.Text, l.Problems)
	}
	tx := l.Text
	if tx.Transform != [6]float64{2, 0, 0, 2, 10, 20} || tx.Value != "Hello\rWorld" {
		t.Errorf("transform %v value %q", tx.Transform, tx.Value)
	}
	if tx.Bounds != [4]float64{-1, -10, 100, 5} || tx.BoundingBox != [4]float64{0, -9, 98, 4} {
		t.Errorf("bounds %v %v", tx.Bounds, tx.BoundingBox)
	}
	if !tx.Box || tx.BoxBounds != [4]float64{0, -1.5, 200.5, 50} {
		t.Errorf("box %v %v", tx.Box, tx.BoxBounds)
	}
	if tx.Justification != "center" || tx.Orientation != "horizontal" || tx.AntiAlias != "AnCr" {
		t.Errorf("justification %q orientation %q anti alias %q", tx.Justification, tx.Orientation, tx.AntiAlias)
	}
	if tx.Warp == nil || tx.Engine == nil {
		t.Fatal("warp or engine missing")
	}
	if len(tx.Runs) != 2 {
		t.Fatalf("runs %+v", tx.Runs)
	}
	r0, r1 := tx.Runs[0], tx.Runs[1]
	if r0.Start != 0 || r0.Length != 6 || r0.Font != "Beleren)Bold" || r0.FontSize != 12.5 || r0.Leading != 11 || r0.Tracking != 20 || r0.FauxBold {
		t.Errorf("first run %+v", r0)
	}
	if r0.FillColor.R != 255 || r0.FillColor.G != 0 || r0.Alpha != 1 {
		t.Errorf("first run color %+v alpha %v", r0.FillColor, r0.Alpha)
	}
	if r1.Start != 6 || r1.Font != "MinionPro" || r1.FontSize != 9 || !r1.FauxBold {
		t.Errorf("second run inherits the normal style sheet: %+v", r1)
	}
	if got := strings.Join(tx.Fonts(), ","); got != "Beleren)Bold,MinionPro" {
		t.Errorf("fonts %q", got)
	}
	if p := tx.Paragraphs(); len(p) != 2 || p[1] != "World" {
		t.Errorf("paragraphs %q", p)
	}
}
