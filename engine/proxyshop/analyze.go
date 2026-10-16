package proxyshop

import (
	"fmt"
	"image"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/odevine/mimic/engine/psd"
)

// Source is one PSD that Analyze reads
type Source struct {
	// Key names the source in the recipe and prefixes its parts, such as
	// "normal" or "front"
	Key string
	// Name is the PSD's path inside the folder Build opens sources from
	Name string
	File *psd.File
	// Serves is the shapes the source's layout draws, guessed from Name when
	// it is empty
	Serves []Shape
}

// Shape is a role and kind a layout serves, as a layout file writes it
type Shape struct {
	Role string `json:"role"`
	Kind string `json:"kind"`
}

// Options name the template Analyze writes
type Options struct {
	ID          string
	Name        string
	Description string
	Version     string
	MinEngine   string
	// Progress hears how far Convert's build has come, and may be nil
	Progress Progress
}

// Analysis is a recipe Analyze wrote and what it could not follow
type Analysis struct {
	Recipe *Recipe
	// Notes list each layer the recipe leaves out or draws differently from
	// Proxyshop, so a person can review the conversion
	Notes []string
}

// Analyze reads Proxyshop PSDs and writes a recipe that converts them into one
// template with a layout per source. It follows the conventions Proxyshop's
// templates share: groups named for a frame element hold a hidden layer per
// color, references mark where text and symbols go, and the art sits in a
// layer named Layer 1
func Analyze(srcs []Source, opt Options) (*Analysis, error) {
	if len(srcs) == 0 {
		return nil, fmt.Errorf("no source PSD to analyze")
	}
	if !idPattern.MatchString(opt.ID) {
		return nil, fmt.Errorf("template id %q is not an owner and a name of lower case letters, digits and hyphens joined by a dot, like mimic.m15", opt.ID)
	}
	a := &analyzer{
		recipe: &Recipe{Format: RecipeFormat, Sources: map[string]string{}, Layouts: Object{}},
		used:   map[string]bool{},
	}
	if err := a.template(opt); err != nil {
		return nil, err
	}
	for _, s := range kindSources(srcs) {
		if s.Key == "" || !keyPattern.MatchString(s.Key) {
			return nil, fmt.Errorf("source key %q is not lower case letters, digits and underscores", s.Key)
		}
		if _, dup := a.recipe.Sources[s.Key]; dup {
			return nil, fmt.Errorf("source key %q is used twice", s.Key)
		}
		a.recipe.Sources[s.Key] = s.Name
		layout, err := a.layout(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		if err := a.recipe.Layouts.Set(s.Key, layout); err != nil {
			return nil, err
		}
	}
	if err := a.contract(); err != nil {
		return nil, err
	}
	if err := a.recipe.Check(); err != nil {
		return nil, err
	}
	sort.Strings(a.notes)
	return &Analysis{Recipe: a.recipe, Notes: a.notes}, nil
}

type analyzer struct {
	recipe *Recipe
	notes  []string
	used   map[string]bool
}

func (a *analyzer) note(format string, args ...any) {
	a.notes = append(a.notes, fmt.Sprintf(format, args...))
}

func (a *analyzer) template(opt Options) error {
	t := Object{}
	version := opt.Version
	if version == "" {
		version = "0.1.0"
	}
	fields := []struct {
		k string
		v any
	}{
		{"contract", "1.0"},
		{"id", opt.ID},
		{"version", version},
	}
	if opt.MinEngine != "" {
		fields = append(fields, struct {
			k string
			v any
		}{"minEngine", opt.MinEngine})
	}
	fields = append(fields, []struct {
		k string
		v any
	}{
		{"name", opt.Name},
		{"description", opt.Description},
		{"author", map[string]string{"name": "mimic"}},
		{"license", "CC BY-NC 4.0 for the derivative work: the cuts, blends, layouts, and manifest. The art stays with its original authors."},
		{"credits", []map[string]string{{
			"name": "Proxyshop",
			"url":  "https://github.com/Investigamer/Proxyshop",
			"note": "The frame art is cut from a template Proxyshop distributes, which remains the work of its original designers and is redistributed with attribution",
		}}},
	}...)
	for _, f := range fields {
		if s, ok := f.v.(string); ok && s == "" {
			continue
		}
		if err := t.Set(f.k, f.v); err != nil {
			return err
		}
	}
	a.recipe.Template = t
	return nil
}

// GuessShapes reads the shapes a Proxyshop PSD serves from its file name
func GuessShapes(name string) []Shape {
	base := strings.ToLower(strings.TrimSuffix(path.Base(strings.ReplaceAll(name, "\\", "/")), path.Ext(name)))
	words := strings.FieldsFunc(base, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
	has := func(ws ...string) bool {
		for _, w := range words {
			for _, x := range ws {
				if w == x {
					return true
				}
			}
		}
		return false
	}
	kind := "standard"
	switch {
	case has("basic"):
		kind = "basic_land"
	case has("pw", "planeswalker") || strings.HasSuffix(base, "pw"):
		kind = "planeswalker"
	case has("saga"):
		kind = "saga"
	case has("class"):
		kind = "class"
	case has("leveler"):
		kind = "leveler"
	case has("mutate"):
		kind = "mutate"
	case has("prototype"):
		kind = "prototype"
	case has("battle"):
		kind = "battle"
	case has("planar"):
		kind = "plane"
	case has("token"):
		kind = "token"
	}
	role := "single"
	switch {
	case has("split"):
		role = "split"
	case has("adventure"):
		role = "adventure"
	case has("mdfc") && has("back"):
		role = "mdfc_back"
	case has("mdfc"):
		role = "mdfc_front"
	case has("tf", "transform") && has("back"):
		role = "transform_back"
	case has("tf", "transform") && has("front"):
		role = "transform_front"
	}
	if kind == "battle" && role == "single" {
		role = "transform_front"
	}
	return []Shape{{Role: role, Kind: kind}}
}

// stackEntry is one entry of a layout's stack as Analyze builds it
type stackEntry struct {
	id, part, condition, mode string
	opacity                   float64
	// face is which face of a two faced card the entry reads, and at moves it
	// from its part's origin
	face string
	at   *image.Point
}

// layoutState gathers what a source's layout needs while its layers are read
type layoutState struct {
	src     Source
	f       *psd.File
	entries []stackEntry
	ids     map[string]bool
	// artAfter is the entry the art sits above, empty for beneath the stack
	artAfter string
	artSeen  bool
	// has records which frame groups the document holds, by slug
	has map[string]bool
	// divider is the part drawn between rules and flavor text
	divider string
	// basics are the layers of a basic land template, one per land, by key
	basics, snowBasics map[string]*psd.Layer
	basicsAt           int
	// role is the first role the layout serves
	role string
	// boxes and art replace the text boxes and art windows read the usual way,
	// for a layout such as a split card's that has its own
	boxes Object
	art   []Object
	// claimed are root layers a card kind's own reading takes, which the
	// usual reading leaves alone
	claimed map[*psd.Layer]bool
	// moreBoxes are text boxes a card kind adds to those read the usual way,
	// and sections its layout's sections
	moreBoxes Object
	sections  Object
	// canvas and symbol replace the canvas and set symbol box read the usual
	// way, for a kind whose PSD keeps them elsewhere
	canvas, symbol Object
	// maskBlend is the blend the document's Masks/1/2 ramp makes, read once
	maskBlend     *PairFit
	maskBlendRead bool
}

// pairMask is the blend Proxyshop gives a pair of colors that has no pair
// layer: the first color's layer over the second's with the ramp of Masks/1/2
// as its mask
func (st *layoutState) pairMask(a *analyzer) *PairFit {
	if st.maskBlendRead {
		return st.maskBlend
	}
	st.maskBlendRead = true
	st.maskBlend = st.rampBlend(a, st.f.Root.Find("Masks", "1/2"))
	return st.maskBlend
}

// rampBlend is the blend a ramp mask such as Masks/1/2 gives a pair of colors,
// or nil when the layer has no mask
func (st *layoutState) rampBlend(a *analyzer, m *psd.Layer) *PairFit {
	if m == nil || m.Mask == nil {
		return nil
	}
	img, err := m.MaskImage()
	if err != nil {
		a.note("%s: the %s ramp did not read: %v", st.src.Name, strings.Join(m.Path(), "/"), err)
		return nil
	}
	r := m.Mask.Rect
	y := (r.Min.Y + r.Max.Y) / 2
	def := float64(m.Mask.DefaultColor) / 255
	at := func(x int) float64 {
		if r.Empty() || x < r.Min.X || x >= r.Max.X {
			return def
		}
		return float64(img.GrayAt(x, y).Y) / 255
	}
	return MaskBlend(at, st.f.Width)
}

func (a *analyzer) layout(s Source) (Object, error) {
	f := s.File
	st := &layoutState{src: s, f: f, ids: map[string]bool{}, has: map[string]bool{}}
	serves := s.Serves
	if len(serves) == 0 {
		serves = GuessShapes(s.Name)
	}
	st.role = serves[0].Role
	// Children run top to bottom, and the stack bottom to top
	root := f.Root.Children
	for i := len(root) - 1; i >= 0; i-- {
		st.has[slug(root[i].Name)] = true
	}
	if err := a.kindBefore(st); err != nil {
		return nil, err
	}
	if isSplit(f) {
		if err := a.split(st); err != nil {
			return nil, err
		}
	} else {
		for i := len(root) - 1; i >= 0; i-- {
			if err := a.rootLayer(st, root[i]); err != nil {
				return nil, err
			}
		}
		if err := a.textIcons(st); err != nil {
			return nil, err
		}
		if err := a.basicLands(st); err != nil {
			return nil, err
		}
	}
	if err := a.kindAfter(st); err != nil {
		return nil, err
	}

	out := Object{}
	set := func(k string, v any) error { return out.Set(k, v) }
	if err := set("serves", serves); err != nil {
		return nil, err
	}
	canvas := obj("width", f.Width, "height", f.Height)
	if st.canvas != nil {
		canvas = st.canvas
	} else if f.Width > f.Height {
		// A landscape frame is authored on its side, at the resolution of its
		// short side, and turned upright once drawn
		canvas = append(canvas, obj("dpi", int(math.Round(float64(f.Height)/2.72)), "rotate", 270)...)
	}
	if err := set("canvas", canvas); err != nil {
		return nil, err
	}
	var stack []Object
	for _, e := range st.entries {
		o := Object{}
		o.Set("id", e.id)
		o.Set("part", e.part)
		if e.condition != "" {
			o.Set("condition", e.condition)
		}
		if e.face != "" {
			o.Set("face", e.face)
		}
		if e.at != nil {
			o.Set("at", obj("x", e.at.X, "y", e.at.Y))
		}
		if e.mode != "" {
			o.Set("mode", e.mode)
		}
		if e.opacity > 0 && e.opacity < 1 {
			o.Set("opacity", math.Round(e.opacity*1000)/1000)
		}
		stack = append(stack, o)
	}
	if err := set("stack", stack); err != nil {
		return nil, err
	}
	boxes := st.boxes
	if boxes == nil {
		var err error
		if boxes, err = a.textBoxes(st); err != nil {
			return nil, err
		}
	}
	boxes = append(boxes, st.moreBoxes...)
	if err := set("textBoxes", boxes); err != nil {
		return nil, err
	}
	if len(st.sections) > 0 {
		if err := set("sections", st.sections); err != nil {
			return nil, err
		}
	}
	if st.art != nil {
		if err := set("art", st.art); err != nil {
			return nil, err
		}
	} else if art := artWindow(f, st.artAfter, st.borderless()); art != nil {
		if err := set("art", []Object{art}); err != nil {
			return nil, err
		}
	} else {
		a.note("%s: no art frame reference, so the layout has no art window", s.Name)
	}
	sym := st.symbol
	if sym == nil {
		sym = setSymbol(f)
	}
	if sym != nil {
		if err := set("symbols", Object{{Key: "set", Value: mustRaw(sym)}}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// idPattern is the form of a template id
var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*\.[a-z0-9]+(-[a-z0-9]+)*$`)

// skipped are root layers that hold no frame art: references, the art, the
// text, and helpers Proxyshop reads or copies from
var skipped = regexp.MustCompile(`(?i)^(full art frame|art frame.*|borderless frame|scryfall scan frame|border crop|trim reference|text and icons|legal|masks|effects|expansion reference|expansion symbol|.*reference.*)$`)

// The groups each slot reads, by slug
var groupSlots = map[string]string{
	"background":               "background",
	"nyx":                      "background",
	"pinlines_textbox":         "pinlines",
	"land_pinlines_textbox":    "pinlines",
	"pinlines":                 "pinlines",
	"pinlines_saga_stripe":     "pinlines",
	"textbox":                  "pinlines",
	"companion":                "pinlines",
	"snow":                     "pinlines",
	"land":                     "pinlines",
	"nonland":                  "pinlines",
	"name_title_boxes":         "twins",
	"twins":                    "twins",
	"tf_twins":                 "twins",
	"legendary_crown":          "crown",
	"pt_box":                   "ptBox",
	"pt_and_level_boxes":       "ptBox",
	"color_indicator":          "indicator",
	"pinlines_textbox_dark":    "pinlines",
	"mdfc_pinlines_textbox":    "pinlines",
	"textbox_beyond":           "pinlines",
	"background_beyond":        "background",
	"prototype_textbox":        "pinlines",
	"prototype_pt_box":         "ptBox",
	"prototype_manabox":        "twins",
	"pt_box_dark":              "ptBox",
	"pt_box_alternate":         "ptBox",
	"legendary_crown_dark":     "crown",
	"name_title_boxes_dark":    "twins",
	"pinlines_textbox_classic": "pinlines",
}

// optional groups are alternatives Proxyshop shows only for a setting or a card
// the engine has no condition for, so a conversion leaves them out
var optional = map[string]string{
	"pinlines_textbox_dark": "a dark mode setting",
	"pt_box_dark":           "a dark mode setting",
	"pt_box_alternate":      "an alternate setting",
	"mdfc_pinlines_textbox": "modal double faced cards",
	"textbox_beyond":        "the Universes Beyond setting",
	"background_beyond":     "the Universes Beyond setting",
	"prototype_textbox":     "prototype cards",
	"prototype_pt_box":      "prototype cards",
	"prototype_manabox":     "prototype cards",
	"hollow_crown_shadow":   "the hollow crown of a legendary nyx or companion card",
	"overlay":               "a reference Proxyshop does not draw",
}

// condition is when a group draws, from its slug and the groups beside it
func (st *layoutState) condition(s string) string {
	switch s {
	case "nyx":
		return "nyx"
	case "background":
		if st.has["nyx"] {
			return "nonnyx"
		}
	case "land_pinlines_textbox", "land":
		return "land"
	case "pinlines_textbox", "nonland":
		if st.has["land_pinlines_textbox"] || st.has["land"] {
			return "nonland"
		}
	case "legendary_crown", "legendary_shading", "legendary_shadows", "legendary_shadow":
		return "legendary"
	case "companion":
		return "companion"
	case "color_indicator":
		return "color_indicator"
	case "pt_box":
		return "creature"
	}
	return ""
}

// colorKeys are the variant keys a frame slot can ask for
var colorKeys = map[string]bool{
	"gold": true, "artifact": true, "colorless": true, "land": true, "vehicle": true,
}

// variantKey reads a color layer's name as a variant key and the word after
// it, such as "Back" in "W Back". It reports false for a layer that is not a
// color
func variantKey(name string) (key, suffix string, ok bool) {
	n := strings.TrimSpace(name)
	n = strings.ReplaceAll(n, "-", " ")
	n = strings.ReplaceAll(n, "Colourless", "Colorless")
	fields := strings.Fields(n)
	if len(fields) == 0 {
		return "", "", false
	}
	head := fields[0]
	rest := strings.ToLower(strings.Join(fields[1:], "_"))
	k := NormalizeKey(head)
	if isColorCombo(k) && head == strings.ToUpper(head) && len(k) <= 5 {
		return k, rest, true
	}
	if colorKeys[k] {
		return k, rest, true
	}
	return "", "", false
}

func (a *analyzer) rootLayer(st *layoutState, l *psd.Layer) error {
	name := strings.TrimSpace(l.Name)
	s := slug(name)
	src := st.src
	if st.claimed[l] {
		return nil
	}
	switch {
	case name == "Layer 1" && !l.IsGroup():
		st.artSeen = true
		if n := len(st.entries); n > 0 {
			st.artAfter = st.entries[n-1].id
		}
		return nil
	case skipped.MatchString(name):
		return nil
	case l.Kind == psd.KindAdjustment:
		a.note("%s: the adjustment layer %q changes everything below it, which a template cannot do, so it is left out", src.Name, name)
		return nil
	case l.Kind == psd.KindText:
		return nil
	}
	if key, snow, ok := basicLand(name); ok {
		if st.basics == nil {
			st.basics, st.snowBasics = map[string]*psd.Layer{}, map[string]*psd.Layer{}
			st.basicsAt = len(st.entries)
			// The basics take their place in the stack once they are all read
			st.entries = append(st.entries, stackEntry{})
		}
		if snow {
			st.snowBasics[key] = l
		} else {
			st.basics[key] = l
		}
		return nil
	}
	if why, ok := optional[s]; ok {
		a.note("%s: %q is left out, since Proxyshop shows it only for %s", src.Name, name, why)
		return nil
	}
	if l.IsGroup() {
		colors := colorChildren(l)
		if _, known := groupSlots[s]; known && len(colors) == 0 {
			colors = colorChildrenAtLeast(l, 1)
		}
		if len(colors) > 0 {
			return a.colorGroup(st, l, colors)
		}
		if s == "border" {
			return a.border(st, l)
		}
		if slot, known := groupSlots[s]; known && hasShape(l) {
			return a.fillGroup(st, l, s, slot)
		}
	}
	if !l.Visible {
		if st.condition(s) == "" {
			a.note("%s: the hidden layer %q is left out, since nothing says when Proxyshop shows it", src.Name, name)
			return nil
		}
	}
	return a.static(st, l, s, st.condition(s), nil)
}

// colorChildren are a group's layers that are named for a color, when it has
// enough of them to be a group of colors rather than one that happens to hold
// a layer named like one
func colorChildren(g *psd.Layer) []*psd.Layer { return colorChildrenAtLeast(g, 3) }

func colorChildrenAtLeast(g *psd.Layer, n int) []*psd.Layer {
	var out []*psd.Layer
	for _, c := range g.Children {
		if _, _, ok := variantKey(c.Name); ok {
			out = append(out, c)
		}
	}
	if len(out) < n {
		return nil
	}
	return out
}

// partName makes a unique part name for a source
func (a *analyzer) partName(src, s string) string {
	base := src + "_" + s
	name := base
	for i := 2; a.used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	a.used[name] = true
	return name
}

func (st *layoutState) entryID(s string) string {
	id := s
	for i := 2; st.ids[id]; i++ {
		id = fmt.Sprintf("%s_%d", s, i)
	}
	st.ids[id] = true
	return id
}

// entryMode is the mode and opacity a part draws with in the template for a
// layer of the PSD, and false when the template cannot draw its mode
func entryMode(l *psd.Layer) (string, bool) {
	switch l.Blend {
	case psd.BlendNormal, psd.BlendPassThrough:
		return "", true
	case psd.BlendMultiply, psd.BlendScreen, psd.BlendOverlay, psd.BlendDarken, psd.BlendLighten,
		psd.BlendDifference, psd.BlendExclusion, psd.BlendHue, psd.BlendSaturation, psd.BlendColor, psd.BlendLuminosity:
		return l.Blend.String(), true
	case psd.BlendSoftLight:
		return "softlight", true
	case psd.BlendHardLight:
		return "hardlight", true
	case psd.BlendColorDodge:
		return "colordodge", true
	case psd.BlendColorBurn:
		return "colorburn", true
	}
	return "", false
}

func (a *analyzer) addPart(st *layoutState, l *psd.Layer, s, slot, condition string, r *Render) {
	src := st.src
	name := a.partName(src.Key, s)
	mode, ok := entryMode(l)
	if !ok {
		a.note("%s: %q blends in %s mode, which a template cannot draw, so it draws in normal mode", src.Name, l.Name, l.Blend)
	}
	if l.IsGroup() && l.Parent != nil && l.Parent.Parent == nil {
		prefs := st.prefs(s)
		r.Show = append(r.Show, shapeShows(l, prefs)...)
		if mc := maskCopy(st.f, l, prefs); mc != nil {
			r.CopyMasks = append(r.CopyMasks, *mc)
		}
	}
	a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{
		Source: src.Key,
		Dir:    src.Key + "/" + s,
		Slot:   slot,
		Render: r,
	}})
	st.entries = append(st.entries, stackEntry{id: st.entryID(s), part: name, condition: condition, mode: mode})
}

// static adds a layer or group that does not change with the card, drawn as
// the PSD shows it with the overrides given
func (a *analyzer) static(st *layoutState, l *psd.Layer, s, condition string, show [][]string) error {
	r := &Render{Target: l.Path(), Variants: Object{{Key: "any", Value: mustRaw(RenderVariant{})}}}
	if !l.Visible {
		r.Show = append(r.Show, l.Path())
	}
	r.Show = append(r.Show, show...)
	a.addPart(st, l, s, "", condition, r)
	return nil
}

// border adds a Border group's layers each as a part, the normal border for a
// card that is not legendary and the legendary one for a card that is, the way
// Proxyshop swaps them. A group that styles or masks its layers draws whole
func (a *analyzer) border(st *layoutState, g *psd.Layer) error {
	plain := g.Effects == nil && g.Mask == nil && g.VectorMask == nil && g.Opacity == 255 &&
		(g.Blend == psd.BlendPassThrough || g.Blend == psd.BlendNormal)
	var legendary, normal *psd.Layer
	for _, c := range g.Children {
		n := strings.ToLower(c.Name)
		switch {
		case strings.Contains(n, "legendary"):
			legendary = c
		case strings.Contains(n, "normal"):
			normal = c
		}
	}
	if !plain || legendary == nil || normal == nil {
		if legendary != nil && normal != nil {
			for _, v := range []struct {
				keep, drop *psd.Layer
				s, cond    string
			}{{normal, legendary, "border", "nonlegendary"}, {legendary, normal, "border_legendary", "legendary"}} {
				r := &Render{Target: g.Path(), Show: [][]string{v.keep.Path()}, Hide: [][]string{v.drop.Path()}, Variants: anyVariant()}
				if !g.Visible {
					r.Show = append(r.Show, g.Path())
				}
				a.addPart(st, g, v.s, "", v.cond, r)
			}
			return nil
		}
		return a.static(st, g, "border", "", nil)
	}
	for i := len(g.Children) - 1; i >= 0; i-- {
		c := g.Children[i]
		s, cond := "border_"+slug(c.Name), ""
		switch c {
		case normal:
			s, cond = "border", "nonlegendary"
		case legendary:
			s, cond = "border_legendary", "legendary"
		default:
			// Normal and legendary already cover every card between them, so
			// an unconditional third layer here would always draw over
			// whichever of those is right for a given card, except the one
			// Proxyshop itself marks "(leave on)" to say it means to keep
			// regardless. A further alternate with no such marker, such as a
			// plain "Full Art Border", is one this conversion has no way to
			// tell from the ordinary one, so it is left out rather than
			// drawn over every card
			leaveOn := strings.Contains(strings.ToLower(c.Name), "leave on")
			switch {
			case leaveOn:
				s = "border_" + strings.TrimSuffix(strings.TrimSuffix(slug(c.Name), "_leave_on"), "_border")
			case c.Visible:
				a.note("%s: the border layer %q is neither the normal nor the legendary one and is left out, since drawing it for every card would cover whichever of those is right", st.src.Name, c.Name)
				continue
			default:
				a.note("%s: the hidden border layer %q is left out", st.src.Name, c.Name)
				continue
			}
		}
		r := &Render{Target: c.Path(), Show: [][]string{c.Path()}, Variants: anyVariant()}
		a.addPart(st, c, s, "", cond, r)
	}
	return nil
}

func anyVariant() Object { return Object{{Key: "any", Value: mustRaw(RenderVariant{})}} }

// colorGroup adds a group with a layer per color, one part for the plain
// colors and one for each set of colors with a word after them, such as the
// "W Back" layers of a group that also has "W"
func (a *analyzer) colorGroup(st *layoutState, g *psd.Layer, colors []*psd.Layer) error {
	src := st.src
	s := slug(g.Name)
	slot, ok := groupSlots[s]
	if !ok {
		slot = "pinlines"
		a.note("%s: the color group %q is not one Proxyshop's templates name, so it reads the pinline colors", src.Name, g.Name)
	}
	type set struct {
		suffix string
		keys   []string
		layers map[string]*psd.Layer
	}
	sets := map[string]*set{}
	var order []string
	for i := len(colors) - 1; i >= 0; i-- {
		c := colors[i]
		key, suffix, _ := variantKey(c.Name)
		ss, ok := sets[suffix]
		if !ok {
			ss = &set{suffix: suffix, layers: map[string]*psd.Layer{}}
			sets[suffix] = ss
			order = append(order, suffix)
		}
		if _, dup := ss.layers[key]; dup {
			a.note("%s: %q has two layers for %q, and the lower one is used", src.Name, g.Name, key)
			continue
		}
		ss.layers[key] = c
		ss.keys = append(ss.keys, key)
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i] == "" && order[j] != "" })
	base := st.condition(s)
	for _, suffix := range order {
		ss := sets[suffix]
		cond := base
		ps := s
		if suffix != "" {
			sc, ok := suffixCondition(suffix)
			if !ok {
				a.note("%s: the %q layers of %q are left out, since nothing says when Proxyshop shows them", src.Name, suffix, g.Name)
				continue
			}
			cond = joinConditions(base, sc)
			ps = s + "_" + suffix
		}
		r := &Render{Target: g.Path(), Variants: Object{}, FitPairs: true}
		if !g.Visible {
			r.Show = append(r.Show, g.Path())
		}
		sort.SliceStable(ss.keys, func(i, j int) bool { return keyRank(ss.keys[i]) < keyRank(ss.keys[j]) })
		for _, key := range ss.keys {
			// An indicator's images are named by the colors they show
			if slot == "indicator" && !isColorCombo(key) {
				a.note("%s: the %q layer of %q is left out, since an indicator shows only a card's colors", src.Name, ss.layers[key].Name, g.Name)
				continue
			}
			v := RenderVariant{Show: [][]string{ss.layers[key].Path()}}
			for _, c := range colors {
				if c != ss.layers[key] && c.Visible {
					v.Hide = append(v.Hide, c.Path())
				}
			}
			if err := r.Variants.Set(key, v); err != nil {
				return err
			}
		}
		a.addPart(st, g, ps, slot, cond, r)
		if fit := st.pairMask(a); fit != nil && !hasPair(ss.keys) && countMonos(ss.keys) >= 2 {
			a.recipe.Parts[len(a.recipe.Parts)-1].Rule.Blend = mustRaw(fit.blend())
		}
	}
	return nil
}

func hasPair(keys []string) bool {
	for _, k := range keys {
		if len(k) == 2 && isColorCombo(k) {
			return true
		}
	}
	return false
}

func countMonos(keys []string) int {
	n := 0
	for _, k := range keys {
		if len(k) == 1 && isColorCombo(k) {
			n++
		}
	}
	return n
}

// sortKeys puts variant keys in the order they are listed
func sortKeys(keys []string) {
	sort.SliceStable(keys, func(i, j int) bool {
		if keyRank(keys[i]) != keyRank(keys[j]) {
			return keyRank(keys[i]) < keyRank(keys[j])
		}
		return keys[i] < keys[j]
	})
}

// prefs are the shape and mask names a group of this layout looks for
func (st *layoutState) prefs(s string) []string {
	prefs := framePrefs(st.role)
	base := strings.TrimSuffix(path.Base(st.src.Name), path.Ext(st.src.Name))
	prefs = append(append([]string(nil), templatePrefs[base]...), prefs...)
	if s == "legendary_crown" {
		prefs = append([]string{"Legendary"}, prefs...)
	}
	return prefs
}

// colorMap is a template's own colors for a group Proxyshop fills, or nil for
// the defaults
func (st *layoutState) colorMap(s string) map[string]rgbColor {
	base := strings.TrimSuffix(path.Base(st.src.Name), path.Ext(st.src.Name))
	return templateColors[base][s]
}

// keyOrder is the order variants are listed in: colors, pairs, then the rest
var keyOrder = []string{"w", "u", "b", "r", "g", "wu", "ub", "br", "rg", "wg", "wb", "bg", "ug", "ur", "wr", "gold", "artifact", "colorless", "land", "vehicle"}

func keyRank(k string) int {
	for i, o := range keyOrder {
		if o == k {
			return i
		}
	}
	return len(keyOrder) + len(k)
}

// suffixCondition is when Proxyshop shows the layers named with a word after
// their color
func suffixCondition(suffix string) (string, bool) {
	switch suffix {
	case "back":
		return "back", true
	case "land":
		return "land", true
	case "nyx":
		return "nyx", true
	}
	return "", false
}

func joinConditions(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "," + b
}

// slug turns a layer name into a lower case name of letters, digits and
// underscores
func slug(name string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = false
			b.WriteRune(r)
		default:
			under = true
		}
	}
	if b.Len() == 0 {
		return "layer"
	}
	return b.String()
}

// textIcons adds the frame pieces kept among the text: the divider and a
// transform icon with its circle
func (a *analyzer) textIcons(st *layoutState) error {
	ti := st.f.Root.Find("Text and Icons")
	if ti == nil {
		return nil
	}
	if d := ti.Find("Divider"); d != nil {
		name := a.partName(st.src.Key, "divider")
		r := &Render{Target: d.Path(), Show: [][]string{d.Path()}, Variants: Object{{Key: "any", Value: mustRaw(RenderVariant{})}}}
		a.recipe.Parts = append(a.recipe.Parts, NamedPart{Name: name, Rule: PartRule{Source: st.src.Key, Dir: st.src.Key + "/divider", Render: r}})
		st.divider = name
	}
	tf := ti.Find("Transform")
	if tf == nil || !tf.IsGroup() {
		return nil
	}
	// Proxyshop shows the transform icon only on the faces of a transform card
	if !strings.HasPrefix(st.role, "transform") {
		return nil
	}
	if c := tf.Find("Circle"); c != nil {
		r := &Render{Target: c.Path(), Variants: Object{{Key: "any", Value: mustRaw(RenderVariant{})}}}
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
		if len(r.Variants) > 0 {
			a.addPart(st, icons, "transform_icon_"+strings.ToLower(side), "transform_icon", "", r)
		}
	}
	return nil
}

// borderless reports a frame with no background, whose art Proxyshop fills
// out to the whole card
func (st *layoutState) borderless() bool {
	for _, e := range st.entries {
		if strings.Contains(e.id, "background") || e.id == "nyx" {
			return false
		}
	}
	return true
}

// artWindow is where the art goes: the Art Frame reference's outline, or the
// Full Art Frame's for a borderless frame
func artWindow(f *psd.File, after string, borderless bool) Object {
	ref := f.Root.Find("Art Frame")
	if full := f.Root.Find("Full Art Frame"); borderless && full != nil {
		ref = full
	}
	if ref == nil {
		for _, n := range []string{"Art Frame Normal", "Full Art Frame", "Art Frame Left"} {
			if ref = f.Root.Find(n); ref != nil {
				break
			}
		}
	}
	if ref == nil {
		return nil
	}
	r := referenceRect(ref)
	if r.Empty() {
		return nil
	}
	o := Object{}
	o.Set("x", r.Min.X)
	o.Set("y", r.Min.Y)
	o.Set("width", r.Dx())
	o.Set("height", r.Dy())
	if after != "" {
		o.Set("after", after)
	}
	return o
}

// referenceRect is a reference layer's box: its pixels' bounds, which is what
// Proxyshop measures, or its vector outline's when it has no pixels
func referenceRect(l *psd.Layer) image.Rectangle {
	if !l.Rect.Empty() {
		return l.Rect
	}
	if l.VectorMask != nil {
		var r image.Rectangle
		first := true
		for _, sp := range l.VectorMask.Subpaths {
			for _, k := range sp.Knots {
				p := image.Pt(int(math.Round(k.Anchor.X)), int(math.Round(k.Anchor.Y)))
				if first {
					r = image.Rectangle{Min: p, Max: p}
					first = false
					continue
				}
				r.Min.X, r.Min.Y = min(r.Min.X, p.X), min(r.Min.Y, p.Y)
				r.Max.X, r.Max.Y = max(r.Max.X, p.X), max(r.Max.Y, p.Y)
			}
		}
		if !r.Empty() {
			return r
		}
	}
	return l.Rect
}

// setSymbol is where the expansion symbol goes: the Expansion Reference box
func setSymbol(f *psd.File) Object {
	ref := f.Root.Find("Text and Icons", "Expansion Reference")
	if ref == nil {
		ref = f.Root.Find("Expansion Reference")
	}
	if ref == nil {
		return nil
	}
	r := referenceRect(ref)
	if r.Empty() {
		return nil
	}
	// Proxyshop scales the symbol to the reference's height and lets a wide one
	// run left from its right edge, so the box reaches well to the left
	w := r.Dy() * 5 / 2
	return obj("x", r.Max.X-w, "y", r.Min.Y, "width", w, "height", r.Dy(), "align", "right", "vAlign", "center")
}

// basicTypes are the lands a basic land template has a layer for, by the key of
// the pinline color a basic of that type has
var basicTypes = map[string]string{
	"plains": "w", "island": "u", "swamp": "b", "mountain": "r", "forest": "g", "wastes": "colorless",
}

// basicLand reads a basic land layer's name
func basicLand(name string) (key string, snow, ok bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if rest, cut := strings.CutPrefix(n, "snow-covered "); cut {
		n, snow = rest, true
	}
	key, ok = basicTypes[n]
	return key, snow, ok
}

// basicLands adds a basic land template's land layers as one part read by the
// pinline color, which for a basic is its land type's color, and the snow
// covered layers as another drawn over it for a snow basic
func (a *analyzer) basicLands(st *layoutState) error {
	if st.basics == nil {
		return nil
	}
	at := st.basicsAt
	before := append([]stackEntry(nil), st.entries[:at]...)
	after := append([]stackEntry(nil), st.entries[at+1:]...)
	st.entries = before
	for _, set := range []struct {
		layers map[string]*psd.Layer
		s      string
		cond   string
	}{{st.basics, "basic", ""}, {st.snowBasics, "basic_snow", "snow"}} {
		if len(set.layers) == 0 {
			continue
		}
		keys := make([]string, 0, len(set.layers))
		for k := range set.layers {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool { return keyRank(keys[i]) < keyRank(keys[j]) })
		r := &Render{Variants: Object{}}
		var any *psd.Layer
		for _, k := range keys {
			l := set.layers[k]
			any = l
			if err := r.Variants.Set(k, RenderVariant{Target: l.Path(), Show: [][]string{l.Path()}}); err != nil {
				return err
			}
		}
		r.Target = any.Path()
		cond := set.cond
		if cond == "" && len(st.snowBasics) > 0 {
			cond = "nonsnow"
		}
		a.addPart(st, any, set.s, "pinlines", cond, r)
	}
	st.entries = append(st.entries, after...)
	return nil
}
