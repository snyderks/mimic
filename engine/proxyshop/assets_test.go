package proxyshop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/odevine/mimic/engine/proxyshop/proxyshoptest"
	"github.com/odevine/mimic/engine/psdrender"
)

// TestBuildRenderedFallsBackToAnAssetVariant covers a transform icon group
// that only has some of the icons a card can need, the way pw-tf-front.psd
// lacks the plain triangle a card with no named frame effect defaults to. A
// key the PSD's own layers produce must win over its embedded fallback, and a
// key only the fallback covers must still be written, sized to match the
// PSD-rendered variants
func TestBuildRenderedFallsBackToAnAssetVariant(t *testing.T) {
	full := image.Rect(0, 0, 40, 40)
	doc := proxyshoptest.Write(40, 40,
		proxyshoptest.Named(proxyshoptest.Pixels(full, func(x, y int) color.NRGBA {
			return color.NRGBA{10, 200, 10, 255}
		}), "sunmoondfc", false),
	)
	f := openPSD(t, doc)
	r := &Render{
		Target:        []string{"sunmoondfc"},
		Variants:      Object{},
		AssetVariants: []string{"convertdfc", "upsidedowndfc"},
	}
	if err := r.Variants.Set("sunmoondfc", RenderVariant{}); err != nil {
		t.Fatal(err)
	}
	np := NamedPart{Name: "icon", Rule: PartRule{Dir: "icon", Render: r}}

	written := map[string][]byte{}
	put := func(path string, data []byte) { written[path] = data }
	note := func(string, ...any) {}
	if _, _, err := buildRendered(f, np, psdrender.New(f), put, note); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"sunmoondfc", "convertdfc", "upsidedowndfc"} {
		path := "icon/" + key + ".png"
		data, ok := written[path]
		if !ok {
			t.Errorf("no file written at %q", path)
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 40 {
			t.Errorf("%s: size %v, want 40x40 to match the PSD-rendered variant's crop", path, img.Bounds())
		}
	}

	// sunmoondfc came from the PSD's own layer, not its fallback: its pixels
	// are the solid color the layer was built from, not the embedded asset
	sunmoon, _ := png.Decode(bytes.NewReader(written["icon/sunmoondfc.png"]))
	r0, g0, b0, _ := sunmoon.At(20, 20).RGBA()
	if r0>>8 != 10 || g0>>8 != 200 || b0>>8 != 10 {
		t.Errorf("sunmoondfc pixel = %d,%d,%d, want the PSD layer's own color 10,200,10", r0>>8, g0>>8, b0>>8)
	}
}

func TestBuildRenderedRejectsAnUnknownAssetVariant(t *testing.T) {
	full := image.Rect(0, 0, 10, 10)
	doc := proxyshoptest.Write(10, 10,
		proxyshoptest.Named(proxyshoptest.Pixels(full, func(x, y int) color.NRGBA {
			return color.NRGBA{0, 0, 0, 255}
		}), "a", false),
	)
	f := openPSD(t, doc)
	r := &Render{Target: []string{"a"}, Variants: Object{}, AssetVariants: []string{"notarealicon"}}
	if err := r.Variants.Set("a", RenderVariant{}); err != nil {
		t.Fatal(err)
	}
	np := NamedPart{Name: "icon", Rule: PartRule{Dir: "icon", Render: r}}
	_, _, err := buildRendered(f, np, psdrender.New(f), func(string, []byte) {}, func(string, ...any) {})
	if err == nil {
		t.Fatal("want an error for an assetVariants key with no embedded fallback")
	}
}
