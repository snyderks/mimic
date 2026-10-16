package proxyshop

import (
	"fmt"
	"strings"

	"github.com/odevine/mimic/engine/psd"
)

// Vector templates keep each frame element's outline in a Shape group with a
// hidden shape layer per variant of the frame, such as Normal, TF Front or
// Tall, and color an element either with texture layers or with a fill layer
// Proxyshop adds at render time. Masks for each kind of frame wait in a root
// Masks group and are copied onto the element. The functions here pick the
// shapes and masks a face of a given role shows and make the fills

// MaskCopy copies one layer's mask onto another before a part draws
type MaskCopy struct {
	From []string `json:"from"`
	To   []string `json:"to"`
	// Vector copies the vector mask in place of the user mask
	Vector bool `json:"vector,omitempty"`
	// HidesEffects cuts the layer's styles with the mask as well
	HidesEffects bool `json:"hidesEffects,omitempty"`
}

// framePrefs are the names a face of each role looks for among the shape
// variants and the masks of a vector template, best first
func framePrefs(role string) []string {
	switch role {
	case "transform_front":
		return []string{"Normal TF Front", "TF Front", "Transform", "Front", "Normal"}
	case "transform_back":
		return []string{"Normal TF Back", "TF Back", "Transform", "Back", "Normal"}
	case "mdfc_front":
		return []string{"MDFC", "Normal TF Front", "TF Front", "Transform", "Front", "Normal"}
	case "mdfc_back":
		return []string{"MDFC", "Normal TF Back", "TF Back", "Transform", "Back", "Normal"}
	}
	return []string{"Normal"}
}

// pick finds the child of g a face shows, by the first name in prefs that a
// child has
func pick(g *psd.Layer, prefs []string) *psd.Layer {
	for _, p := range prefs {
		for _, c := range g.Children {
			if strings.EqualFold(strings.TrimSpace(c.Name), p) {
				return c
			}
		}
	}
	return nil
}

func isShapeGroup(l *psd.Layer) bool {
	return l.IsGroup() && strings.EqualFold(strings.TrimSpace(l.Name), "shape")
}

// hasShape reports whether a group holds a Shape group, directly or in a
// group inside it
func hasShape(g *psd.Layer) bool {
	for _, c := range g.Children {
		if isShapeGroup(c) || c.IsGroup() && hasShape(c) {
			return true
		}
	}
	return false
}

// shapeShows lists the shape layers under g that a face of the role shows: in
// each group of shapes whose layers are all hidden, the one the role names
func shapeShows(g *psd.Layer, prefs []string) [][]string {
	var out [][]string
	var choose func(x *psd.Layer)
	choose = func(x *psd.Layer) {
		anyShown := false
		for _, c := range x.Children {
			if c.Visible {
				anyShown = true
			}
		}
		if !anyShown {
			if c := pick(x, prefs); c != nil {
				out = append(out, c.Path())
				if c.IsGroup() {
					choose(c)
				}
			}
			return
		}
		for _, c := range x.Children {
			if c.Visible && c.IsGroup() {
				choose(c)
			}
		}
	}
	var walk func(x *psd.Layer)
	walk = func(x *psd.Layer) {
		for _, c := range x.Children {
			switch {
			case isShapeGroup(c):
				if !c.Visible {
					out = append(out, c.Path())
				}
				choose(c)
			case c.IsGroup() && hasShape(c):
				if !c.Visible && pick(x, prefs) == nil {
					out = append(out, c.Path())
				}
				walk(c)
			}
		}
	}
	walk(g)
	return out
}

// maskCopy is the mask Proxyshop copies onto a root group for a face of the
// role, from Masks/<group name>, or nil when the document keeps none for it
func maskCopy(f *psd.File, g *psd.Layer, prefs []string) *MaskCopy {
	from := f.Root.Find("Masks", g.Name)
	if from == nil || !from.IsGroup() {
		return nil
	}
	for from.IsGroup() {
		next := pick(from, prefs)
		if next == nil {
			return nil
		}
		from = next
	}
	if from.Mask == nil {
		return nil
	}
	return &MaskCopy{From: from.Path(), To: g.Path(), HidesEffects: slug(g.Name) == "pinlines"}
}

