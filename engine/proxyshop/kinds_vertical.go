package proxyshop

import (
	"image"

	"github.com/odevine/mimic/engine/psd"
)

// Proxyshop's vertical-mode.psd draws a saga and a class from one document,
// the art on one half of the card and the text on the other. Root groups named
// Saga and Class hold each kind's text, and the saga's banner and stripe sit
// in its group. A class moves the textbox, drawn on the left in the document,
// onto the right half and puts the art on the left

// kindSources is the sources Analyze reads, with a PSD that serves two kinds
// from groups of its own split into a source per kind, and the shapes a PSD
// made for one kind serves read from its groups
func kindSources(srcs []Source) []Source {
	var out []Source
	for _, s := range srcs {
		// A vector adventure frame draws only adventures
		if s.File != nil && isStorybook(s.File) && isAdventure(s.File) && len(s.Serves) == 0 {
			s.Serves = []Shape{{Role: "adventure", Kind: "standard"}}
		}
		if !isVertical(s.File) {
			out = append(out, s)
			continue
		}
		serves := s.Serves
		if len(serves) == 0 {
			serves = GuessShapes(s.Name)
		}
		for _, k := range []string{"saga", "class"} {
			v := s
			v.Key = s.Key + "_" + k
			v.Serves = []Shape{{Role: serves[0].Role, Kind: k}}
			out = append(out, v)
		}
	}
	return out
}

// isVertical reports whether a document keeps a saga and a class in root
// groups of their own
func isVertical(f *psd.File) bool {
	return f != nil && rootGroup(f, "Saga") != nil && rootGroup(f, "Class") != nil
}

// verticalKind is the kind a layout read from a vertical document draws, saga
// or class, or empty for any other layout
func verticalKind(st *layoutState) string {
	if !isVertical(st.f) || len(st.src.Serves) == 0 {
		return ""
	}
	switch k := st.src.Serves[0].Kind; k {
	case "saga", "class":
		return k
	}
	return ""
}

// sagaBannerColors and sagaStripeColors are Proxyshop's saga_banner_color_map
// and saga_stripe_color_map. Neither has colorless, which Proxyshop then fills
// black
var sagaBannerColors = map[string]rgbColor{
	"w": {241, 225, 193}, "u": {37, 89, 177}, "b": {59, 51, 48}, "r": {218, 56, 44}, "g": {1, 99, 58},
	"gold": {204, 173, 116}, "artifact": {200, 205, 234}, "land": {106, 89, 81}, "colorless": {0, 0, 0},
}

var sagaStripeColors = map[string]rgbColor{
	"w": {235, 209, 156}, "u": {34, 72, 153}, "b": {30, 24, 18}, "r": {197, 41, 30}, "g": {2, 84, 46},
	"gold": {135, 107, 45}, "artifact": {163, 171, 202}, "land": {55, 47, 41}, "colorless": {0, 0, 0},
}

// sagaStripeDual is the stripe of a saga of two colors
var sagaStripeDual = rgbColor{42, 42, 42}

func (a *analyzer) verticalBefore(st *layoutState, kind string) {
	for _, n := range []string{"Saga", "Class"} {
		st.claimed[rootGroup(st.f, n)] = true
	}
	// Proxyshop hides the banner's top for a class
	if b := rootGroup(st.f, "Banner Top"); b != nil && kind == "class" {
		st.claimed[b] = true
	}
}

func (a *analyzer) verticalAfter(st *layoutState, kind string) error {
	a.nameTitleBoxesShape(st)
	switch kind {
	case "saga":
		if err := a.sagaBanner(st); err != nil {
			return err
		}
		st.setArt(st.f.Root.Find("Art Frame Right"))
	case "class":
		a.textboxRight(st)
		st.setArt(st.f.Root.Find("Art Frame Left"))
	}
	return nil
}

// nameTitleBoxesShape fixes Name & Title Boxes/Shape: vertical-mode.psd saves
// "Bottom" visible and "Normal" (or a transform face's own sibling) hidden,
// unlike this document's other Shape groups, where the sibling Proxyshop's
// own code shows is already the one the PSD saves visible. The generic shape
// pick in addPart only overrides a group's native visibility when nothing in
// it is already shown, so it leaves "Bottom" alone, and the title plate draws
// with no texture at all. ClassVectorTemplate and SagaVectorTemplate's own
// enabled_shapes always show Normal (or TF Front/TF Back on a transform face)
// regardless of what the PSD has saved, so this shows that sibling and hides
// whatever else was visible, on every already-built part that draws the group
func (a *analyzer) nameTitleBoxesShape(st *layoutState) {
	g := rootGroup(st.f, "Name & Title Boxes")
	if g == nil {
		return
	}
	sh := g.Find("Shape")
	if sh == nil {
		return
	}
	want := pick(sh, framePrefs(st.role))
	if want == nil {
		return
	}
	var show, hide [][]string
	if !want.Visible {
		show = append(show, want.Path())
	}
	for _, c := range sh.Children {
		if c != want && c.Visible {
			hide = append(hide, c.Path())
		}
	}
	if len(show) == 0 && len(hide) == 0 {
		return
	}
	for i := range a.recipe.Parts {
		p := &a.recipe.Parts[i]
		if p.Rule.Source != st.src.Key || p.Rule.Render == nil {
			continue
		}
		if p.Rule.Dir != st.src.Key+"/name_title_boxes" && p.Rule.Dir != st.src.Key+"/name_title_boxes_back" {
			continue
		}
		p.Rule.Render.Show = append(p.Rule.Render.Show, show...)
		p.Rule.Render.Hide = append(p.Rule.Render.Hide, hide...)
	}
}

