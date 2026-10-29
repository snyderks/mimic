package proxyshop

import (
	"image"
	"math"

	"github.com/odevine/mimic/engine/psd"
)

// Kinds whose extra text sits in fixed places: a leveler's tiers, a mutate
// line, a prototype's cost and box, a battle's defense and a plane's chaos
// ability. Each is a text box with the draws name the engine fills for that
// kind, read from the type layer Proxyshop writes it into

func isLeveler(f *psd.File) bool { return f.Root.Find("Text and Icons", "Leveler Text") != nil }

// levelerAfter adds the level up text, the tiers' ranges, texts and P/T, and
// the starting P/T in the top box
func (a *analyzer) levelerAfter(st *layoutState) error {
	if err := a.ownBoxes(st, true); err != nil {
		return err
	}
	lt := st.f.Root.Find("Text and Icons", "Leveler Text")
	if up := findText(lt, "Rules Text - Level Up"); up != nil {
		st.addBox("level_up", up.areaBox(findRef(lt, "Textbox Reference - Level Text")))
	}
	if pt := findText(lt, "Top Power / Toughness"); pt != nil {
		st.addBox("pt", a.pointBox(st, pt, ptWidth(pt)))
	}
	for i, tier := range []struct{ name, text, ref string }{
		{"Middle", "Rules Text - Levels X-Y", "Textbox Reference - Level X-Y"},
		{"Bottom", "Rules Text - Levels Z+", "Textbox Reference - Levels Z+"},
	} {
		n := []string{"1", "2"}[i]
		if l := findText(lt, tier.name+" Level Label"); l != nil {
			st.addBox("level_label_"+n, a.pointBox(st, l, l.l.Rect.Dx()*3/2, "draws", "level_label"))
		}
		if l := findText(lt, tier.name+" Level"); l != nil {
			st.addBox("level_"+n, a.pointBox(st, l, max(l.l.Rect.Dx()*3/2, int(math.Round(l.size*2.5)))))
		}
		if t := findText(lt, tier.text); t != nil {
			st.addBox("level_"+n+"_text", t.areaBox(findRef(lt, tier.ref)))
		}
		if pt := findText(lt, tier.name+" Power / Toughness"); pt != nil {
			st.addBox("level_"+n+"_pt", a.pointBox(st, pt, ptWidth(pt)))
		}
	}
	return nil
}

// ptWidth is the width of a box for a P/T type layer, room for a two digit
// power and toughness
func ptWidth(t *textLayer) int { return max(t.l.Rect.Dx()*2, int(math.Round(t.size*3))) }

// dropRulesNote takes back the note that the layout has no rules text, for a
// kind whose rules are written in boxes of its own
func (a *analyzer) dropRulesNote(st *layoutState) {
	out := a.notes[:0]
	for _, n := range a.notes {
		if n != st.src.Name+": no rules text layer, so the layout has no rules text" {
			out = append(out, n)
		}
	}
	a.notes = out
}

func isMutate(f *psd.File) bool { return findText(f.Root.Find("Text and Icons"), "Mutate") != nil }

// mutateAfter adds the mutate line in its reference above the rules text
func (a *analyzer) mutateAfter(st *layoutState) error {
	ti := st.f.Root.Find("Text and Icons")
	if m := findText(ti, "Mutate"); m != nil {
		st.addBox("mutate", m.areaBox(findRef(ti, "Mutate Reference")))
	}
	return nil
}

// prototypeGroups are a prototype template's root groups, which draw in the
// prototype's colors
var prototypeGroups = []string{"Prototype Textbox", "Prototype PT Box", "Prototype Manabox"}

func isPrototype(f *psd.File) bool { return rootGroup(f, "Prototype Textbox") != nil }

func (a *analyzer) prototypeBefore(st *layoutState) error {
	for _, n := range prototypeGroups {
		if g := rootGroup(st.f, n); g != nil {
			st.claimed[g] = true
		}
	}
	return nil
}

// prototypeAfter adds the prototype's textbox, P/T box and cost box in the
// prototype slot, and the boxes for its reminder, cost and P/T
func (a *analyzer) prototypeAfter(st *layoutState) error {
	f := st.f
	for _, n := range prototypeGroups {
		g := rootGroup(f, n)
		if g == nil {
			continue
		}
		colors := colorChildrenAtLeast(g, 1)
		if len(colors) == 0 {
			continue
		}
		vs, err := colorVariants(colors)
		if err != nil {
			return err
		}
		r := &Render{Target: g.Path(), Variants: vs}
		if !g.Visible {
			r.Show = append(r.Show, g.Path())
		}
		// The cost box has a shape for two symbols and one for three, and the
		// wider one holds either
		if sh := g.Find("Shape"); sh != nil {
			if wide := pick(sh, []string{"3", "2"}); wide != nil {
				r.Show = append(r.Show, sh.Path(), wide.Path())
				a.note("%s: %q always draws its three symbol shape, since nothing picks it by the prototype cost's length", st.src.Name, n)
			}
		}
		start, firstPart := len(st.entries), len(a.recipe.Parts)
		a.addPart(st, g, slug(n), "prototype", "", r)
		a.placeEntries(st, g, a.takeEntries(st, start, firstPart, "", ""))
	}
	ti := f.Root.Find("Text and Icons")
	textbox := findRef(ti, "Textbox Reference")
	if t := findText(ti, "Prototype Rules Text"); t != nil {
		p := t.paragraph()
		if !textbox.Empty() && p.Max.Y > textbox.Min.Y {
			p.Max.Y = textbox.Min.Y
		}
		st.addBox("prototype", obj("rect", rectObj(p), "text", t.rulesStyle("center"), "flow", Object{}))
	}
	if t := findText(ti, "Prototype Mana Cost"); t != nil {
		ts := obj("fontSize", math.Round(t.size), "color", hexColor(t.run.FillColor), "align", align(t.t.Justification), "vAlign", "baseline",
			"shadow", obj("distance", 0.0907, "angle", 25))
		st.addBox("prototype_mana", obj("rect", t.baselineBox(t.l.Rect.Dx()*2), "text", ts, "flow", Object{}))
	}
	if t := findText(ti, "Prototype Power / Toughness"); t != nil {
		st.addBox("prototype_pt", a.pointBox(st, t, ptWidth(t)))
	}
	return nil
}

