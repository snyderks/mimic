package proxyshop

import (
	"math"

	"github.com/odevine/mimic/engine/psd"
)

// A token template draws the whole frame from one of four groups in Frame, by
// whether the token is legendary and whether it is a creature, and fades the
// art behind its text with one of three Textbox groups, by how much text the
// token has. The conversion draws the creature frames over the others on the
// creature condition, and the fade and text of the group for full text, since
// no condition reads how long a card's text is

func isToken(f *psd.File) bool {
	fr := rootGroup(f, "Frame")
	return fr != nil && fr.Find("Legendary") != nil && fr.Find("Non-Legendary") != nil
}

func (a *analyzer) tokenBefore(st *layoutState) error {
	st.claimed[rootGroup(st.f, "Frame")] = true
	if tb := rootGroup(st.f, "Textbox"); tb != nil && tb.Find("Full") != nil {
		st.claimed[tb] = true
	}
	return nil
}

func (a *analyzer) tokenAfter(st *layoutState) error {
	f := st.f
	src := st.src.Name
	full := (*psd.Layer)(nil)
	if tb := rootGroup(f, "Textbox"); tb != nil {
		full = tb.Find("Full")
		if full != nil {
			r := &Render{Target: tb.Path(), Show: [][]string{full.Path()}, Variants: anyVariant()}
			if !tb.Visible {
				r.Show = append(r.Show, tb.Path())
			}
			for _, c := range full.Children {
				if c.Kind == psd.KindText && c.Visible {
					r.Hide = append(r.Hide, c.Path())
				}
			}
			start, firstPart := len(st.entries), len(a.recipe.Parts)
			a.addPart(st, tb, "textbox", "", "", r)
			a.placeEntries(st, tb, a.takeEntries(st, start, firstPart, "", ""))
			a.note("%s: the token draws the Full textbox for every card, since nothing picks One Line or None by the length of its text", src)
		}
	}

	fr := rootGroup(f, "Frame")
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	for _, set := range []struct{ group, s, cond string }{
		{"Non-Legendary", "frame", "nonlegendary"},
		{"Legendary", "frame_legendary", "legendary"},
	} {
		g := fr.Find(set.group)
		if g == nil {
			continue
		}
		for _, kind := range []struct{ group, s, cond string }{{"Non-Creature", "", ""}, {"Creature", "_creature", "creature"}} {
			k := g.Find(kind.group)
			if k == nil {
				continue
			}
			colors := colorChildrenAtLeast(k, 1)
			if len(colors) == 0 {
				continue
			}
			vs, err := colorVariants(colors)
			if err != nil {
				return err
			}
			r := &Render{Target: k.Path(), Variants: vs, FitPairs: true}
			for _, x := range []*psd.Layer{fr, g, k} {
				if !x.Visible {
					r.Show = append(r.Show, x.Path())
				}
			}
			a.addPart(st, k, set.s+kind.s, "pinlines", joinConditions(set.cond, kind.cond), r)
		}
	}
	a.placeEntries(st, fr, a.takeEntries(st, start, firstPart, "", ""))

	if err := a.ownBoxes(st, true); err != nil {
		return err
	}
	boxes := st.boxes
	ti := f.Root.Find("Text and Icons")
	shadow := obj("distance", 0.0907, "angle", 25)
	if title := findText(ti, "Card Name"); title != nil {
		ref := findRef(ti, "Name Reference Non-Legendary")
		box := obj("rect", title.baselineBox(ref.Dx()), "text", append(title.style(a, src), obj("shadow", shadow)...), "flow", Object{})
		if leg := findRef(ti, "Name Reference Legendary"); !leg.Empty() {
			box = append(box, obj("when", []Object{obj("condition", "legendary", "rect", obj("x", int(math.Round(title.x))-leg.Dx()/2, "width", leg.Dx()))})...)
		}
		boxes.Set("title", box)
	}
	if full != nil {
		if tl := findText(full, "Typeline"); tl != nil {
			w := findRef(full, "Typeline Reference").Dx()
			if w <= 0 {
				w = tl.l.Rect.Dx() * 2
			}
			boxes.Set("type_full", obj("rect", tl.baselineBox(w), "text", append(tl.style(a, src), obj("shadow", shadow)...), "flow", Object{}, "draws", "type"))
		}
		if rules := findText(full, "Rules Text - Noncreature", "Rules Text - Creature", "Rules Text"); rules != nil {
			box := rules.areaBox(findRef(full, "Textbox Reference"))
			ts := rules.rulesStyle("center")
			ts.Set("align", "center")
			ts.Set("shadow", shadow)
			box.Set("text", ts)
			boxes.Set("oracle", box)
		}
	}
	st.boxes = boxes
	return nil
}