// sagaBanner adds the saga's banner and stripe, colored by the pinlines, in
// the place of the Saga group with the parts already read from it
func (a *analyzer) sagaBanner(st *layoutState) error {
	g := rootGroup(st.f, "Saga")
	banner, stripe := g.Find("Banner"), g.Find("Stripe")
	if banner == nil || stripe == nil {
		a.note("%s: the Saga group has no Banner or Stripe, so the saga has no banner", st.src.Name)
		return nil
	}
	r := &Render{Target: g.Path(), Show: [][]string{g.Path()}, Variants: Object{}}
	trim := pick(g, framePrefs(st.role))
	for _, c := range g.Children {
		switch {
		case c == banner || c == stripe:
		case c == trim:
			r.Show = append(r.Show, c.Path())
		case c.Visible:
			r.Hide = append(r.Hide, c.Path())
		}
	}
	keys := []string{"wu", "ub", "br", "rg", "wg", "wb", "bg", "ug", "ur", "wr"}
	for k := range sagaBannerColors {
		keys = append(keys, k)
	}
	sortKeys(keys)
	for _, k := range keys {
		bf := RenderFill{Into: shapeHolder(banner).Path(), Clip: true}
		sf := RenderFill{Into: shapeHolder(stripe).Path(), Clip: true}
		if c, ok := sagaBannerColors[k]; ok {
			bf.Color, sf.Color = c.hex(), sagaStripeColors[k].hex()
		} else {
			l, rt := pairRun(k)
			bf.Gradient = &RenderGradient{Stops: []GradientStop{
				{Color: sagaBannerColors[l].hex(), Location: pairStops[0]},
				{Color: sagaBannerColors[rt].hex(), Location: pairStops[1]},
			}}
			sf.Color = sagaStripeDual.hex()
		}
		if err := r.Variants.Set(k, RenderVariant{Fills: []RenderFill{bf, sf}}); err != nil {
			return err
		}
	}
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	a.addPart(st, g, "saga_banner", "pinlines", "", r)
	es := a.takeEntries(st, start, firstPart, "", "")

	// The divider read from the group goes back above the banner
	pos := rootIndex(st.f, g)
	kept := st.entries[:0:0]
	for _, e := range st.entries {
		if rootIndex(st.f, a.entryRoot(st, e)) == pos {
			es = append(es, e)
			continue
		}
		kept = append(kept, e)
	}
	st.entries = kept
	a.placeEntries(st, g, es)
	return nil
}

// textboxRight moves the textbox onto the right half, centered on the art
// frame there, the way Proxyshop merges the Textbox group and moves it
func (a *analyzer) textboxRight(st *layoutState) {
	g, right := rootGroup(st.f, "Textbox"), st.f.Root.Find("Art Frame Right")
	if g == nil || right == nil {
		return
	}
	var bounds image.Rectangle
	for _, s := range g.Children {
		if !isShapeGroup(s) {
			continue
		}
		reach := styleReach(s)
		for _, c := range s.Children {
			if c.Visible {
				bounds = bounds.Union(c.Rect.Inset(-max(reach, styleReach(c))))
			}
		}
	}
	if bounds.Empty() {
		a.note("%s: the Textbox group has no shape to measure, so the class's textbox stays on the left", st.src.Name)
		return
	}
	ref := referenceRect(right)
	at := bounds.Min.Add(image.Pt((ref.Min.X+ref.Max.X)/2-(bounds.Min.X+bounds.Max.X)/2, 0))
	moved := map[string]bool{}
	for i := range a.recipe.Parts {
		p := &a.recipe.Parts[i]
		if d := p.Rule.Dir; p.Rule.Source == st.src.Key && (d == st.src.Key+"/textbox" || d == st.src.Key+"/textbox_back") {
			p.Rule.Crop = rectOf(bounds)
			moved[p.Name] = true
		}
	}
	for i := range st.entries {
		if moved[st.entries[i].part] {
			pt := at
			st.entries[i].at = &pt
		}
	}
}

// setArt makes a reference the layout's art window, beneath the stack or
// above the entry the art follows
func (st *layoutState) setArt(ref *psd.Layer) {
	if ref == nil {
		return
	}
	r := referenceRect(ref)
	if r.Empty() {
		return
	}
	o := obj("x", r.Min.X, "y", r.Min.Y, "width", r.Dx(), "height", r.Dy())
	if st.artAfter != "" {
		o = append(o, obj("after", st.artAfter)...)
	}
	st.art = []Object{o}
}