func isBattle(f *psd.File) bool { return rootGroup(f, "Defense Box") != nil }

// battleAfter adds the defense in its shield, keeps the rules clear of it,
// and lays the legal lines along the side the frame turns to the bottom
func (a *analyzer) battleAfter(st *layoutState) error {
	ti := st.f.Root.Find("Text and Icons")
	boxes, err := a.textBoxes(st)
	if err != nil {
		return err
	}
	shield := "defense_box"
	if st.entryIndex(shield) < 0 {
		shield = ""
	}
	if raw := boxes.Get("oracle"); raw != nil && shield != "" {
		var box Object
		if err := box.UnmarshalJSON(raw); err != nil {
			return err
		}
		flow := obj("avoidLayer", shield)
		if st.divider != "" {
			flow = append(flow, obj("divider", st.divider)...)
		}
		box.Set("flow", flow)
		boxes.Set("oracle", box)
	}
	if d := findText(ti, "Defense"); d != nil {
		w := int(math.Round(d.size * 2))
		if g := rootGroup(st.f, "Defense Box"); g != nil {
			if b := groupBounds(g); !b.Empty() {
				w = b.Dx()
			}
		}
		boxes.Set("defense", a.pointBox(st, d, w))
	}
	if st.f.Width > st.f.Height {
		for _, k := range []string{"collector", "set", "artist", "copyright"} {
			boxes.Delete(k)
		}
		a.legalOnTheSide(st, func(k string, box Object) { boxes.Set(k, box) })
		// The artist follows the set code on the lower row, as it does on an
		// upright card, rather than sharing the collector number's row
		legal := st.f.Root.Find("Legal")
		if row := findText(legal, "Bottom"); row != nil && findText(legal, "Top") != nil {
			if r, ok := boxRect(boxes, "set"); ok {
				if raw := boxes.Get("artist"); raw != nil {
					var box Object
					if err := box.UnmarshalJSON(raw); err != nil {
						return err
					}
					ar, _ := boxRect(boxes, "artist")
					box.Set("rect", obj("x", r["x"]+int(math.Round(401*row.size/68)), "y", r["y"], "width", ar["width"], "height", ar["height"]))
					boxes.Set("artist", box)
				}
			}
		}
	}
	st.boxes = boxes
	return nil
}

// groupBounds is the union of the pixels of a group's visible layers
func groupBounds(g *psd.Layer) image.Rectangle {
	var r image.Rectangle
	for _, c := range g.Children {
		if !c.Visible {
			continue
		}
		if c.IsGroup() {
			r = r.Union(groupBounds(c))
		} else {
			r = r.Union(c.Rect)
		}
	}
	return r
}

func isPlanar(f *psd.File) bool {
	return findText(f.Root.Find("Text and Icons"), "Chaos Ability") != nil
}

// planarAfter keeps the plane landscape, as it is printed, and adds the
// static ability, the chaos ability and the chaos symbol a phenomenon leaves
// out
func (a *analyzer) planarAfter(st *layoutState) error {
	f := st.f
	if err := a.ownBoxes(st, true); err != nil {
		return err
	}
	ti := f.Root.Find("Text and Icons")
	// A plane is an oversized card about 3.5 inches tall with its bleed
	st.canvas = obj("width", f.Width, "height", f.Height, "dpi", int(math.Round(float64(f.Height)/3.74)))
	if c := ti.Find("Chaos Symbol"); c != nil {
		r := &Render{Target: c.Path(), Variants: anyVariant()}
		if !c.Visible {
			r.Show = append(r.Show, c.Path())
		}
		a.addPart(st, c, "chaos_symbol", "", "chaos", r)
	}
	static := findText(ti, "Static Ability")
	chaos := findText(ti, "Chaos Ability")
	if static != nil {
		p := static.paragraph()
		if chaos != nil && p.Max.Y > chaos.l.Rect.Min.Y {
			p.Max.Y = chaos.l.Rect.Min.Y - int(math.Round(chaos.size/4))
		}
		st.addBox("oracle", obj("rect", rectObj(p), "text", static.rulesStyle("top"), "flow", Object{}))
	}
	if chaos != nil {
		p := chaos.paragraph()
		if p.Max.Y > f.Height {
			p.Max.Y = f.Height
		}
		st.addBox("chaos", obj("rect", rectObj(p), "text", chaos.rulesStyle("top"), "flow", Object{}))
	}
	// The legal lines are centered under the textbox, the artist above the set
	legal := f.Root.Find("Legal")
	if artist, set := findText(legal, "Artist"), findText(legal, "Set"); artist != nil && set != nil {
		for _, k := range []string{"collector", "copyright"} {
			st.boxes.Delete(k)
		}
		w := f.Width / 2
		ts := obj("fontSize", math.Round(artist.size*0.925), "font", "small-caps", "color", hexColor(artist.run.FillColor), "align", "center", "vAlign", "baseline")
		st.addBox("artist", obj("rect", artist.baselineBox(w), "text", ts, "flow", Object{}))
		st.addBox("set", a.pointBox(st, set, w))
	}
	return nil
}
