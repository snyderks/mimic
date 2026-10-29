package psd

import (
	"bytes"
	"strings"
	"testing"
)

// descriptorFixture writes, by hand, a descriptor holding every value type
func descriptorFixture() []byte {
	var b bw
	b.unicode("Name")
	b.key("null")
	b.u32(14)

	b.key("Objc")
	b.str("Objc")
	b.unicode("")
	b.key("Pnt ")
	b.u32(1)
	b.key("Hrzn")
	b.str("UntF")
	b.str("#Pxl")
	b.f64(12.5)

	b.key("list")
	b.str("VlLs")
	b.u32(2)
	b.str("long")
	b.i32(-3)
	b.str("TEXT")
	b.unicode("é")

	b.key("doub")
	b.str("doub")
	b.f64(0.25)

	b.key("bool")
	b.str("bool")
	b.u8(1)

	b.key("comp")
	b.str("comp")
	b.i64(1 << 40)

	b.key("enum")
	b.str("enum")
	b.key("BlnM")
	b.key("linearDodge")

	b.key("tdta")
	b.str("tdta")
	b.u32(3)
	b.str("abc")

	b.key("type")
	b.str("type")
	b.unicode("cls")
	b.key("Clr ")

	b.key("GlbC")
	b.str("GlbC")
	b.unicode("")
	b.key("RGBC")

	b.key("alis")
	b.str("alis")
	b.u32(2)
	b.str("zz")

	b.key("keyOriginShapeBBox")
	b.str("GlbO")
	b.unicode("")
	b.key("unitRect")
	b.u32(0)

	b.key("Pts ")
	b.str("ObAr")
	b.u32(2)
	b.unicode("")
	b.key("rationalPoint")
	b.u32(1)
	b.key("Hrzn")
	b.str("UnFl")
	b.str("#Pxl")
	b.u32(2)
	b.f64(1)
	b.f64(2)

	b.key("null")
	b.str("obj ")
	b.u32(6)
	b.str("prop")
	b.unicode("")
	b.key("Lyr ")
	b.key("Nm  ")
	b.str("Enmr")
	b.unicode("")
	b.key("Lyr ")
	b.key("Ordn")
	b.key("Trgt")
	b.str("Idnt")
	b.i32(42)
	b.str("indx")
	b.i32(3)
	b.str("name")
	b.unicode("")
	b.key("Lyr ")
	b.unicode("Layer 1")
	b.str("rele")
	b.unicode("")
	b.key("Lyr ")
	b.i32(-1)

	b.key("Pth ")
	b.str("Pth ")
	b.u32(1)
	b.u8(7)
	return b.Bytes()
}

func TestParseDescriptor(t *testing.T) {
	d, err := ParseDescriptor(descriptorFixture())
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Name" || d.Class != "null" || len(d.Items) != 14 {
		t.Fatalf("descriptor %q %q %d items", d.Name, d.Class, len(d.Items))
	}
	if u, ok := d.Path("Objc").Unit("Hrzn"); !ok || u.Unit != "#Pxl" || u.Value != 12.5 {
		t.Errorf("unit float %+v", u)
	}
	l := d.List("list")
	if len(l) != 2 || l[0] != int32(-3) || l[1] != "é" {
		t.Errorf("list %#v", l)
	}
	if f, _ := d.Float("doub"); f != 0.25 {
		t.Errorf("doub %v", f)
	}
	if b, _ := d.Bool("bool"); !b {
		t.Error("bool")
	}
	if v, _ := d.Get("comp"); v != int64(1<<40) {
		t.Errorf("comp %v", v)
	}
	if e, _ := d.Enum("enum"); e != "linearDodge" {
		t.Errorf("enum %q", e)
	}
	if r := d.Raw("tdta"); string(r) != "abc" {
		t.Errorf("tdta %q", r)
	}
	if v, _ := d.Get("type"); v != (Class{Name: "cls", ID: "Clr "}) {
		t.Errorf("type %#v", v)
	}
	if v, _ := d.Get("alis"); !bytes.Equal(v.(RawData), []byte("zz")) {
		t.Errorf("alis %#v", v)
	}
	if g := d.Desc("keyOriginShapeBBox"); g == nil || g.Class != "unitRect" {
		t.Errorf("long key global object %+v", g)
	}
	v, _ := d.Get("Pts ")
	oa, ok := v.(*ObjectArray)
	if !ok || oa.Count != 2 || oa.Class != "rationalPoint" {
		t.Fatalf("object array %#v", v)
	}
	if u, _ := oa.Get("Hrzn"); u.(UnitFloats).Unit != "#Pxl" || len(u.(UnitFloats).Values) != 2 || u.(UnitFloats).Values[1] != 2 {
		t.Errorf("unit floats %#v", u)
	}
	ref, _ := d.Get("null")
	r := ref.(Reference)
	if len(r) != 6 || r[0].Form != "prop" || r[0].Value != "Nm  " || r[1].Value != (Enum{"Ordn", "Trgt"}) ||
		r[2].Value != int32(42) || r[3].Value != int32(3) || r[4].Value != "Layer 1" || r[5].Value != int32(-1) {
		t.Errorf("reference %#v", r)
	}
	if v, _ := d.Get("Pth "); !bytes.Equal(v.(RawData), []byte{7}) {
		t.Errorf("path %#v", v)
	}
	if !strings.Contains(d.Dump(), `"linearDodge"`) && !strings.Contains(d.Dump(), "linearDodge") {
		t.Errorf("dump %s", d.Dump())
	}
}

