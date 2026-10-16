package proxyshop

import (
	"github.com/odevine/mimic/engine/psd"
)

// Proxyshop's vector adventure frame keeps both pages of the book in a root
// Storybook group. The right page is the card's textbox, in the pinline
// colors, and the left page with the adventure's name and type plates takes
// the adventure's own colors. The wings behind the book are filled with a
// color map of their own

// adventureWingsColors is Proxyshop's adventure_wings_color_map
var adventureWingsColors = map[string]rgbColor{
	"w": {213, 203, 181}, "u": {181, 198, 213}, "b": {162, 155, 152}, "r": {192, 142, 115}, "g": {174, 174, 155},
	"gold": {196, 172, 131}, "land": {194, 178, 177},
}

// isStorybook is a vector adventure frame, whose pages sit in a root group
func isStorybook(f *psd.File) bool { return rootGroup(f, "Storybook") != nil }

func (a *analyzer) adventureBefore(st *layoutState) error {
	if !isStorybook(st.f) {
		return nil
	}
	st.claimed[rootGroup(st.f, "Storybook")] = true
	// The right page is the textbox, so the root Textbox group is not drawn
	if tb := rootGroup(st.f, "Textbox"); tb != nil {
		st.claimed[tb] = true
	}
	return nil
}

// storybook adds the book's layers in the place of the Storybook group, the
// parts that take the adventure's colors reading the card's second half
func (a *analyzer) storybook(st *layoutState) error {
	g := rootGroup(st.f, "Storybook")
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	for i := len(g.Children) - 1; i >= 0; i-- {
		c := g.Children[i]
		var err error
		switch c.Name {
		case "Pinlines":
			err = a.fillGroup(st, c, "pinlines", "pinlines")
		case "Wings":
			var vs Object
			if vs, err = fillVariants(c, adventureWingsColors, true, pairStops); err == nil {
				a.addPart(st, c, "wings", "pinlines", "", &Render{Target: c.Path(), Variants: vs})
			}
		case "Right", "Left", "Adventure Name", "Adventure Typeline", "Adventure Typeline Accent":
			err = a.storybookPage(st, c)
		default:
			err = a.rootLayer(st, c)
		}
		if err != nil {
			return err
		}
	}
	es := a.takeEntries(st, start, firstPart, "storybook", "")
	for i := range es {
		if p := a.recipe.Parts.Get(es[i].part); p != nil && p.Slot == "twins" {
			es[i].face = "second"
		}
	}
	a.placeEntries(st, g, es)
	return nil
}

// storybookPage adds a page or plate of the book with a layer per color: the
// right page in the pinline colors, its pairs blended by the Masks/Right/1/2
// ramp, and the rest in the adventure's colors, which Proxyshop reads the way
// it reads the colors of a card's twins
func (a *analyzer) storybookPage(st *layoutState, g *psd.Layer) error {
	colors := colorChildrenAtLeast(g, 1)
	if len(colors) == 0 {
		return a.rootLayer(st, g)
	}
	vs, err := colorVariants(colors)
	if err != nil {
		return err
	}
	slot := "twins"
	if g.Name == "Right" {
		slot = "pinlines"
	}
	a.addPart(st, g, slug(g.Name), slot, "", &Render{Target: g.Path(), Variants: vs})
	if slot == "pinlines" {
		if fit := st.rampBlend(a, st.f.Root.Find("Masks", "Right", "1/2")); fit != nil {
			a.recipe.Parts[len(a.recipe.Parts)-1].Rule.Blend = mustRaw(fit.blend())
		}
	}
	return nil
}
