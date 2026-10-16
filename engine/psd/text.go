package psd

import (
	"fmt"
	"strings"
)

// Text is a type layer's content and style, read from its TySh block
type Text struct {
	// Transform maps the text's own coordinates to the document as xx, xy, yx,
	// yy, tx and ty, so a point (x, y) lands at (xx*x + yx*y + tx, xy*x + yy*y + ty)
	Transform [6]float64
	// Value is the text with paragraphs separated by carriage returns
	Value string
	// Bounds and BoundingBox are the descriptor's bounds and boundingBox in the
	// text's own coordinates, as left, top, right and bottom
	Bounds      [4]float64
	BoundingBox [4]float64
	// Box is true for paragraph text, whose lines wrap inside BoxBounds, and
	// false for point text
	Box       bool
	BoxBounds [4]float64
	// Justification is that of the first paragraph: "left", "right", "center",
	// "justify-left", "justify-right", "justify-center" or "justify-all"
	Justification string
	// Runs split Value into spans of one style, in order
	Runs []TextRun
	// Orientation is "horizontal" or "vertical"
	Orientation string
	// AntiAlias is the descriptor's anti alias method key, such as "AnCr"
	AntiAlias string
	Desc      *Descriptor
	Warp      *Descriptor
	// Engine is the parsed EngineData, nil when the descriptor has none or it
	// did not parse
	Engine map[string]any
}

// TextRun is a span of text with one style. Sizes are in points in the text's
// own coordinates, before Transform scales them. Tracking is in thousandths of
// an em
type TextRun struct {
	Start, Length int
	Font          string
	FontSize      float64
	Leading       float64
	AutoLeading   bool
	Tracking      float64
	FauxBold      bool
	FauxItalic    bool
	// FillColor is from 0 to 255 per channel with Alpha from 0 to 1
	FillColor Color
	Alpha     float64
}

var justifications = []string{"left", "right", "center", "justify-left", "justify-right", "justify-center", "justify-all"}

func parseText(b []byte) (*Text, error) {
	c := newCursor(b)
	c.u16()
	t := &Text{}
	for i := range t.Transform {
		t.Transform[i] = c.f64()
	}
	c.u16()
	c.u32()
	if c.err != nil {
		return nil, fmt.Errorf("psd: text: %w", c.err)
	}
	t.Desc = readDescriptor(c, 0)
	if c.err != nil {
		return nil, fmt.Errorf("psd: text descriptor: %w", c.err)
	}
	c.u16()
	c.u32()
	if c.err == nil {
		w := readDescriptor(c, 0)
		if c.err == nil {
			t.Warp = w
		}
	}
	d := t.Desc
	t.Value, _ = d.String("Txt ")
	t.Bounds = rectFromDesc(d.Desc("bounds"))
	t.BoundingBox = rectFromDesc(d.Desc("boundingBox"))
	t.Orientation = "horizontal"
	if o, ok := d.Enum("Ornt"); ok && o == "Vrtc" {
		t.Orientation = "vertical"
	}
	t.AntiAlias, _ = d.Enum("AntA")
	if raw := d.Raw("EngineData"); raw != nil {
		if e, err := ParseEngineData(raw); err == nil {
			t.Engine = e
			t.fromEngine()
		}
	}
	return t, nil
}

func rectFromDesc(d *Descriptor) [4]float64 {
	if d == nil {
		return [4]float64{}
	}
	return [4]float64{d.FloatOr("Left", 0), d.FloatOr("Top ", 0), d.FloatOr("Rght", 0), d.FloatOr("Btom", 0)}
}

// fromEngine fills the runs, paragraph and box settings from the engine tree
func (t *Text) fromEngine() {
	e := t.Engine
	child := engineGet(e, "EngineDict", "Rendered", "Shapes", "Children", 0)
	if st, ok := engineInt(engineGet(child, "ShapeType")); ok {
		t.Box = st == 1
	}
	if bb, ok := engineGet(child, "Cookie", "Photoshop", "BoxBounds").([]any); ok && len(bb) == 4 {
		for i := range bb {
			t.BoxBounds[i], _ = engineFloat(bb[i])
		}
	}

	var fonts []string
	if fs, ok := engineGet(e, "ResourceDict", "FontSet").([]any); ok {
		for _, f := range fs {
			name, _ := engineGet(f, "Name").(string)
			fonts = append(fonts, name)
		}
	}
	defStyle := engineGet(e, "ResourceDict", "StyleSheetSet", engineIndex(e, "ResourceDict", "TheNormalStyleSheet"), "StyleSheetData")

	runs, _ := engineGet(e, "EngineDict", "StyleRun", "RunArray").([]any)
	lengths, _ := engineGet(e, "EngineDict", "StyleRun", "RunLengthArray").([]any)
	pos := 0
	for i, r := range runs {
		data := engineGet(r, "StyleSheet", "StyleSheetData")
		prop := func(k string) any {
			if v := engineGet(data, k); v != nil {
				return v
			}
			return engineGet(defStyle, k)
		}
		run := TextRun{Start: pos, Alpha: 1}
		if i < len(lengths) {
			run.Length, _ = engineInt(lengths[i])
		}
		pos += run.Length
		if fi, ok := engineInt(prop("Font")); ok && fi >= 0 && fi < len(fonts) {
			run.Font = fonts[fi]
		}
		run.FontSize, _ = engineFloat(prop("FontSize"))
		run.Leading, _ = engineFloat(prop("Leading"))
		run.AutoLeading, _ = prop("AutoLeading").(bool)
		run.Tracking, _ = engineFloat(prop("Tracking"))
		run.FauxBold, _ = prop("FauxBold").(bool)
		run.FauxItalic, _ = prop("FauxItalic").(bool)
		run.FillColor, run.Alpha = engineColor(prop("FillColor"))
		t.Runs = append(t.Runs, run)
	}

	defPara := engineGet(e, "ResourceDict", "ParagraphSheetSet", engineIndex(e, "ResourceDict", "TheNormalParagraphSheet"), "Properties")
	para := engineGet(e, "EngineDict", "ParagraphRun", "RunArray", 0, "ParagraphSheet", "Properties")
	j, ok := engineInt(engineGet(para, "Justification"))
	if !ok {
		j, ok = engineInt(engineGet(defPara, "Justification"))
	}
	t.Justification = "left"
	if ok && j >= 0 && j < len(justifications) {
		t.Justification = justifications[j]
	}
}

func engineIndex(e any, path ...any) int {
	i, _ := engineInt(engineGet(e, path...))
	return i
}

// engineColor reads a color written as a type and values, where type 1 is ARGB
// from 0 to 1
func engineColor(v any) (Color, float64) {
	vals, _ := engineGet(v, "Values").([]any)
	f := make([]float64, len(vals))
	for i, x := range vals {
		f[i], _ = engineFloat(x)
	}
	if len(f) == 4 {
		return Color{R: f[1] * 255, G: f[2] * 255, B: f[3] * 255, Model: "rgb"}, f[0]
	}
	if len(f) == 2 {
		g := f[1] * 255
		return Color{R: g, G: g, B: g, Model: "gray"}, f[0]
	}
	return Color{Model: "rgb"}, 1
}

// Fonts lists the distinct fonts the text's runs use, in order of first use
func (t *Text) Fonts() []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range t.Runs {
		if r.Font != "" && !seen[r.Font] {
			seen[r.Font] = true
			out = append(out, r.Font)
		}
	}
	return out
}

// Paragraphs splits Value at carriage returns
func (t *Text) Paragraphs() []string {
	return strings.Split(strings.TrimRight(t.Value, "\r"), "\r")
}
