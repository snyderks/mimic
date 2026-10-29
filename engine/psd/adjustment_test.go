package psd

import (
	"image"
	"testing"
)

func levelsBlock() []byte {
	var b bw
	b.u16(2)
	for i := range 29 {
		b.u16(uint16(i))
		b.u16(250)
		b.u16(5)
		b.u16(255)
		b.u16(120)
	}
	return b.Bytes()
}

func curvesBlock() []byte {
	var b bw
	b.u8(0)
	b.u16(1)
	b.u32(0b101)
	for range 2 {
		b.u16(2)
		b.u16(0)
		b.u16(0)
		b.u16(255)
		b.u16(255)
	}
	b.str("Crv ")
	b.u16(4)
	b.u32(1)
	b.u16(2)
	b.u16(3)
	for _, v := range []uint16{0, 0, 140, 128, 255, 255} {
		b.u16(v)
	}
	return b.Bytes()
}

func hueBlock() []byte {
	var b bw
	b.u16(2)
	b.u8(1)
	b.u8(0)
	for _, v := range []int16{30, 25, 0, -10, 20, 5} {
		b.i16(v)
	}
	for i := range 6 {
		for _, v := range []int16{int16(i), 10, 20, 30, 1, 2, 3} {
			b.i16(v)
		}
	}
	return b.Bytes()
}

func selectiveBlock() []byte {
	var b bw
	b.u16(1)
	b.u16(1)
	for i := range 10 {
		for j := range 4 {
			b.i16(int16(i*10 + j))
		}
	}
	return b.Bytes()
}

func TestAdjustments(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	adj := func(name, key string, data []byte) testLayer {
		return testLayer{name: name, rect: r, opacity: 255, channels: rgbaChannels(r, 1), blocks: []testBlock{{key, data}}}
	}
	var brit, blnc, post bw
	brit.i16(-20)
	brit.i16(35)
	brit.i16(127)
	brit.u8(0)
	for _, v := range []int16{1, 2, 3, 4, 5, 6, 7, 8, 9} {
		blnc.i16(v)
	}
	blnc.u8(1)
	post.u16(4)
	post.u16(0)
	cg := descBlock([]uint32{16}, &desc{class: "null", items: []kv{{"Vrsn", int32(1)}, {"Brgh", int32(15)}, {"Cntr", int32(-5)}, {"means", int32(127)}, {"Lab ", false}, {"useLegacy", true}}})
	d := testDoc{w: 2, h: 2, merged: [][]byte{make([]byte, 4), make([]byte, 4), make([]byte, 4)}, layers: []testLayer{
		adj("Levels", "levl", levelsBlock()),
		adj("Curves", "curv", curvesBlock()),
		adj("Hue", "hue2", hueBlock()),
		adj("Selective", "selc", selectiveBlock()),
		adj("Brit", "brit", brit.Bytes()),
		adj("CgEd", "CgEd", cg),
		adj("Balance", "blnc", blnc.Bytes()),
		adj("Posterize", "post", post.Bytes()),
		adj("Invert", "nvrt", nil),
	}}
	f := decodeDoc(t, d)
	for _, l := range f.Layers {
		if l.Kind != KindAdjustment || l.Adjustment == nil {
			t.Fatalf("%s: kind %v", l.Name, l.Kind)
		}
	}
	lv := f.Find("Levels").Adjustment
	if lv.Type != "levels" || len(lv.Levels) != 29 || lv.Levels[3] != (LevelsRecord{3, 250, 5, 255, 1.2}) {
		t.Errorf("levels %+v", lv.Levels[:4])
	}
	cv := f.Find("Curves").Adjustment
	if cv.Type != "curves" || len(cv.Curves) != 1 || cv.Curves[0].Channel != 2 || len(cv.Curves[0].Points) != 3 || cv.Curves[0].Points[1] != (Point{X: 128, Y: 140}) {
		t.Errorf("curves take the extension: %+v", cv.Curves)
	}
	hs := f.Find("Hue").Adjustment.HueSaturation
	if hs == nil || !hs.Colorize || hs.ColorizeHue != 30 || hs.Hue != -10 || hs.Saturation != 20 || hs.Ranges[5].Bounds[0] != 5 || hs.Ranges[2].Lightness != 3 {
		t.Errorf("hue saturation %+v", hs)
	}
	sc := f.Find("Selective").Adjustment.SelectiveColor
	if sc == nil || !sc.Absolute || sc.Colors[0] != [4]int{10, 11, 12, 13} || sc.Colors[8] != [4]int{90, 91, 92, 93} {
		t.Errorf("selective color %+v", sc)
	}
	if bc := f.Find("Brit").Adjustment.BrightnessContrast; bc == nil || bc.Brightness != -20 || bc.Contrast != 35 || !bc.Legacy {
		t.Errorf("brightness %+v", bc)
	}
	if bc := f.Find("CgEd").Adjustment; bc.Type != "brightnesscontrast" || bc.BrightnessContrast.Brightness != 15 || bc.BrightnessContrast.Contrast != -5 || !bc.BrightnessContrast.Legacy || bc.Desc == nil {
		t.Errorf("content generator %+v", bc.BrightnessContrast)
	}
	if cb := f.Find("Balance").Adjustment.ColorBalance; cb == nil || cb.Midtones != [3]int{4, 5, 6} || !cb.PreserveLuminosity {
		t.Errorf("color balance %+v", cb)
	}
	if p := f.Find("Posterize").Adjustment; p.Type != "posterize" || p.Posterize != 4 {
		t.Errorf("posterize %+v", p)
	}
	if f.Find("Invert").Adjustment.Type != "invert" {
		t.Error("invert")
	}
}

// TestLevelsExtension reads the records past 29 that a Lvls extension holds
func TestLevelsExtension(t *testing.T) {
	b := levelsBlock()
	var ext bw
	ext.str("Lvls")
	ext.u16(3)
	ext.u16(31)
	for i := range 2 {
		ext.u16(uint16(100 + i))
		ext.u16(200)
		ext.u16(0)
		ext.u16(255)
		ext.u16(100)
	}
	ext.u16(0)
	b = append(b, ext.Bytes()...)
	lv := parseLevels(newCursor(b))
	if len(lv) != 31 || lv[30] != (LevelsRecord{101, 200, 0, 255, 1}) || lv[28].InputFloor != 28 {
		t.Errorf("levels %d records, last %+v", len(lv), lv[len(lv)-1])
	}
	if lv := parseLevels(newCursor(levelsBlock())); len(lv) != 29 {
		t.Errorf("levels without the extension %d", len(lv))
	}
}
