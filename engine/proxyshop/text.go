package proxyshop

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/odevine/mimic/engine/psd"
)

// mustRaw marshals a value this package built, which cannot fail
func mustRaw(v any) json.RawMessage {
	raw, err := marshalRaw(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// obj builds an ordered object from keys and values in turn
func obj(kv ...any) Object {
	o := Object{}
	for i := 0; i+1 < len(kv); i += 2 {
		o = append(o, Member{Key: kv[i].(string), Value: mustRaw(kv[i+1])})
	}
	return o
}

// textLayer is a type layer placed in the document
type textLayer struct {
	l *psd.Layer
	t *psd.Text
	// x and y are where the text's origin lands, the baseline of its first
	// line for point text
	x, y float64
	// size is the first run's font size in document pixels
	size float64
	run  psd.TextRun
}

func readText(l *psd.Layer) *textLayer {
	if l == nil || l.Text == nil || len(l.Text.Runs) == 0 {
		return nil
	}
	t := l.Text
	scale := math.Hypot(t.Transform[2], t.Transform[3])
	if scale == 0 {
		scale = 1
	}
	return &textLayer{l: l, t: t, x: t.Transform[4], y: t.Transform[5], size: t.Runs[0].FontSize * scale, run: t.Runs[0]}
}

// findText finds a type layer by name anywhere under g
func findText(g *psd.Layer, names ...string) *textLayer {
	if g == nil {
		return nil
	}
	for _, n := range names {
		var hit *psd.Layer
		var walk func(*psd.Layer)
		walk = func(l *psd.Layer) {
			for _, c := range l.Children {
				if hit != nil {
					return
				}
				if c.Name == n && c.Text != nil {
					hit = c
					return
				}
				if c.IsGroup() {
					walk(c)
				}
			}
		}
		walk(g)
		if t := readText(hit); t != nil {
			return t
		}
	}
	return nil
}

// findRef finds a reference layer by name anywhere under g
func findRef(g *psd.Layer, names ...string) image.Rectangle {
	if g == nil {
		return image.Rectangle{}
	}
	for _, n := range names {
		var hit *psd.Layer
		var walk func(*psd.Layer)
		walk = func(l *psd.Layer) {
			for _, c := range l.Children {
				if hit != nil {
					return
				}
				if c.Name == n {
					hit = c
					return
				}
				if c.IsGroup() {
					walk(c)
				}
			}
		}
		walk(g)
		if hit != nil {
			if r := referenceRect(hit); !r.Empty() {
				return r
			}
		}
	}
	return image.Rectangle{}
}

// fontRole is the template's font role for a Proxyshop font
func fontRole(font string) string {
	f := strings.ToLower(font)
	switch {
	case strings.Contains(f, "smallcaps") || strings.Contains(f, "small caps") || strings.Contains(f, "proxyglyph"):
		return "small-caps"
	case strings.Contains(f, "beleren"):
		return "title"
	case strings.Contains(f, "gotham") || strings.Contains(f, "relay"):
		return "info"
	case strings.Contains(f, "plantin") && strings.Contains(f, "italic"):
		return "body-italic"
	}
	return ""
}

// bundledPlantin reports whether font is a Plantin style the template already
// has bundled for its body role, roman or italic. A bold Plantin, such as
// "PlantinMTPro-Bold", is not bundled, so a box set in it falls back to the
// same unbolded body font a roman Plantin box does, silently losing the
// weight a real card prints it with unless this is flagged like any other
// unavailable font
func bundledPlantin(font string) bool {
	f := strings.ToLower(font)
	return strings.Contains(f, "plantin") && !strings.Contains(f, "bold")
}

func hexColor(c psd.Color) string {
	to := func(v float64) int { return int(math.Round(math.Max(0, math.Min(255, v)))) }
	return fmt.Sprintf("#%02X%02X%02X", to(c.R), to(c.G), to(c.B))
}

func align(j string) string {
	switch {
	case strings.HasSuffix(j, "right"):
		return "right"
	case strings.HasSuffix(j, "center"):
		return "center"
	}
	return "left"
}

// style is the text style of a type layer, at its own size
func (t *textLayer) style(a *analyzer, src string, extra ...any) Object {
	o := obj("fontSize", math.Round(t.size))
	if role := fontRole(t.run.Font); role != "" {
		o = append(o, obj("font", role)...)
	} else if !bundledPlantin(t.run.Font) && t.run.Font != "" {
		a.note("%s: %q is set in %s, which the template draws in its body font", src, t.l.Name, t.run.Font)
	}
	o = append(o, obj("color", hexColor(t.run.FillColor), "align", align(t.t.Justification), "vAlign", "baseline")...)
	if t.run.Tracking != 0 {
		o = append(o, obj("tracking", t.run.Tracking)...)
	}
	if sh := t.shadow(); sh != nil {
		o = append(o, obj("shadow", sh)...)
	}
	return append(o, obj(extra...)...)
}

// round4 rounds to four decimal places, enough precision for a fraction of a
// font size without the noise of a float's full expansion
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// shadow is the layer's own active drop shadow, in the form a text box's style
// carries: Distance as a fraction of the layer's own font size, independent of
// any scale the converter applies elsewhere, and Angle converted from
// Photoshop's convention (degrees counterclockwise from the positive x axis)
// to the engine's (clockwise from straight down). It is nil when the layer
// carries no active drop shadow
func (t *textLayer) shadow() Object {
	if t.l.Effects == nil || !t.l.Effects.Visible || t.run.FontSize == 0 {
		return nil
	}
	for _, s := range t.l.Effects.DropShadows {
		if !s.Active() {
			continue
		}
		angle := math.Mod(90-s.Angle, 360)
		if angle < 0 {
			angle += 360
		}
		o := obj("distance", round4(s.Distance/t.run.FontSize), "angle", round4(angle))
		if s.Opacity > 0 && s.Opacity < 1 {
			o = append(o, obj("opacity", round4(s.Opacity))...)
		}
		if c := hexColor(s.Color); c != "#000000" {
			o = append(o, obj("color", c)...)
		}
		return o
	}
	return nil
}

// artistPrefix is literal text Proxyshop bakes in before the artist credit's
// own placeholder name, such as an old-bordered frame's "Illus. ". It is empty
// when the layer carries no such text, including the one-character leading
// run normal templates use for the nib icon, which the engine already draws
// in code rather than as literal text
func artistPrefix(t *textLayer) string {
	const placeholder = "Artist"
	v := strings.TrimRight(t.t.Value, "\r\n")
	if !strings.HasSuffix(v, placeholder) {
		return ""
	}
	prefix := strings.TrimSuffix(v, placeholder)
	if len([]rune(prefix)) <= 1 {
		return ""
	}
	return prefix
}

// baselineBox is a box whose top is a point text's baseline, width wide, placed
// across from the text's origin by its alignment
func (t *textLayer) baselineBox(width int) Object {
	x := t.x
	switch align(t.t.Justification) {
	case "right":
		x -= float64(width)
	case "center":
		x -= float64(width) / 2
	}
	h := int(math.Round(t.size * 1.3))
	return obj("x", int(math.Round(x)), "y", int(math.Round(t.y)), "width", width, "height", h)
}

func rectObj(r image.Rectangle) Object {
	return obj("x", r.Min.X, "y", r.Min.Y, "width", r.Dx(), "height", r.Dy())
}

// textBoxes lays out the text the way the PSD's type layers and references do
func (a *analyzer) textBoxes(st *layoutState) (Object, error) {
	return a.textBoxesIn(st, st.f.Root.Find("Text and Icons"))
}

// textBoxesIn lays out the text of the type layers in the group ti, which is
// a group of its own in a PSD that keeps one per frame size
func (a *analyzer) textBoxesIn(st *layoutState, ti *psd.Layer) (Object, error) {
	f := st.f
	src := st.src.Name
	legal := f.Root.Find("Legal")
	boxes := Object{}
	add := func(key string, box Object) { boxes = append(boxes, Member{Key: key, Value: mustRaw(box)}) }
	textbox := findRef(ti, "Textbox Reference", "Textbox Reference Base")
	ptRef := findRef(ti, "PT Reference", "PT Box Reference")
	expRef := findRef(ti, "Expansion Reference")
	if expRef.Empty() {
		expRef = findRef(f.Root, "Expansion Reference")
	}
	right := f.Width - f.Width*9/100
	if !textbox.Empty() {
		right = textbox.Max.X
	}
	hasPT := st.ids["pt_box"]

	mana := findText(ti, "Mana Cost")
	if mana != nil {
		w := mana.l.Rect.Dx()
		if w <= 0 {
			w = f.Width / 4
		}
		st := obj("fontSize", math.Round(mana.size), "color", hexColor(mana.run.FillColor), "align", align(mana.t.Justification), "vAlign", "baseline",
			"minFontSize", math.Round(mana.size), "shadow", obj("distance", 0.0907, "angle", 25))
		add("mana", obj("rect", mana.baselineBox(w), "text", st, "flow", Object{}))
	}
	title := findText(ti, "Card Name")
	if shift := findText(ti, "Card Name Shift"); shift != nil && (title == nil || !title.l.Visible && shift.l.Visible) {
		title = shift
	}
	if title != nil {
		w := right - int(title.x)
		flow := Object{}
		if mana != nil {
			w = int(mana.x) - int(title.x)
			flow = obj("clearOf", "mana")
		}
		add("title", obj("rect", title.baselineBox(w), "text", title.style(a, src), "flow", flow))
	} else {
		a.note("%s: no Card Name layer, so the layout has no title", src)
	}
	if tl := findText(ti, "Typeline"); tl != nil {
		w := right - int(math.Round(tl.x))
		box := obj("rect", tl.baselineBox(w), "text", tl.style(a, src), "flow", Object{}, "draws", "type")
		var when []Object
		symW := w
		if !expRef.Empty() {
			symLeft := expRef.Max.X - 2*expRef.Dy()
			symW = symLeft - int(math.Round(tl.x)) - 30*f.Width/3264
			when = append(when, obj("condition", "set_symbol", "rect", obj("width", symW)))
		}
		if shift := findText(ti, "Typeline Shift"); shift != nil {
			sx := int(math.Round(shift.x))
			dx := sx - int(math.Round(tl.x))
			when = append(when, obj("condition", "color_indicator", "rect", obj("x", sx, "width", w-dx)))
			if symW != w {
				when = append(when, obj("condition", "color_indicator,set_symbol", "rect", obj("x", sx, "width", symW-dx)))
			}
		}
		if len(when) > 0 {
			box = append(box, obj("when", when)...)
		}
		add("type_full", box)
	}
	rules := findText(ti, "Rules Text - Noncreature", "Rules Text - Creature", "Rules Text")
	if rules != nil && !textbox.Empty() {
		ts := obj("fontSize", math.Round(rules.size), "color", hexColor(rules.run.FillColor), "align", "left", "vAlign", "center")
		if !rules.run.AutoLeading && rules.run.Leading > 0 {
			ts = append(ts, obj("lineSpacing", math.Round(rules.run.Leading/rules.run.FontSize*100)/100)...)
		}
		ts = append(ts, obj("paddingX", 40*f.Width/3264, "paddingY", 40*f.Width/3264)...)
		flow := Object{}
		if hasPT {
			flow = append(flow, obj("avoidLayer", "pt_box")...)
		}
		if st.divider != "" {
			flow = append(flow, obj("divider", st.divider)...)
		}
		add("oracle", obj("rect", rectObj(textbox), "text", ts, "flow", flow))
	} else if rules == nil {
		a.note("%s: no rules text layer, so the layout has no rules text", src)
	}
	if pt := findText(ti, "Power / Toughness"); pt != nil {
		w := ptRef.Dx()
		if w <= 0 {
			w = pt.l.Rect.Dx() * 2
		}
		add("pt", obj("rect", pt.baselineBox(w), "text", pt.style(a, src), "flow", Object{}))
	}
	if fp := findText(ti, "Flipside Power / Toughness"); fp != nil {
		w := fp.l.Rect.Dx() * 2
		add("flipside_pt", obj("rect", fp.baselineBox(w), "text", fp.style(a, src), "flow", Object{}))
	}

	// The legal lines follow Proxyshop's collector layout: number and rarity on
	// top, and the set, language and artist below
	top := findText(legal, "Top")
	bottom := findText(legal, "Bottom")
	artist := findText(legal, "Artist")
	set := findText(legal, "Set")
	collector := findText(legal, "Collector")
	legalW := right - f.Width/10
	if artist != nil && align(artist.t.Justification) == "center" {
		a.centeredLegal(add, artist, top, bottom, set, collector, legalW, src)
		return boxes, nil
	}
	if top != nil {
		add("collector", obj("rect", top.baselineBox(legalW), "text", top.style(a, src), "flow", Object{}))
	}
	row := set
	if bottom != nil {
		row = bottom
	}
	if row != nil {
		add("set", obj("rect", row.baselineBox(legalW), "text", set.orStyle(a, src, row), "flow", Object{}))
	}
	if artist != nil {
		ab := artist
		x := artist.x
		y := artist.y
		if row != nil && top != nil {
			// The artist sits after the set code on the lower row
			x = row.x + 401*row.size/68
			y = row.y
		}
		// Proxyglyph's capitals stand taller than the small caps role's
		size := math.Round(ab.size * 0.925)
		ts := obj("fontSize", size, "font", "small-caps", "color", hexColor(ab.run.FillColor), "align", "left", "vAlign", "baseline")
		add("artist", obj("rect", obj("x", int(math.Round(x)), "y", int(math.Round(y)), "width", legalW, "height", int(size*1.5)), "text", ts, "flow", Object{}))
	}
	if top != nil {
		w := legalW * 6 / 10
		r := right + 21*f.Width/3264
		if !ptRef.Empty() {
			r = ptRef.Max.X - 3
		}
		ts := obj("fontSize", math.Round(top.size), "color", hexColor(top.run.FillColor), "align", "right", "vAlign", "baseline", "tracking", 40)
		flow := Object{}
		if hasPT && row != nil {
			flow = obj("duck", obj("layer", "pt_box", "row", "artist"))
		}
		add("copyright", obj("rect", obj("x", r-w, "y", int(math.Round(top.y)), "width", w, "height", int(math.Round(top.size*1.75))), "text", ts, "flow", flow))
	}
	return boxes, nil
}

// centeredLegal sets the legal lines of a classic frame centered under the
// textbox: the artist on the upper line, and the set line below it. With the
// collector group, Proxyshop writes the collector number and the set as one
// centered line, which the boxes split at the middle. A classic frame prints
// no copyright
func (a *analyzer) centeredLegal(add func(string, Object), artist, top, bottom, set, collector *textLayer, w int, src string) {
	ts := artist.style(a, src)
	ts.Set("align", "center")
	if p := artistPrefix(artist); p != "" {
		ts.Set("prefix", p)
	}
	add("artist", obj("rect", artist.baselineBox(w), "text", ts, "flow", Object{}))
	if top == nil || bottom == nil {
		// An old-bordered card prints its own number here, in place of a
		// modern reprint's set code and language. Proxyshop toggles between a
		// hidden Collector layer and a visible Set layer for this; a PSD that
		// defines only Set still gets its position and style, just drawing
		// the collector vocabulary instead
		row := collector
		if row == nil {
			row = set
		}
		if row != nil {
			add("collector", obj("rect", row.baselineBox(w), "text", row.style(a, src), "flow", Object{}))
		}
		return
	}
	half := w / 2
	gap := bottom.size / 2
	h := int(math.Round(bottom.size * 1.3))
	y := int(math.Round(bottom.y))
	for _, b := range []struct {
		key, align string
		x          float64
	}{{"collector", "right", bottom.x - gap - float64(half)}, {"set", "left", bottom.x + gap}} {
		bs := bottom.style(a, src)
		bs.Set("align", b.align)
		add(b.key, obj("rect", obj("x", int(math.Round(b.x)), "y", y, "width", half, "height", h), "text", bs, "flow", Object{}))
	}
}

// orStyle is the style of t, or of fallback when t is missing
func (t *textLayer) orStyle(a *analyzer, src string, fallback *textLayer) Object {
	if t == nil {
		return fallback.style(a, src)
	}
	return t.style(a, src)
}
