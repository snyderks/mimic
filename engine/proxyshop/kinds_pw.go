package proxyshop

import (
	"encoding/json"
	"image"
	"math"
	"slices"

	"github.com/odevine/mimic/engine/psd"
)

// A planeswalker template keeps its frame in a root group per size, pw-3 for
// three abilities or fewer and pw-4 for more, or in one Planeswalker group, and
// the loyalty art and the sample ability text in Loyalty Graphics. Proxyshop
// shows the size group the card needs, writes each ability into a copy of the
// sample text with a copy of the badge for its cost beside it, and shades every
// other ability by knocking ragged bands out of a darker copy of the textbox.
// The conversion draws each size group on the rows condition and the abilities
// as a section whose items are the badges, the cost and the colon

func isPlaneswalker(f *psd.File) bool { return rootGroup(f, "Loyalty Graphics") != nil }

// pwGroup is one of a planeswalker template's frame groups and when it draws
type pwGroup struct {
	g            *psd.Layer
	prefix, cond string
}

func pwGroups(f *psd.File) []pwGroup {
	three, four := rootGroup(f, "pw-3"), rootGroup(f, "pw-4")
	switch {
	case three != nil && four != nil:
		return []pwGroup{{three, "pw3", "rows<4"}, {four, "pw4", "rows>3"}}
	case three != nil:
		return []pwGroup{{three, "pw3", ""}}
	}
	if g := rootGroup(f, "Planeswalker"); g != nil {
		return []pwGroup{{g, "", ""}}
	}
	return nil
}

func (a *analyzer) planeswalkerBefore(st *layoutState) error {
	for _, pg := range pwGroups(st.f) {
		st.claimed[pg.g] = true
	}
	st.claimed[rootGroup(st.f, "Loyalty Graphics")] = true
	return nil
}

func (a *analyzer) planeswalkerAfter(st *layoutState) error {
	f := st.f
	groups := pwGroups(f)
	if len(groups) == 0 {
		a.note("%s: Loyalty Graphics has no pw-3, pw-4 or Planeswalker group beside it, so the planeswalker frame is left out", st.src.Name)
		return nil
	}
	// The groups are read bottom first, so each takes its place in the stack
	ordered := append([]pwGroup(nil), groups...)
	if len(ordered) == 2 && rootIndex(f, ordered[0].g) > rootIndex(f, ordered[1].g) {
		ordered[0], ordered[1] = ordered[1], ordered[0]
	}
	var pwRoots []int
	for _, pg := range ordered {
		pwRoots = append(pwRoots, rootIndex(f, pg.g))
		start, firstPart := len(st.entries), len(a.recipe.Parts)
		for i := len(pg.g.Children) - 1; i >= 0; i-- {
			c := pg.g.Children[i]
			if c.Name == "Textbox" && c.IsGroup() && c.Find("Ability Dividers") != nil {
				// The darker copy is drawn per ability by the section's bands
				r := &Render{Target: c.Path(), Hide: [][]string{c.Find("Ability Dividers").Path()}, Variants: anyVariant()}
				a.addPart(st, c, "textbox", "", "", r)
				continue
			}
			if err := a.rootLayer(st, c); err != nil {
				return err
			}
		}
		a.placeEntries(st, pg.g, a.takeEntries(st, start, firstPart, pg.prefix, pg.cond))
	}
	a.pwTwinsByPinlines(st)

	lg := rootGroup(f, "Loyalty Graphics")
	// The sections' images draw above every frame group and below the loyalty
	// box, which the shading must not cover
	top := ""
	for _, e := range st.entries {
		if slices.Contains(pwRoots, rootIndex(f, a.entryRoot(st, e))) {
			top = e.id
		}
	}
	loyalty := ""
	if sl := lg.Find("Starting Loyalty"); sl != nil && sl.IsGroup() {
		r := &Render{Target: sl.Path(), Variants: anyVariant()}
		if !sl.Visible {
			r.Show = append(r.Show, sl.Path())
		}
		for _, c := range sl.Children {
			if c.Kind == psd.KindText && c.Visible {
				r.Hide = append(r.Hide, c.Path())
			}
		}
		start, firstPart := len(st.entries), len(a.recipe.Parts)
		a.addPart(st, sl, "loyalty_box", "", "loyalty", r)
		es := a.takeEntries(st, start, firstPart, "", "")
		loyalty = es[0].id
		a.placeEntries(st, lg, es)
		if t := findText(sl, "Text"); t != nil {
			w := sl.Find("Symbol").Rect.Dx()
			if w <= 0 {
				w = int(math.Round(t.size * 3))
			}
			st.moreBoxes.Set("loyalty", a.pointBox(st, t, w))
		}
	}

	badges := a.pwBadges(st, lg)
	edges := a.pwEdges(st, groups)
	gap := int(math.Round(64 * float64(f.Width) / 3264))
	for _, pg := range groups {
		ti := pg.g.Find("Text and Icons")
		ref := findRef(ti, "Textbox Reference")
		if ref.Empty() {
			a.note("%s: %s has no Textbox Reference, so its abilities are left out", st.src.Name, pg.g.Name)
			continue
		}
		sec := a.pwSection(st, lg, ref, gap, badges)
		if sec == nil {
			continue
		}
		if loyalty != "" {
			sec.Set("avoidLayer", loyalty)
		}
		if top != "" {
			sec.Set("above", top)
		}
		if bands := a.pwBands(st, pg); bands != nil {
			sec.Set("bands", append(bands, edges...))
		}
		if pg.cond != "" {
			sec.Set("condition", pg.cond)
		}
		name := "abilities"
		if pg.prefix != "" {
			name = "abilities_" + pg.prefix[2:]
		}
		st.addSection(name, sec)
	}

	boxes, err := a.pwTextBoxes(st, groups)
	if err != nil {
		return err
	}
	// The legal line on the right moves to the artist's row when the loyalty
	// box takes its place
	if raw := boxes.Get("copyright"); raw != nil && loyalty != "" && boxes.Has("artist") {
		var box Object
		if err := box.UnmarshalJSON(raw); err != nil {
			return err
		}
		box.Set("flow", obj("duck", obj("layer", loyalty, "row", "artist")))
		boxes.Set("copyright", box)
	}
	st.boxes = boxes
	a.dropRulesNote(st)
	if ref := findRef(groups[0].g.Find("Text and Icons"), "Expansion Reference"); !ref.Empty() {
		st.symbol = symbolAt(ref)
		if len(groups) > 1 {
			a.note("%s: the set symbol sits where %s has it, since a layout has one place for it", st.src.Name, groups[0].g.Name)
		}
	}
	// The textbox is translucent, so the art runs down behind it the way
	// Proxyshop fills a planeswalker's art to the frame
	if art := artWindow(f, st.artAfter, true); art != nil {
		st.art = []Object{art}
	}
	return nil
}

