package proxyshop

import (
	"image"
	"math"

	"github.com/odevine/mimic/engine/psd"
)

// A saga keeps its chapter text and numerals in a Saga group, and a class its
// level text and level bar in a Class group. Proxyshop writes each chapter or
// level into a copy of the sample Text layer, spreads them down the Textbox
// Reference, and puts copies of the numerals or of the level bar beside each.
// The conversion makes them a section split by chapters or levels, with the
// first line, the reminder, in a description box

// chapterGroup is a saga's or a class's group and the group holding its
// references, with the split its rows take
type chapterGroup struct {
	g, refs *psd.Layer
	split   string
}

func chapterGroups(st *layoutState) []chapterGroup {
	f := st.f
	// A vertical document keeps each kind's references in its own group
	switch verticalKind(st) {
	case "saga":
		g := rootGroup(f, "Saga")
		return []chapterGroup{{g, g, "chapters"}}
	case "class":
		g := rootGroup(f, "Class")
		return []chapterGroup{{g, g, "levels"}}
	}
	var out []chapterGroup
	ti := f.Root.Find("Text and Icons")
	for _, c := range []struct{ name, split string }{{"Saga", "chapters"}, {"Class", "levels"}} {
		if g := f.Root.Find("Text and Icons", c.name); g != nil && g.IsGroup() {
			out = append(out, chapterGroup{g, ti, c.split})
		}
	}
	return out
}

func isChapters(st *layoutState) bool { return len(chapterGroups(st)) > 0 }

func (a *analyzer) chaptersBefore(st *layoutState) error {
	if k := verticalKind(st); k != "" {
		a.verticalBefore(st, k)
	}
	return nil
}

func (a *analyzer) chaptersAfter(st *layoutState) error {
	if err := a.ownBoxes(st, true); err != nil {
		return err
	}
	for _, cg := range chapterGroups(st) {
		if err := a.chapters(st, cg); err != nil {
			return err
		}
	}
	if k := verticalKind(st); k != "" {
		if err := a.verticalAfter(st, k); err != nil {
			return err
		}
	}
	// Proxyshop shows TF Twins only for a saga that transforms
	for i := range st.entries {
		if st.entries[i].id == "tf_twins" {
			st.entries[i].condition = joinConditions(st.entries[i].condition, "front")
		}
	}
	// The saga's left panel takes the background's colors, not the pinlines'
	for i := range a.recipe.Parts {
		p := &a.recipe.Parts[i].Rule
		if p.Source == st.src.Key && p.Dir == st.src.Key+"/textbox" && p.Slot == "pinlines" && st.f.Root.Find("Pinlines & Saga Stripe") != nil {
			p.Slot = "background"
		}
	}
	return nil
}

func (a *analyzer) chapters(st *layoutState, cg chapterGroup) error {
	g := cg.g
	src := st.src.Name
	ref := findRef(cg.refs, "Textbox Reference")
	text := findText(g, "Text")
	if ref.Empty() || text == nil {
		a.note("%s: %s has no Textbox Reference or Text layer, so its rows are left out", src, g.Name)
		return nil
	}
	h := float64(st.f.Height)
	spacing := int(math.Round(80 * h / 4440))
	sec := obj("split", cg.split, "rect", rectObj(ref), "text", text.sectionStyle())
	p := text.paragraph()
	sec = append(sec, obj("body", obj("x", p.Min.X, "width", min(p.Max.X, ref.Max.X)-p.Min.X), "gap", spacing)...)

	// The divider under the reminder stays where the PSD has it, and the
	// section draws it again between the rows
	divider := ""
	if d := g.Find("Divider"); d != nil && !d.IsGroup() && !d.Rect.Empty() {
		r := &Render{Target: d.Path(), Variants: anyVariant()}
		if !d.Visible {
			r.Show = append(r.Show, d.Path())
		}
		a.addPart(st, d, slug(g.Name)+"_divider", "", "", r)
		divider = a.recipe.Parts[len(a.recipe.Parts)-1].Name
	}

	switch cg.split {
	case "chapters":
		sec = append(sec, obj("spread", obj("outside", 1, "inside", 1.5))...)
		if items := a.sagaNumerals(st, g, spacing); items != nil {
			sec = append(sec, obj("items", []Object{items})...)
		}
		if divider != "" {
			d := g.Find("Divider")
			sec = append(sec, obj("divider", obj("part", divider, "y", (d.Rect.Min.Y+d.Rect.Max.Y)/2))...)
		}
	case "levels":
		header, items := a.classStage(st, g, spacing)
		if header > 0 {
			sec = append(sec, obj("header", obj("for", "cost", "height", header), "items", items)...)
		}
	}
	st.addSection(slug(cg.split), sec)

	// The reminder line goes in its own box above the rows
	if rt := findText(g, "Reminder Text"); rt != nil {
		dref := findRef(cg.refs, "Description Reference")
		if dref.Empty() {
			dref = findRef(g, "Description Reference")
		}
		var box Object
		if !dref.Empty() {
			box = rt.areaBox(dref)
		} else {
			// With no reference the reminder fills its paragraph down to the
			// divider below it
			r := rt.paragraph()
			if d := g.Find("Divider"); d != nil && d.Rect.Min.Y > r.Min.Y && d.Rect.Min.Y < r.Max.Y {
				r.Max.Y = d.Rect.Min.Y
			}
			box = obj("rect", rectObj(r), "text", rt.rulesStyle("center"), "flow", Object{})
		}
		st.addBox("description", box)
	}
	return nil
}

