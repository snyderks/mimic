package proxyshop

import (
	"image"
	"math"

	"github.com/odevine/mimic/engine/psd"
)

// A split template holds the two halves of the card in root groups Left and
// Right, and the textures both halves use in hidden root groups that Proxyshop
// copies into each half at render time. The conversion cuts each texture once
// at the left half and draws it again over the right half, the way the M15
// template's split layout does

func isSplit(f *psd.File) bool {
	return f.Root.Find("Left", "Text and Icons") != nil && f.Root.Find("Right", "Text and Icons") != nil
}

// splitStops are where Proxyshop's pinline gradients change color on a split
// card, as fractions of the whole document's width: one pair for each half,
// and the fuse bar's for two, three and four colors
var (
	splitLeftStops = [2]float64{0.28, 0.33}
	fuseStops      = map[int][]float64{2: {0.50, 0.54}, 3: {0.28, 0.33, 0.71, 0.76}, 4: {0.28, 0.33, 0.50, 0.54, 0.71, 0.76}}
)

// seamsOf turns gradient stops into a blend's seams, a pair of stops per seam
func seamsOf(stops []float64, docW int) []Object {
	w := float64(docW)
	var out []Object
	for i := 0; i+1 < len(stops); i += 2 {
		center := math.Round(((stops[i]+stops[i+1])/2-0.5)*w*10) / 10
		width := math.Round((stops[i+1]-stops[i])*w*10) / 10
		out = append(out, obj("center", center, "width", width))
	}
	return out
}

// colorVariants are the variants of a group with a layer per color, each
// showing its own layer and hiding the others
func colorVariants(colors []*psd.Layer) (Object, error) {
	byKey := map[string]*psd.Layer{}
	var keys []string
	for i := len(colors) - 1; i >= 0; i-- {
		key, suffix, _ := variantKey(colors[i].Name)
		if suffix != "" || byKey[key] != nil {
			continue
		}
		byKey[key] = colors[i]
		keys = append(keys, key)
	}
	sortKeys(keys)
	vs := Object{}
	for _, k := range keys {
		v := RenderVariant{Show: [][]string{byKey[k].Path()}}
		for _, c := range colors {
			if c != byKey[k] && c.Visible {
				v.Hide = append(v.Hide, c.Path())
			}
		}
		if err := vs.Set(k, v); err != nil {
			return nil, err
		}
	}
	return vs, nil
}

// fillVariants are the variants of a group Proxyshop fills: a solid color per
// single color, and when pairs is set, a gradient per pair with its colors
// changing between the stops
func fillVariants(into *psd.Layer, colors map[string]rgbColor, pairs bool, stops [2]float64) (Object, error) {
	var keys []string
	for k := range colors {
		if k != "vehicle" {
			keys = append(keys, k)
		}
	}
	if pairs {
		keys = append(keys, "wu", "ub", "br", "rg", "wg", "wb", "bg", "ug", "ur", "wr")
	}
	sortKeys(keys)
	vs := Object{}
	for _, k := range keys {
		fill := RenderFill{Into: into.Path(), Clip: true}
		if c, ok := colors[k]; ok {
			fill.Color = c.hex()
		} else {
			l, r := pairRun(k)
			fill.Gradient = &RenderGradient{Stops: []GradientStop{
				{Color: colors[l].hex(), Location: stops[0]},
				{Color: colors[r].hex(), Location: stops[1]},
			}}
		}
		if err := vs.Set(k, RenderVariant{Fills: []RenderFill{fill}}); err != nil {
			return nil, err
		}
	}
	return vs, nil
}

