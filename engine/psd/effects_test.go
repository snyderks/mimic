package psd

import (
	"image"
	"math"
	"testing"
)

// legacyColor writes an lrFX color: RGB space and four 16 bit components
func legacyColor(b *bw, r, g, bl uint16) {
	b.u16(0)
	b.u16(r)
	b.u16(g)
	b.u16(bl)
	b.u16(0)
}

func fixed16(v float64) int32 { return int32(math.Round(v * 65536)) }

// lrFXBlock writes an lrFX block in the version 2 layout Photoshop writes
func lrFXBlock(visible bool) []byte {
	var out bw
	entry := func(key string, body func(*bw)) {
		var b bw
		body(&b)
		out.str("8BIM")
		out.str(key)
		out.u32(uint32(b.Len()))
		out.Write(b.Bytes())
	}
	out.u16(0)
	out.u16(6)
	entry("cmnS", func(b *bw) {
		b.u32(0)
		if visible {
			b.u8(1)
		} else {
			b.u8(0)
		}
		b.u16(0)
	})
	entry("dsdw", func(b *bw) {
		b.u32(2)
		b.i32(fixed16(12))
		b.i32(fixed16(3))
		b.i32(fixed16(90))
		b.i32(fixed16(7.5))
		legacyColor(b, 0x3b3c, 0x2828, 0x2a2a)
		b.str("8BIMmul ")
		b.u8(1)
		b.u8(0)
		b.u8(191)
		legacyColor(b, 0, 0, 0)
	})
	entry("isdw", func(b *bw) {
		b.u32(2)
		b.i32(fixed16(7))
		b.i32(0)
		b.i32(fixed16(45))
		b.i32(fixed16(3))
		legacyColor(b, 0, 0, 0)
		b.str("8BIMmul ")
		b.u8(0)
		b.u8(1)
		b.u8(89)
		legacyColor(b, 0, 0, 0)
	})
	entry("iglw", func(b *bw) {
		b.u32(2)
		b.i32(fixed16(250))
		b.i32(0)
		legacyColor(b, 0xffff, 0xffff, 0xffff)
		b.str("8BIMvLit")
		b.u8(1)
		b.u8(145)
		b.u8(0)
		legacyColor(b, 0xffff, 0xffff, 0xffff)
	})
	entry("bevl", func(b *bw) {
		b.u32(2)
		b.i32(fixed16(60))
		b.i32(fixed16(20))
		b.i32(fixed16(37))
		b.str("8BIMscrn")
		b.str("8BIMmul ")
		legacyColor(b, 0xffff, 0xffff, 0xffff)
		legacyColor(b, 0, 0, 0)
		b.u8(3)
		b.u8(230)
		b.u8(43)
		b.u8(1)
		b.u8(1)
		b.u8(1)
		legacyColor(b, 0xffff, 0xffff, 0xffff)
		legacyColor(b, 0, 0, 0)
	})
	entry("sofi", func(b *bw) {
		b.u32(2)
		b.str("8BIMnorm")
		legacyColor(b, 0x8181, 0x8181, 0x8181)
		b.u8(255)
		b.u8(1)
		legacyColor(b, 0x8181, 0x8181, 0x8181)
	})
	return out.Bytes()
}

func TestLegacyEffects(t *testing.T) {
	fx, err := ParseLegacyEffects(lrFXBlock(true), 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !fx.Visible || len(fx.List()) != 5 {
		t.Fatalf("visible %v, %d effects", fx.Visible, len(fx.List()))
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	ds := fx.DropShadows[0]
	if !ds.Active() || ds.Blend != BlendMultiply || !near(ds.Opacity, 191.0/255) || ds.Size != 12 || ds.Choke != 3 || ds.Angle != 90 || ds.Distance != 7.5 || ds.UseGlobalLight {
		t.Errorf("drop shadow %+v", ds)
	}
	if !near(ds.Color.R, float64(0x3b3c)/257) || !near(ds.Color.B, float64(0x2a2a)/257) || ds.Color.Model != "rgb" {
		t.Errorf("drop shadow color %+v", ds.Color)
	}
	is := fx.InnerShadows[0]
	if is.Enabled || !is.UseGlobalLight || is.Angle != 120 || is.Distance != 3 {
		t.Errorf("inner shadow takes the global angle: %+v", is)
	}
	ig := fx.InnerGlows[0]
	if !ig.Enabled || ig.Source != "center" || ig.Size != 250 || ig.Blend != BlendVividLight || !near(ig.Opacity, 145.0/255) || ig.Color.G != 255 {
		t.Errorf("inner glow %+v", ig)
	}
	b := fx.Bevels[0]
	if b.Style != "emboss" || b.Size != 37 || b.Up || !b.UseGlobalLight || b.Angle != 120 || b.Altitude != 40 {
		t.Errorf("bevel %+v", b)
	}
	if b.HighlightBlend != BlendScreen || b.ShadowBlend != BlendMultiply || !near(b.HighlightOpacity, 230.0/255) || !near(b.ShadowOpacity, 43.0/255) || b.HighlightColor.B != 255 {
		t.Errorf("bevel shading %+v", b)
	}
	co := fx.ColorOverlays[0]
	if !co.Enabled || co.Opacity != 1 || co.Color.R != 129 || co.Blend != BlendNormal {
		t.Errorf("color overlay %+v", co)
	}
	off, err := ParseLegacyEffects(lrFXBlock(false), 30, 30)
	if err != nil || off.Visible {
		t.Errorf("hidden effects %v %v", off, err)
	}
	if _, err := ParseLegacyEffects(lrFXBlock(true)[:40], 30, 30); err == nil {
		t.Error("a cut block should fail")
	}
}

// TestLegacyEffectsFallback reads lrFX only for a layer without lfx2
func TestLegacyEffectsFallback(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	modern := descBlock([]uint32{0, 16}, &desc{class: "null", items: []kv{
		{"Scl ", pct(100)},
		{"masterFXSwitch", true},
		{"FrFX", &desc{class: "FrFX", items: []kv{{"enab", true}, {"present", true}, {"Sz  ", px(2)}}}},
	}})
	d := testDoc{w: 2, h: 2, merged: [][]byte{make([]byte, 4), make([]byte, 4), make([]byte, 4)}, resources: map[int][]byte{1037: u32b(75)}, layers: []testLayer{
		{name: "Old", rect: r, opacity: 255, channels: rgbaChannels(r, 1), blocks: []testBlock{{"lrFX", lrFXBlock(true)}}},
		{name: "Both", rect: r, opacity: 255, channels: rgbaChannels(r, 1), blocks: []testBlock{{"lrFX", lrFXBlock(true)}, {"lfx2", modern}}},
	}}
	f := decodeDoc(t, d)
	old := f.Find("Old").Effects
	if old == nil || len(old.DropShadows) != 1 || old.InnerShadows[0].Angle != 75 || old.Desc != nil {
		t.Errorf("legacy effects %+v", old)
	}
	both := f.Find("Both").Effects
	if both == nil || len(both.Strokes) != 1 || len(both.DropShadows) != 0 {
		t.Errorf("lfx2 wins over lrFX: %+v", both)
	}
}