// symbolAt is the set symbol's box for an Expansion Reference
func symbolAt(r image.Rectangle) Object {
	w := r.Dy() * 5 / 2
	return obj("x", r.Max.X-w, "y", r.Min.Y, "width", w, "height", r.Dy(), "align", "right", "vAlign", "center")
}

// pwTwinsByPinlines gives a root Name & Title Boxes group with pair layers the
// pinline colors, which is how a planeswalker template with one reads it
func (a *analyzer) pwTwinsByPinlines(st *layoutState) {
	for i := range a.recipe.Parts {
		p := &a.recipe.Parts[i].Rule
		if p.Source != st.src.Key || p.Slot != "twins" || p.Render == nil || len(p.Render.Target) != 1 {
			continue
		}
		if hasPair(p.Render.Variants.Keys()) {
			p.Slot = "pinlines"
		}
	}
}

// pwBadges makes a part of each loyalty badge, by the sign of the cost it
// shows, with the cost's text left for the section to draw
func (a *analyzer) pwBadges(st *layoutState, lg *psd.Layer) Object {
	parts := Object{}
	for _, b := range []struct{ layer, key string }{{"+", "plus"}, {"-", "minus"}, {"0", "zero"}} {
		g := lg.Find(b.layer)
		if g == nil || !g.IsGroup() {
			continue
		}
		r := &Render{Target: g.Path(), Show: [][]string{g.Path()}, Variants: anyVariant()}
		for _, c := range g.Children {
			if c.Kind == psd.KindText && c.Visible {
				r.Hide = append(r.Hide, c.Path())
			}
		}
		name := a.partName(st.src.Key, "badge_"+b.key)
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{Source: st.src.Key, Dir: st.src.Key + "/badge_" + b.key, Render: r}})
		parts.Set(b.key, name)
	}
	return parts
}