func (a *analyzer) split(st *layoutState) error {
	f := st.f
	src := st.src
	left, right := f.Root.Find("Left"), f.Root.Find("Right")
	lref, rref := f.Root.Find("Left", "Background", "Reference"), f.Root.Find("Right", "Background", "Reference")
	if lref == nil || rref == nil {
		a.note("%s: the halves have no Background/Reference to measure them by, so the split layout is left out", src.Name)
		return nil
	}
	half := referenceRect(lref)
	shift := referenceRect(rref).Min.Sub(half.Min)
	crop := rectOf(half)

	// half adds a part drawn over the left half and again over the right
	addHalf := func(l *psd.Layer, s, slot string, r *Render, blend Object) {
		name := a.partName(src.Key, s)
		rule := PartRule{Source: src.Key, Dir: src.Key + "/" + s, Slot: slot, Crop: crop, Render: r}
		if blend != nil {
			rule.Blend = mustRaw(blend)
		}
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: rule})
		at := half.Min.Add(shift)
		st.entries = append(st.entries,
			stackEntry{id: st.entryID(s + "_1"), part: name, face: "first"},
			stackEntry{id: st.entryID(s + "_2"), part: name, face: "second", at: &at})
	}
	textureHalf := func(name, s, slot string) error {
		g := f.Root.Find(name)
		if g == nil {
			return nil
		}
		colors := colorChildrenAtLeast(g, 1)
		if len(colors) == 0 {
			return nil
		}
		vs, err := colorVariants(colors)
		if err != nil {
			return err
		}
		r := &Render{Target: g.Path(), Show: [][]string{g.Path()}, Variants: vs, FitPairs: true}
		r.Show = append(r.Show, shapeShows(g, framePrefs("split"))...)
		addHalf(g, s, slot, r, nil)
		return nil
	}

	if err := textureHalf("Background", "background", "background"); err != nil {
		return err
	}
	if err := textureHalf("Textbox", "textbox", "pinlines"); err != nil {
		return err
	}
	if p := left.Find("Pinlines"); p != nil {
		vs, err := fillVariants(shapeHolder(p), pinlineColors, true, splitLeftStops)
		if err != nil {
			return err
		}
		r := &Render{Target: p.Path(), Variants: vs, FitPairs: true}
		if !p.Visible {
			r.Show = append(r.Show, p.Path())
		}
		addHalf(p, "pinlines", "pinlines", r, nil)
	}
	if err := textureHalf("Name & Title Boxes", "name_title_boxes", "twins"); err != nil {
		return err
	}
	if o := left.Find("Art Outline"); o != nil {
		r := &Render{Target: o.Path(), Show: [][]string{o.Path()}, Variants: anyVariant()}
		addHalf(o, "art_outline", "", r, nil)
	}

	if b := f.Root.Find("Border"); b != nil {
		a.addPart(st, b, "border", "", "", &Render{Target: b.Path(), Show: [][]string{b.Path()}, Variants: anyVariant()})
	}
	if b := f.Root.Find("Border Fuse"); b != nil {
		a.addPart(st, b, "border_fuse", "", "fuse", &Render{Target: b.Path(), Show: [][]string{b.Path()}, Variants: anyVariant()})
	}
	fuseBlend := obj("shape", "linear", "seams", obj(
		"2", seamsOf(fuseStops[2], f.Width), "3", seamsOf(fuseStops[3], f.Width), "4", seamsOf(fuseStops[4], f.Width)))
	if fuse := f.Root.Find("Fuse"); fuse != nil {
		if tb := fuse.Find("Textbox"); tb != nil {
			if colors := colorChildrenAtLeast(tb, 1); len(colors) > 0 {
				vs, err := colorVariants(colors)
				if err != nil {
					return err
				}
				r := &Render{Target: tb.Path(), Show: [][]string{fuse.Path(), tb.Path()}, Variants: vs}
				a.addPart(st, tb, "fuse_textbox", "fuse", "fuse", r)
				a.recipe.Parts[len(a.recipe.Parts)-1].Rule.Blend = mustRaw(fuseBlend)
			}
		}
		if p := fuse.Find("Pinlines"); p != nil {
			vs, err := fillVariants(shapeHolder(p), pinlineColors, false, splitLeftStops)
			if err != nil {
				return err
			}
			r := &Render{Target: p.Path(), Show: [][]string{fuse.Path(), p.Path()}, Variants: vs}
			a.addPart(st, p, "fuse_pinlines", "fuse_pinlines", "fuse", r)
			a.recipe.Parts[len(a.recipe.Parts)-1].Rule.Blend = mustRaw(fuseBlend)
		}
	}

	boxes := Object{}
	add := func(key string, box Object) { boxes = append(boxes, Member{Key: key, Value: mustRaw(box)}) }
	var art []Object
	for i, side := range []*psd.Layer{left, right} {
		n := []string{"1", "2"}[i]
		face := []string{"first", "second"}[i]
		ti := side.Find("Text and Icons")
		textbox := findRef(ti, "Textbox Reference")
		fuseRef := findRef(ti, "Textbox Reference Fuse")
		exp := findRef(ti, "Expansion Reference")
		mana := findText(ti, "Mana Cost")
		if mana != nil {
			ts := obj("fontSize", math.Round(mana.size), "color", hexColor(mana.run.FillColor), "align", align(mana.t.Justification), "vAlign", "baseline",
				"minFontSize", math.Round(mana.size), "shadow", obj("distance", 0.0907, "angle", 25))
			add("mana_"+n, obj("rect", mana.baselineBox(mana.l.Rect.Dx()), "text", ts, "flow", Object{}, "draws", "mana", "face", face))
		}
		if title := findText(ti, "Card Name"); title != nil {
			w := textbox.Max.X - int(title.x)
			flow := Object{}
			if mana != nil {
				flow = obj("clearOf", "mana_"+n)
			}
			add("title_"+n, obj("rect", title.baselineBox(w), "text", title.style(a, src.Name), "flow", flow, "draws", "title", "face", face))
		}
		if tl := findText(ti, "Typeline"); tl != nil {
			right := textbox.Max.X
			if !exp.Empty() {
				right = exp.Min.X - 25*f.Height/3264
			}
			add("type_"+n, obj("rect", tl.baselineBox(right-int(math.Round(tl.x))), "text", tl.style(a, src.Name), "flow", Object{}, "draws", "type", "face", face))
		}
		if rules := findText(ti, "Rules Text"); rules != nil && !textbox.Empty() {
			pad := 28 * f.Height / 3264
			ts := obj("fontSize", math.Round(rules.size), "color", hexColor(rules.run.FillColor), "align", "left", "vAlign", "center")
			if !rules.run.AutoLeading && rules.run.Leading > 0 {
				ts = append(ts, obj("lineSpacing", math.Round(rules.run.Leading/rules.run.FontSize*100)/100)...)
			}
			ts = append(ts, obj("paddingX", pad, "paddingY", pad)...)
			box := obj("rect", rectObj(textbox), "text", ts, "flow", Object{}, "draws", "oracle", "face", face)
			if !fuseRef.Empty() {
				box = append(box, obj("when", []Object{obj("condition", "fuse", "rect", obj("height", fuseRef.Dy()))})...)
			}
			add("oracle_"+n, box)
		}
		if ref := side.Find("Art Frame"); ref != nil {
			r := referenceRect(ref)
			art = append(art, obj("x", r.Min.X, "y", r.Min.Y, "width", r.Dx(), "height", r.Dy(), "face", face))
		}
	}
	if ft := findText(f.Root.Find("Fuse"), "Fuse Text"); ft != nil {
		add("fuse", obj("rect", ft.baselineBox(f.Width-2*int(ft.x)), "text", ft.style(a, src.Name), "flow", Object{}, "condition", "fuse"))
	}
	a.legalOnTheSide(st, add)
	st.boxes = boxes
	st.art = art
	return nil
}