// rgbColor is a color from a Proxyshop color map
type rgbColor [3]int

func (c rgbColor) hex() string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

// pinlineColors is Proxyshop's default pinlines_color_map
var pinlineColors = map[string]rgbColor{
	"w": {246, 246, 239}, "u": {0, 117, 190}, "b": {39, 38, 36}, "r": {239, 56, 39}, "g": {0, 123, 67},
	"gold": {246, 210, 98}, "land": {174, 151, 135}, "artifact": {230, 236, 242}, "colorless": {230, 236, 242},
	"vehicle": {77, 45, 5},
}

// indicatorColors is Proxyshop's default indicator_color_map
var indicatorColors = map[string]rgbColor{
	"w": {248, 244, 240}, "u": {0, 109, 174}, "b": {57, 52, 49}, "r": {222, 60, 35}, "g": {0, 109, 66},
	"artifact": {181, 197, 205}, "colorless": {214, 214, 220}, "land": {165, 150, 132},
}

// pairStops are where Proxyshop's two color pinline gradient changes color
var pairStops = [2]float64{0.40, 0.60}

// fillGroup adds a group Proxyshop colors with a fill it makes at render time:
// a solid color per single color and a gradient per pair, at the top of the
// group, clipped to the shapes below it
func (a *analyzer) fillGroup(st *layoutState, g *psd.Layer, s, slot string) error {
	colors := pinlineColors
	if slot == "indicator" {
		colors = indicatorColors
	}
	if m := st.colorMap(s); m != nil {
		colors = m
	}
	into := shapeHolder(g)
	r := &Render{Target: g.Path(), Variants: Object{}, FitPairs: true}
	if !g.Visible {
		r.Show = append(r.Show, g.Path())
	}
	var keys []string
	for k := range colors {
		if slot != "ptBox" && k == "vehicle" {
			continue
		}
		// An indicator shows the card's colors and nothing else
		if slot == "indicator" && !isColorCombo(k) {
			continue
		}
		keys = append(keys, k)
	}
	if slot == "pinlines" || slot == "crown" || slot == "background" {
		for _, p := range []string{"wu", "ub", "br", "rg", "wg", "wb", "bg", "ug", "ur", "wr"} {
			keys = append(keys, p)
		}
	}
	sortKeys(keys)
	for _, k := range keys {
		var fill RenderFill
		fill.Into = into.Path()
		fill.Clip = true
		if c, ok := colors[k]; ok {
			fill.Color = c.hex()
		} else {
			l, rt := pairRun(k)
			fill.Gradient = &RenderGradient{Stops: []GradientStop{
				{Color: colors[l].hex(), Location: pairStops[0]},
				{Color: colors[rt].hex(), Location: pairStops[1]},
			}}
		}
		if err := r.Variants.Set(k, RenderVariant{Fills: []RenderFill{fill}}); err != nil {
			return err
		}
	}
	a.addPart(st, g, s, slot, st.condition(s), r)
	return nil
}

// shapeHolder is the group a fill goes into: the first group at or under g
// that holds a Shape group directly, which is where Proxyshop adds it
func shapeHolder(g *psd.Layer) *psd.Layer {
	for _, c := range g.Children {
		if isShapeGroup(c) {
			return g
		}
	}
	for _, c := range g.Children {
		if c.IsGroup() && hasShape(c) {
			return shapeHolder(c)
		}
	}
	return g
}

// templatePrefs are names a template looks for before those of its face, for
// templates whose code always picks a variant of the frame, by the PSD's name
var templatePrefs = map[string][]string{
	// ClassicModernTemplate is always extended
	"classic-modern": {"Extended"},
}