// pwSection lays out one size group's abilities from the sample text layers
func (a *analyzer) pwSection(st *layoutState, lg *psd.Layer, ref image.Rectangle, gap int, badges Object) Object {
	ability := findText(lg, "Ability Text")
	static := findText(lg, "Static Text")
	if ability == nil {
		a.note("%s: Loyalty Graphics has no Ability Text, so the abilities are left out", st.src.Name)
		return nil
	}
	ts := ability.sectionStyle()
	sec := obj("split", "loyalty", "rect", rectObj(ref), "text", ts)
	body := ability.paragraph()
	right := min(body.Max.X, ref.Max.X)
	sec = append(sec, obj("body", obj("x", body.Min.X, "width", right-body.Min.X))...)
	if static != nil {
		p := static.paragraph()
		right := min(p.Max.X, ref.Max.X)
		sec = append(sec, obj("plain", obj("x", p.Min.X, "width", right-p.Min.X))...)
	}
	sec = append(sec, obj("gap", gap)...)

	// Proxyshop centers the colon on its ability and moves the badge with it,
	// so the colon's middle is where the authored row's middle was. The badge
	// sits beside the ability's opening line specifically, a real card's own
	// convention, so it anchors to that line rather than to the middle of
	// however many lines the ability wraps to on a given card
	colon := findText(lg, "Colon")
	anchor := (ability.l.Rect.Min.Y + ability.l.Rect.Max.Y) / 2
	if colon != nil {
		anchor = (colon.l.Rect.Min.Y + colon.l.Rect.Max.Y) / 2
	}
	var items []Object
	if len(badges) > 0 {
		items = append(items, obj("for", "cost", "parts", badges, "y", anchor, "anchor", "first-line"))
	}
	// The cost text sits a little apart in each badge, and the zero badge's
	// place is the middle of them
	cost := findText(lg.Find("0"), "Cost")
	if cost == nil {
		cost = findText(lg, "Cost")
	}
	if cost != nil {
		w := int(math.Round(cost.size * 2.4))
		if s := lg.Find("0", "Symbol"); s != nil && !s.Rect.Empty() {
			w = s.Rect.Dx()
		}
		items = append(items, obj("for", "cost", "rect", cost.baselineBox(w), "text", cost.style(a, st.src.Name), "y", anchor, "anchor", "first-line"))
	}
	if colon != nil {
		items = append(items, obj("for", "cost", "rect", colon.baselineBox(int(math.Round(colon.size))), "text", colon.style(a, st.src.Name), "format", ":", "y", anchor, "anchor", "first-line"))
	}
	if len(items) > 0 {
		sec = append(sec, obj("items", items)...)
	}
	if d := lg.Find("Divider"); d != nil && !d.IsGroup() && !d.Rect.Empty() {
		name := a.partName(st.src.Key, "ability_divider")
		r := &Render{Target: d.Path(), Show: [][]string{d.Path()}, Variants: anyVariant()}
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{Source: st.src.Key, Dir: st.src.Key + "/ability_divider", Render: r}})
		// Proxyshop sets the divider midway between two abilities and then six
		// pixels higher
		mid := (d.Rect.Min.Y+d.Rect.Max.Y)/2 + int(math.Round(6*float64(st.f.Height)/4440))
		sec = append(sec, obj("divider", obj("part", name, "y", mid))...)
	}
	return sec
}

// pwBands makes the darker textbox each other ability shows, cut the way the
// Ragged Lines knockout cuts it out of the pinlines
func (a *analyzer) pwBands(st *layoutState, pg pwGroup) Object {
	dark := pwDark(pg.g)
	if dark == nil {
		return nil
	}
	prefix := pg.prefix
	if prefix != "" {
		prefix += "_"
	}
	band := a.partName(st.src.Key, prefix+"band")
	r := &Render{Target: dark.Path(), Show: [][]string{dark.Path()}, Variants: anyVariant(), Cut: pwPinlineCut(pg.g)}
	a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: band, Rule: PartRule{Source: st.src.Key, Dir: st.src.Key + "/" + prefix + "band", Render: r}})
	return obj("part", band, "rows", "even")
}

// pwDark is a size group's darker textbox, which Proxyshop shows on the
// abilities its ragged lines leave
func pwDark(g *psd.Layer) *psd.Layer {
	if d := g.Find("Textbox", "Ability Dividers", "Textbox"); d != nil && !d.Rect.Empty() {
		return d
	}
	return nil
}