// legalOnTheSide places the legal lines of a frame authored on its side, whose
// type runs up the left edge, in the upright card the layout delivers
func (a *analyzer) legalOnTheSide(st *layoutState, add func(string, Object)) {
	f := st.f
	legal := f.Root.Find("Legal")
	upright := func(t *textLayer) image.Point {
		// The canvas turns a quarter counterclockwise, so a point's distance
		// from the left edge becomes its height and its distance from the
		// right edge its depth
		return image.Pt(int(math.Round(t.y)), int(math.Round(float64(f.Width)-t.x)))
	}
	w := f.Height * 2 / 3
	lineAt := func(key string, p image.Point, h int, style Object) {
		add(key, obj("rect", obj("x", p.X, "y", p.Y, "width", w, "height", h), "text", style, "flow", Object{}, "space", "output"))
	}
	line := func(key string, t *textLayer, style Object) {
		if t == nil {
			return
		}
		lineAt(key, upright(t), int(math.Round(t.size*1.75)), style)
	}
	top, bottom := findText(legal, "Top"), findText(legal, "Bottom")
	artist, set := findText(legal, "Artist"), findText(legal, "Set")
	artistStyle := func(src *textLayer) Object {
		return obj("fontSize", math.Round(src.size*0.925), "font", "small-caps", "color", hexColor(src.run.FillColor), "align", "left", "vAlign", "baseline")
	}
	switch {
	case bottom != nil:
		// A Collector group splits the legal line in two: Top prints the
		// number and rarity alone, and Bottom prints the set code, language
		// and artist credit together on one row (its own literal text reads
		// "SET • EN aArtist"). The standalone Artist and Set layers are the
		// alternate layout Proxyshop draws when Collector is off, each on its
		// own row, so they stand in for Bottom's two halves rather than
		// drawing at their own authored position
		line("set", bottom, bottom.style(a, st.src.Name))
		if artist != nil {
			p := upright(bottom)
			// The artist credit opens where the set code and language leave
			// off, the same proportion of its own size that the row-based
			// legal line uses for the same join
			p.X += int(math.Round(401 * bottom.size / 68))
			lineAt("artist", p, int(math.Round(artist.size*1.75)), artistStyle(artist))
		}
	case set != nil:
		line("set", set, set.style(a, st.src.Name))
		if artist != nil {
			line("artist", artist, artistStyle(artist))
		}
	default:
		if artist != nil {
			line("artist", artist, artistStyle(artist))
		}
	}
	if top != nil {
		line("collector", top, top.style(a, st.src.Name))
	}
}
