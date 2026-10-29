package proxyshop

import (
	"math"
	"strings"

	"github.com/odevine/mimic/engine/psd"
)

// Double faced and two part cards: the transform icon a planeswalker template
// keeps at the root, the bars a modal double faced card shows for its other
// face, and the adventure half's text

// rootTransform is a Transform group at the document's root holding the
// circle and the icons, as the planeswalker transform templates keep it
func rootTransform(f *psd.File) *psd.Layer {
	g := rootGroup(f, "Transform")
	if g == nil || g.Find("Circle") == nil {
		return nil
	}
	return g
}

func hasRootTransform(f *psd.File) bool { return rootTransform(f) != nil }

func (a *analyzer) transformBefore(st *layoutState) error {
	st.claimed[rootTransform(st.f)] = true
	return nil
}

// transformAfter adds the circle and the icons of the face, which Proxyshop
// shows only on the faces of a transform card
func (a *analyzer) transformAfter(st *layoutState) error {
	if !strings.HasPrefix(st.role, "transform") {
		return nil
	}
	tf := rootTransform(st.f)
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	if err := a.transformIcons(st, tf); err != nil {
		return err
	}
	a.placeEntries(st, tf, a.takeEntries(st, start, firstPart, "", ""))
	return nil
}

// transformIcons adds a Transform group's circle and a part per side whose
// variants are the icons
func (a *analyzer) transformIcons(st *layoutState, tf *psd.Layer) error {
	if c := tf.Find("Circle"); c != nil {
		r := &Render{Target: c.Path(), Variants: anyVariant()}
		if !c.Visible {
			r.Show = append(r.Show, c.Path())
		}
		a.addPart(st, c, "transform_circle", "", "", r)
	}
	for _, side := range []string{"Front", "Back"} {
		icons := tf.Find(side)
		if icons == nil || !icons.IsGroup() {
			continue
		}
		r := &Render{Target: icons.Path(), Variants: Object{}}
		if !icons.Visible {
			r.Show = append(r.Show, icons.Path())
		}
		for i := len(icons.Children) - 1; i >= 0; i-- {
			c := icons.Children[i]
			key := slug(c.Name)
			if key == "meld" || r.Variants.Has(key) {
				continue
			}
			v := RenderVariant{Show: [][]string{c.Path()}}
			for _, o := range icons.Children {
				if o != c && o.Visible {
					v.Hide = append(v.Hide, o.Path())
				}
			}
			if err := r.Variants.Set(key, v); err != nil {
				return err
			}
		}
		// A PSD whose icon group does not cover every icon a card can need,
		// such as the plain triangle a card with no named frame effect
		// defaults to, falls back to the embedded art for the keys it lacks
		fallbacks := make([]string, 0, len(transformIconAssets))
		for key := range transformIconAssets {
			fallbacks = append(fallbacks, key)
		}
		sortKeys(fallbacks)
		for _, key := range fallbacks {
			if !r.Variants.Has(key) {
				r.AssetVariants = append(r.AssetVariants, key)
			}
		}
		if len(r.Variants) > 0 {
			a.addPart(st, icons, "transform_icon_"+strings.ToLower(side), "transform_icon", "", r)
		}
	}
	return nil
}

// mdfcGroup is the group of a modal double faced card's bars, MDFC Front or
// MDFC Back, at the root or among the text
func mdfcGroup(f *psd.File) *psd.Layer {
	for _, n := range []string{"MDFC Front", "MDFC Back"} {
		if g := rootGroup(f, n); g != nil {
			return g
		}
		if g := f.Root.Find("Text and Icons", n); g != nil && g.IsGroup() {
			return g
		}
	}
	return nil
}

func hasMDFC(f *psd.File) bool { return mdfcGroup(f) != nil }

func (a *analyzer) mdfcBefore(st *layoutState) error {
	st.claimed[mdfcGroup(st.f)] = true
	return nil
}

