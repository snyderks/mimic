package proxyshop

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/odevine/mimic/engine/author"
	"github.com/odevine/mimic/engine/proxyshop/proxyshoptest"
	"github.com/odevine/mimic/engine/psd"
)

func openPSD(t *testing.T, data []byte) *psd.File {
	t.Helper()
	f, err := psd.Decode(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestConvertBuildsAndBundles(t *testing.T) {
	const w, h = 400, 300
	white := color.NRGBA{240, 238, 230, 255}
	blue := color.NRGBA{0, 110, 180, 255}
	frame := image.Rect(20, 20, 380, 280)
	solidLayer := func(c color.NRGBA) proxyshoptest.Layer {
		return proxyshoptest.Pixels(frame, func(x, y int) color.NRGBA {
			if x > 60 && x < 340 && y > 60 && y < 160 {
				return color.NRGBA{}
			}
			return c
		})
	}
	// The pair runs a smooth blend from white to blue across the middle
	pair := proxyshoptest.Pixels(frame, func(x, y int) color.NRGBA {
		if x > 60 && x < 340 && y > 60 && y < 160 {
			return color.NRGBA{}
		}
		g := math.Max(0, math.Min(1, ((float64(x)+0.5)/w-0.375)/0.25))
		g = g * g * (3 - 2*g)
		mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-g) + float64(b)*g)) }
		return color.NRGBA{mix(white.R, blue.R), mix(white.G, blue.G), mix(white.B, blue.B), 255}
	})
	doc := proxyshoptest.Write(w, h,
		proxyshoptest.Folder("Background", false,
			proxyshoptest.Named(pair, "WU", true),
			proxyshoptest.Named(solidLayer(blue), "U", true),
			proxyshoptest.Named(solidLayer(white), "W", true),
		),
		proxyshoptest.Named(proxyshoptest.Pixels(image.Rect(60, 60, 340, 160), func(x, y int) color.NRGBA { return color.NRGBA{A: 255} }), "Art Frame", true),
		proxyshoptest.Named(proxyshoptest.Pixels(image.Rect(0, 0, w, h), func(x, y int) color.NRGBA { return color.NRGBA{9, 9, 9, 255} }), "Layer 1", false),
	)
	open := func(name string) (*psd.File, error) { return openPSD(t, doc), nil }
	var steps []string
	out, an, err := Convert([]SourceFile{{Name: "normal.psd"}}, open, Options{
		ID: "test.normal", Name: "Test", Description: "A test",
		Progress: func(part string, done, total int) { steps = append(steps, part) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0] != "normal_background" || steps[1] != "" {
		t.Errorf("progress %v", steps)
	}
	if _, ok := out.Files["normal/background/wu.png"]; ok {
		t.Error("the pair image was kept though a blend draws it")
	}
	img, err := png.Decode(bytes.NewReader(out.Files["normal/background/u.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Size() != frame.Size() {
		t.Errorf("the variant is %v, want it cropped to %v", img.Bounds(), frame)
	}
	if got := color.NRGBAModel.Convert(img.At(5, 5)).(color.NRGBA); got != blue {
		t.Errorf("the blue variant has %v", got)
	}

	var tmpl struct {
		Parts map[string]struct {
			Origin map[string]int
			Blend  struct {
				Shape string
				Seams map[string][]struct{ Center, Width float64 }
			}
		}
	}
	if err := json.Unmarshal(out.Files["template.json"], &tmpl); err != nil {
		t.Fatal(err)
	}
	bg := tmpl.Parts["normal_background"]
	if bg.Origin["x"] != 20 || bg.Origin["y"] != 20 {
		t.Errorf("origin %v", bg.Origin)
	}
	if seam := bg.Blend.Seams["2"]; bg.Blend.Shape != "smooth" || len(seam) != 1 || math.Abs(seam[0].Center) > 1 || math.Abs(seam[0].Width-100) > 2 {
		t.Errorf("blend %+v", bg.Blend)
	}
	if an.Recipe.Parts.Get("normal_background").Render == nil {
		t.Error("the recipe does not render the background")
	}

	var bundle bytes.Buffer
	if err := out.Bundle(&bundle); err != nil {
		t.Fatal(err)
	}
	if _, ds := author.Build(out.FS(), author.Options{}); ds.HasErrors() {
		t.Errorf("the template does not build: %v", ds)
	}
	if bundle.Len() == 0 {
		t.Error("an empty bundle")
	}
}