// sagaNumerals makes a part of each chapter numeral, every one placed where
// the first is so the item moves them all alike, and the item that draws them
// beside each chapter, spaced the way Proxyshop stacks several numerals
func (a *analyzer) sagaNumerals(st *layoutState, g *psd.Layer, spacing int) Object {
	first := g.Find("I")
	if first == nil || first.Rect.Empty() {
		return nil
	}
	mid := (first.Rect.Min.Y + first.Rect.Max.Y) / 2
	parts := Object{}
	for _, n := range []string{"I", "II", "III", "IV", "V", "VI"} {
		l := g.Find(n)
		if l == nil || l.Rect.Empty() {
			continue
		}
		name := a.partName(st.src.Key, slug(g.Name)+"_"+slug(n))
		r := &Render{Target: l.Path(), Show: [][]string{l.Path()}, Variants: anyVariant()}
		crop := l.Rect.Inset(-styleReach(l))
		at := image.Pt(crop.Min.X, crop.Min.Y-(l.Rect.Min.Y+l.Rect.Max.Y)/2+mid)
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{
			Source: st.src.Key, Dir: st.src.Key + "/" + slug(g.Name) + "_" + slug(n), Crop: rectOf(crop),
			Origin: mustRaw(obj("x", at.X, "y", at.Y)), Render: r}})
		parts.Set(n, name)
	}
	step := first.Rect.Dy() + int(math.Round(float64(spacing)/3))
	return obj("for", "chapter", "parts", parts, "y", mid, "step", step)
}

// classStage makes the level bar a part and returns the room it takes above
// each level and the items that draw it with the level's cost and number
func (a *analyzer) classStage(st *layoutState, g *psd.Layer, spacing int) (int, []Object) {
	stage := g.Find("Stage")
	if stage == nil || !stage.IsGroup() {
		a.note("%s: %s has no Stage group, so the levels have no bar", st.src.Name, g.Name)
		return 0, nil
	}
	var bounds image.Rectangle
	r := &Render{Target: stage.Path(), Show: [][]string{stage.Path()}, Variants: anyVariant()}
	for _, c := range stage.Children {
		if c.Kind == psd.KindText {
			if c.Visible {
				r.Hide = append(r.Hide, c.Path())
			}
			continue
		}
		if c.Visible {
			bounds = bounds.Union(c.Rect)
		}
	}
	if bounds.Empty() {
		return 0, nil
	}
	name := a.partName(st.src.Key, slug(g.Name)+"_stage")
	a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{Source: st.src.Key, Dir: st.src.Key + "/" + slug(g.Name) + "_stage", Render: r}})
	top := bounds.Min.Y
	items := []Object{obj("for", "cost", "part", name, "anchor", "top", "y", top)}
	if cost := findText(stage, "Cost"); cost != nil {
		items = append(items, obj("for", "cost", "rect", inkLine(cost), "text", cost.style(a, st.src.Name), "format", "{}:", "anchor", "top", "y", top))
	}
	if level := findText(stage, "Level"); level != nil {
		items = append(items, obj("for", "level", "rect", inkLine(level), "text", level.style(a, st.src.Name), "format", "Level {}", "anchor", "top", "y", top))
	}
	// Proxyshop keeps half its spacing between the bar and the level's text
	return bounds.Dy() + spacing/2, items
}

// inkLine is a box for one line of a type layer, with its top on the line's
// baseline as a baseline box has it: across the layer's paragraph, or its
// pixels for point text, and down from the bottom of its pixels, which is the
// baseline for text with no descenders such as a cost or a level
func inkLine(t *textLayer) Object {
	if !t.t.Box {
		return t.baselineBox(int(math.Round(t.size * 4)))
	}
	p := t.paragraph()
	return obj("x", p.Min.X, "y", t.l.Rect.Max.Y, "width", p.Dx(), "height", int(math.Round(t.size*1.3)))
}