func TestParseDescriptorErrors(t *testing.T) {
	full := descriptorFixture()
	for _, n := range []int{0, 3, 9, 20, 60, len(full) - 1} {
		if _, err := ParseDescriptor(full[:n]); err == nil {
			t.Errorf("descriptor cut to %d bytes should fail", n)
		}
	}
	var b bw
	b.unicode("")
	b.key("null")
	b.u32(1)
	b.key("bad ")
	b.str("XXXX")
	if _, err := ParseDescriptor(b.Bytes()); err == nil || !strings.Contains(err.Error(), "XXXX") {
		t.Errorf("unknown type error %v", err)
	}
}

func TestDescriptorHelpersOnMissing(t *testing.T) {
	var d *Descriptor
	if d.Has("x") || d.Desc("x") != nil || d.FloatOr("x", 3) != 3 || d.BoolOr("x", true) != true {
		t.Error("a nil descriptor answers with defaults")
	}
}

func TestColors(t *testing.T) {
	cases := []struct {
		d       *desc
		model   string
		r, g, b float64
	}{
		{rgb(1, 2, 3), "rgb", 1, 2, 3},
		{&desc{class: "RGBC", items: []kv{{"redFloat", 1.0}, {"greenFloat", 0.5}, {"blueFloat", 0.0}}}, "rgb", 255, 127.5, 0},
		{&desc{class: "Grsc", items: []kv{{"Gry ", 25.0}}}, "gray", 191.25, 191.25, 191.25},
		{&desc{class: "HSBC", items: []kv{{"H   ", ang(120)}, {"Strt", 100.0}, {"Brgh", 100.0}}}, "hsb", 0, 255, 0},
		{&desc{class: "CMYC", items: []kv{{"Cyn ", 100.0}, {"Mgnt", 0.0}, {"Ylw ", 0.0}, {"Blck", 0.0}}}, "cmyk", 0, 255, 255},
		{&desc{class: "LbCl", items: []kv{{"Lmnc", 100.0}, {"A   ", 0.0}, {"B   ", 0.0}}}, "lab", 255, 255, 255},
	}
	for _, c := range cases {
		var b bw
		b.descriptor(c.d)
		d, err := ParseDescriptor(b.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		got := parseColor(d)
		near := func(a, b float64) bool { return a-b < 0.6 && b-a < 0.6 }
		if got.Model != c.model || !near(got.R, c.r) || !near(got.G, c.g) || !near(got.B, c.b) {
			t.Errorf("%s: %+v", c.d.class, got)
		}
	}
	var b bw
	b.descriptor(&desc{class: "BkCl", items: []kv{{"Bk  ", "PANTONE"}, {"Nm  ", "185 C"}}})
	d, _ := ParseDescriptor(b.Bytes())
	if c := parseColor(d); c.Model != "book" || c.Book != "PANTONE" || c.Name != "185 C" {
		t.Errorf("book color %+v", c)
	}
}

func TestDescriptorDepthLimit(t *testing.T) {
	var b bw
	for range maxDescriptorDepth + 2 {
		b.unicode("")
		b.key("null")
		b.u32(1)
		b.key("next")
		b.str("Objc")
	}
	b.unicode("")
	b.key("null")
	b.u32(0)
	if _, err := ParseDescriptor(b.Bytes()); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Errorf("deep nesting %v", err)
	}
}