// pwPinlineCut cuts a size group's pinlines out of what is drawn over them
func pwPinlineCut(g *psd.Layer) []RenderCut {
	pin := g.Find("Pinlines")
	if pin == nil {
		return nil
	}
	c := RenderCut{Target: pin.Path()}
	if colors := colorChildrenAtLeast(pin, 1); len(colors) > 0 {
		c.Show = [][]string{colors[len(colors)-1].Path()}
	}
	return []RenderCut{c}
}

// pwEdges makes the ragged edges of the bands from the Masks group's lines. A
// band is cut straight midway between two abilities, and each edge adds the
// darker textbox outside its line's shape with its straight side on that cut,
// so the band ends in the line's ragged outline
func (a *analyzer) pwEdges(st *layoutState, groups []pwGroup) Object {
	masks := rootGroup(st.f, "Masks")
	if masks == nil {
		return nil
	}
	out := Object{}
	for _, e := range []struct {
		line, key string
		top       bool
	}{{"Bottom", "top", true}, {"Top", "bottom", false}} {
		line := masks.Find(e.line)
		if line == nil || line.Rect.Empty() {
			continue
		}
		var src *pwGroup
		for i := range groups {
			if d := pwDark(groups[i].g); d != nil && d.Rect.Min.Y <= line.Rect.Min.Y && d.Rect.Max.Y >= line.Rect.Max.Y {
				src = &groups[i]
				break
			}
		}
		if src == nil {
			a.note("%s: no darker textbox covers Masks/%s, so the bands end straight", st.src.Name, e.line)
			continue
		}
		dark := pwDark(src.g)
		name := a.partName(st.src.Key, "band_"+e.key)
		r := &Render{Target: dark.Path(), Show: [][]string{dark.Path()}, Variants: anyVariant(),
			Cut: append([]RenderCut{{Target: line.Path()}}, pwPinlineCut(src.g)...)}
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{
			Source: st.src.Key, Dir: st.src.Key + "/band_" + e.key, Crop: rectOf(line.Rect), Render: r}})
		y := line.Rect.Max.Y
		if !e.top {
			y = line.Rect.Min.Y
		}
		out.Set(e.key, obj("part", name, "y", y))
	}
	return out
}

// pwTextBoxes reads the title, mana, type and legal lines from the first size
// group and moves each box the second group places differently on its
// condition
func (a *analyzer) pwTextBoxes(st *layoutState, groups []pwGroup) (Object, error) {
	base, err := a.textBoxesIn(st, groups[0].g.Find("Text and Icons"))
	if err != nil {
		return nil, err
	}
	if len(groups) < 2 {
		return base, nil
	}
	other, err := a.textBoxesIn(st, groups[1].g.Find("Text and Icons"))
	if err != nil {
		return nil, err
	}
	for i, m := range base {
		r0, ok0 := boxRect(base, m.Key)
		r1, ok1 := boxRect(other, m.Key)
		if !ok0 || !ok1 {
			continue
		}
		diff := map[string]int{}
		for _, k := range []string{"x", "y", "width", "height"} {
			if r0[k] != r1[k] {
				diff[k] = r1[k]
			}
		}
		if len(diff) == 0 {
			continue
		}
		var box Object
		if err := box.UnmarshalJSON(m.Value); err != nil {
			return nil, err
		}
		var when []boxWhen
		if raw := box.Get("when"); raw != nil {
			if err := json.Unmarshal(raw, &when); err != nil {
				return nil, err
			}
		}
		out := make([]Object, 0, len(when)*2+1)
		for _, w := range when {
			rect := map[string]int{}
			for k, v := range diff {
				rect[k] = v
			}
			for k, v := range w.Rect {
				rect[k] = v
			}
			out = append(out, obj("condition", joinConditions(w.Condition, groups[1].cond), "rect", rectFields(rect)))
		}
		for _, w := range when {
			out = append(out, obj("condition", w.Condition, "rect", rectFields(w.Rect)))
		}
		out = append(out, obj("condition", groups[1].cond, "rect", rectFields(diff)))
		box.Set("when", out)
		base[i].Value = mustRaw(box)
	}
	return base, nil
}

// boxWhen is an override of a text box's rect on a condition
type boxWhen struct {
	Condition string         `json:"condition"`
	Rect      map[string]int `json:"rect"`
}

// rectFields writes the fields of a rect that are set, in the order a rect
// lists them
func rectFields(r map[string]int) Object {
	o := Object{}
	for _, k := range []string{"x", "y", "width", "height"} {
		if v, ok := r[k]; ok {
			o.Set(k, v)
		}
	}
	return o
}