// mdfcAfter adds the face's icon in its own title box colors and the bar
// along the bottom in the other face's, with the other face's type and cost
// written on the bar
func (a *analyzer) mdfcAfter(st *layoutState) error {
	g := mdfcGroup(st.f)
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	for _, s := range []struct {
		name, part, face string
	}{{"Bottom", "mdfc_bottom", "other"}, {"Top", "mdfc_top", ""}} {
		sub := g.Find(s.name)
		if sub == nil || !sub.IsGroup() {
			continue
		}
		colors := colorChildrenAtLeast(sub, 1)
		if len(colors) == 0 {
			continue
		}
		vs, err := colorVariants(colors)
		if err != nil {
			return err
		}
		r := &Render{Target: sub.Path(), Variants: vs}
		if !g.Visible {
			r.Show = append(r.Show, g.Path())
		}
		if !sub.Visible {
			r.Show = append(r.Show, sub.Path())
		}
		a.addPart(st, sub, s.part, "twins", "", r)
		st.entries[len(st.entries)-1].face = s.face
	}
	es := a.takeEntries(st, start, firstPart, "", "")
	if g.Parent == st.f.Root {
		a.placeEntries(st, g, es)
	} else {
		// Bars kept among the text sit above the frame
		st.entries = append(st.entries, es...)
	}

	right := findText(g, "Right")
	left := findText(g, "Left")
	if right != nil {
		w := int(math.Round(right.x)) - st.f.Width/10
		if left != nil {
			w = int(math.Round(right.x - left.x))
		}
		st.moreBoxes.Set("flipside_mana", a.pointBox(st, right, w))
	}
	if left != nil {
		w := st.f.Width / 3
		box := a.pointBox(st, left, w)
		if right != nil {
			box.Set("rect", left.baselineBox(int(math.Round(right.x-left.x))))
			box.Set("flow", obj("clearOf", "flipside_mana"))
		}
		st.moreBoxes.Set("flipside_type", box)
	}
	return nil
}

// isAdventure is a PSD with an adventure half's text beside the card's own
func isAdventure(f *psd.File) bool {
	return f.Root.Find("Text and Icons", "Card Name - Adventure") != nil
}

// adventureAfter writes the adventure's title, cost, type and rules on the
// left page, read from the second half, and keeps the card's own text to the
// first
func (a *analyzer) adventureAfter(st *layoutState) error {
	if isStorybook(st.f) {
		if err := a.storybook(st); err != nil {
			return err
		}
	}
	ti := st.f.Root.Find("Text and Icons")
	boxes, err := a.textBoxes(st)
	if err != nil {
		return err
	}
	for i, m := range boxes {
		switch m.Key {
		case "title", "mana", "type_full", "oracle", "pt":
			var box Object
			if err := box.UnmarshalJSON(m.Value); err != nil {
				return err
			}
			box.Set("face", "first")
			boxes[i].Value = mustRaw(box)
		}
	}
	mana := findText(ti, "Mana Cost - Adventure")
	if mana != nil {
		ts := obj("fontSize", math.Round(mana.size), "color", hexColor(mana.run.FillColor), "align", align(mana.t.Justification), "vAlign", "baseline",
			"minFontSize", math.Round(mana.size), "shadow", obj("distance", 0.0907, "angle", 25))
		boxes.Set("adventure_mana", obj("rect", mana.baselineBox(mana.l.Rect.Dx()*2), "text", ts, "flow", Object{}, "draws", "mana", "face", "second"))
	}
	ref := findRef(ti, "Textbox Reference - Adventure")
	if title := findText(ti, "Card Name - Adventure"); title != nil {
		w := ref.Max.X - int(title.x)
		flow := Object{}
		if mana != nil {
			w = int(mana.x - title.x)
			flow = obj("clearOf", "adventure_mana")
		}
		boxes.Set("adventure_title", obj("rect", title.baselineBox(w), "text", title.style(a, st.src.Name), "flow", flow, "draws", "title", "face", "second"))
	}
	if tl := findText(ti, "Typeline - Adventure"); tl != nil {
		w := ref.Max.X - int(tl.x)
		if ref.Empty() {
			w = tl.l.Rect.Dx()
		}
		boxes.Set("adventure_type", a.pointBox(st, tl, w, "draws", "type", "face", "second"))
	}
	if rules := findText(ti, "Rules Text - Adventure"); rules != nil {
		pad := 40 * st.f.Width / 3264
		box := rules.areaBox(ref, "draws", "oracle", "face", "second")
		box.Set("text", append(rules.rulesStyle("center"), obj("paddingY", pad)...))
		boxes.Set("adventure_oracle", box)
	}
	st.boxes = boxes
	return nil
}
