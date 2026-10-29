package proxyshop

import (
	"encoding/json"
	"image"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/proxyshop/proxyshoptest"
	"github.com/odevine/mimic/engine/psd"
	"github.com/odevine/mimic/engine/psdrender"
)

// paraText is a paragraph type layer whose lines wrap in a box w by h whose
// corner is at x, y
func paraText(name string, x, y, w, h, size float64, font string) node {
	return node{name: name, kind: psd.KindText, hidden: true, rect: image.Rect(int(x), int(y), int(x+w/2), int(y+size)), text: &psd.Text{
		Transform: [6]float64{1, 0, 0, 1, x, y},
		Box:       true,
		BoxBounds: [4]float64{0, 0, w, h},
		Runs:      []psd.TextRun{{FontSize: size, Font: font, Leading: size, FillColor: psd.Color{}}},
	}}
}

func ref(name string, x0, y0, x1, y1 int) node {
	return node{name: name, kind: psd.KindShape, hidden: true, rect: image.Rect(x0, y0, x1, y1)}
}

func shown(n node, r image.Rectangle) node {
	n.hidden = false
	n.rect = r
	return n
}

// analyzed runs Analyze on one document and decodes its layout
func analyzed(t *testing.T, name string, f *psd.File) (*Analysis, map[string]any) {
	t.Helper()
	key := SourceKey(name)
	an, err := Analyze([]Source{{Key: key, Name: name, File: f}}, Options{ID: "test.kind", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	var l map[string]any
	if err := json.Unmarshal(an.Recipe.Layouts.Get(key), &l); err != nil {
		t.Fatal(err)
	}
	return an, l
}

// at reads a value from decoded JSON by a path of keys and indexes
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			s, ok := v.([]any)
			if !ok || k >= len(s) {
				return nil
			}
			v = s[k]
		}
	}
	return v
}

func stackOf(l map[string]any) []string {
	var out []string
	for _, e := range at(l, "stack").([]any) {
		s := e.(map[string]any)["id"].(string)
		if c, ok := e.(map[string]any)["condition"]; ok {
			s += "@" + c.(string)
		}
		if f, ok := e.(map[string]any)["face"]; ok {
			s += "/" + f.(string)
		}
		out = append(out, s)
	}
	return out
}

func contractOf(an *Analysis) string {
	var c string
	json.Unmarshal(an.Recipe.Template.Get("contract"), &c)
	return c
}

func pwSize(name string, top int) node {
	return group(name,
		group("Text and Icons",
			pointText("Card Name", 12, 20, 8, "BelerenProxy-Bold", "left"),
			pointText("Typeline", 12, float64(top-4), 7, "BelerenProxy-Bold", "left"),
			ref("Expansion Reference", 80, top-12, 90, top-4),
			ref("Textbox Reference", 10, top, 90, 125),
		),
		group("Pinlines", colors("W", "U", "WU")...),
		group("Textbox",
			group("Ability Dividers", group("Ragged Lines"), shown(ref("Textbox", 0, 0, 0, 0), image.Rect(10, top-20, 90, 126))),
			shown(ref("Textbox", 0, 0, 0, 0), image.Rect(10, top, 90, 126)),
		),
		group("Background", colors("W", "U", "WU")...),
	)
}

func TestAnalyzePlaneswalker(t *testing.T) {
	f := build(t, 100, 140,
		group("Masks", shown(ref("Top", 0, 0, 0, 0), image.Rect(12, 95, 88, 99)), shown(ref("Bottom", 0, 0, 0, 0), image.Rect(12, 105, 88, 109))),
		group("Legal", pointText("Artist", 10, 135, 4, "Proxyglyph", "left"), pointText("Top", 10, 131, 3, "Gotham-Medium", "left")),
		group("Loyalty Graphics",
			group("Starting Loyalty", pointText("Text", 85, 132, 8, "BelerenProxy-Bold", "center"), layer("Symbol", false)),
			paraText("Ability Text", 20, 100, 60, 20, 5, "PlantinMTPro-Regular"),
			paraText("Static Text", 14, 100, 70, 20, 5, "PlantinMTPro-Regular"),
			pointText("Colon", 18, 104, 6, "PlantinMTPro-Regular", "center"),
			group("+", layer("Symbol", false), pointText("Cost", 10, 103, 5, "BelerenProxy-Bold", "center")),
			group("0", layer("Symbol", false), pointText("Cost", 10, 104, 5, "BelerenProxy-Bold", "center")),
			group("-", layer("Symbol", false), pointText("Cost", 10, 105, 5, "BelerenProxy-Bold", "center")),
		),
		pwSize("pw-3", 90),
		pwSize("pw-4", 80),
		ref("Full Art Frame", 5, 5, 95, 130),
		ref("Art Frame", 10, 25, 90, 75),
		layer("Layer 1", false),
		shown(ref("BG", 0, 0, 0, 0), image.Rect(0, 0, 100, 140)),
	)
	f.Root.Find("pw-3").Visible = false
	f.Root.Find("pw-4").Visible = false
	an, l := analyzed(t, "pw.psd", f)

	want := []string{
		"bg",
		"pw4_background@rows>3", "pw4_textbox@rows>3", "pw4_pinlines@rows>3",
		"pw3_background@rows<4", "pw3_textbox@rows<4", "pw3_pinlines@rows<4",
		"loyalty_box@loyalty",
	}
	if got := stackOf(l); !reflect.DeepEqual(got, want) {
		t.Errorf("stack\n got %v\nwant %v", got, want)
	}
	if c := contractOf(an); c != "1.2" {
		t.Errorf("contract %q, want 1.2 for a layout with sections", c)
	}
	if got := at(l, "art", 0, "height"); got != 125.0 {
		t.Errorf("art %v, want the Full Art Frame behind the textbox", at(l, "art"))
	}

	for _, c := range []struct{ name, cond, rectY, band string }{{"abilities_3", "rows<4", "90", "pw_pw3_band"}, {"abilities_4", "rows>3", "80", "pw_pw4_band"}} {
		s := at(l, "sections", c.name)
		if s == nil {
			t.Fatalf("no section %s among %v", c.name, at(l, "sections"))
		}
		if at(s, "split") != "loyalty" || at(s, "condition") != c.cond || at(s, "avoidLayer") != "loyalty_box" || at(s, "above") != "pw3_pinlines" {
			t.Errorf("%s: %v", c.name, s)
		}
		if got, _ := json.Marshal(at(s, "rect", "y")); string(got) != c.rectY {
			t.Errorf("%s rect %v", c.name, at(s, "rect"))
		}
		badges := at(s, "items", 0, "parts")
		if !reflect.DeepEqual(badges, map[string]any{"plus": "pw_badge_plus", "minus": "pw_badge_minus", "zero": "pw_badge_zero"}) {
			t.Errorf("%s badges %v", c.name, badges)
		}
		if at(s, "items", 2, "format") != ":" || at(s, "items", 1, "text", "font") != "title" {
			t.Errorf("%s cost and colon items %v", c.name, at(s, "items"))
		}
		if at(s, "bands", "part") != c.band || at(s, "bands", "rows") != "even" || at(s, "bands", "top", "part") != "pw_band_top" || at(s, "bands", "bottom", "y") != 95.0 {
			t.Errorf("%s bands %v", c.name, at(s, "bands"))
		}
	}

	// The textbox leaves the darker copy to the bands, which cut the pinlines
	// out of it, and each edge is cut by its ragged line
	if r := an.Recipe.Parts.Get("pw_pw3_textbox").Render; !reflect.DeepEqual(r.Hide, [][]string{{"pw-3", "Textbox", "Ability Dividers"}}) {
		t.Errorf("textbox hides %v", r.Hide)
	}
	if r := an.Recipe.Parts.Get("pw_pw3_band").Render; len(r.Cut) != 1 || !reflect.DeepEqual(r.Cut[0].Target, []string{"pw-3", "Pinlines"}) {
		t.Errorf("band cuts %+v", r.Cut)
	}
	edge := an.Recipe.Parts.Get("pw_band_bottom")
	if edge == nil || edge.Crop == nil || edge.Crop.Y0 != 95 || !reflect.DeepEqual(edge.Render.Cut[0].Target, []string{"Masks", "Top"}) {
		t.Errorf("bottom edge %+v", edge)
	}

	// The type line moves up for four abilities, whatever else changes it
	when := at(l, "textBoxes", "type_full", "when").([]any)
	var conds []string
	for _, w := range when {
		conds = append(conds, at(w, "condition").(string))
	}
	if !strings.Contains(strings.Join(conds, " "), "set_symbol,rows>3") || conds[len(conds)-1] != "rows>3" || at(when[len(when)-1], "rect", "y") != 76.0 {
		t.Errorf("type line overrides %v", when)
	}
	if at(l, "textBoxes", "loyalty", "text", "align") != "center" {
		t.Errorf("loyalty box %v", at(l, "textBoxes", "loyalty"))
	}
	if at(l, "textBoxes", "copyright", "flow", "duck", "layer") != "loyalty_box" {
		t.Errorf("copyright %v", at(l, "textBoxes", "copyright"))
	}
	if at(l, "symbols", "set", "y") != 78.0 {
		t.Errorf("set symbol %v, want pw-3's", at(l, "symbols"))
	}
	for _, n := range an.Notes {
		if strings.Contains(n, "no rules text") {
			t.Errorf("note %q for a layout whose rules are a section", n)
		}
	}
}

func TestAnalyzeSagaAndClass(t *testing.T) {
	saga := build(t, 100, 140,
		group("Text and Icons",
			group("Saga",
				paraText("Reminder Text", 12, 20, 30, 20, 4, "PlantinMTPro-Italic"),
				shown(layer("Divider", false), image.Rect(12, 44, 48, 46)),
				paraText("Text", 20, 50, 28, 10, 5, "PlantinMTPro-Regular"),
				shown(layer("II", true), image.Rect(4, 70, 14, 80)),
				shown(layer("I", true), image.Rect(4, 50, 14, 60)),
			),
			ref("Description Reference", 10, 18, 48, 42),
			ref("Textbox Reference", 14, 46, 48, 120),
		),
		group("Textbox", colors("W", "U", "Gold")...),
		group("Pinlines & Saga Stripe", colors("W", "U", "WU")...),
		layer("Layer 1", false),
	)
	an, l := analyzed(t, "saga.psd", saga)
	s := at(l, "sections", "chapters")
	if at(s, "split") != "chapters" || at(s, "spread", "inside") != 1.5 || at(s, "divider", "part") != "saga_saga_divider" || at(s, "divider", "y") != 45.0 {
		t.Errorf("chapters %v", s)
	}
	if at(s, "items", 0, "for") != "chapter" || at(s, "items", 0, "y") != 55.0 || at(s, "items", 0, "parts", "II") != "saga_saga_ii" {
		t.Errorf("numerals %v", at(s, "items"))
	}
	// Every numeral is placed where the first is, so one item moves them all
	ii := an.Recipe.Parts.Get("saga_saga_ii")
	if ii == nil || ii.Crop.Y0 != 70 || string(ii.Origin) != `{"x":4,"y":50}` {
		t.Errorf("numeral II %+v", ii)
	}
	if at(l, "textBoxes", "description", "rect", "y") != 18.0 {
		t.Errorf("description %v", at(l, "textBoxes", "description"))
	}
	if p := an.Recipe.Parts.Get("saga_textbox"); p.Slot != "background" {
		t.Errorf("the saga's textbox reads %q", p.Slot)
	}
	if got := stackOf(l); got[len(got)-1] != "saga_divider" {
		t.Errorf("stack %v, want the divider under the reminder kept", got)
	}

	class := build(t, 100, 140,
		group("Text and Icons",
			group("Class",
				paraText("Reminder Text", 52, 10, 40, 30, 4, "PlantinMTPro-Italic"),
				shown(layer("Divider", false), image.Rect(50, 24, 95, 26)),
				paraText("Text", 52, 30, 40, 10, 5, "PlantinMTPro-Regular"),
				group("Stage",
					shown(paraText("Cost", 52, 46, 40, 6, 4, "PlantinMTPro-Regular"), image.Rect(53, 47, 60, 51)),
					shown(paraText("Level", 52, 46, 40, 6, 4, "PlantinMTPro-Bold"), image.Rect(80, 47, 92, 52)),
					shown(layer("Arena Asset", false), image.Rect(50, 44, 95, 54)),
				),
			),
			ref("Textbox Reference", 50, 28, 95, 120),
		),
		layer("Layer 1", false),
	)
	class.Root.Find("Text and Icons", "Class", "Stage").Visible = false
	_, l = analyzed(t, "class.psd", class)
	s = at(l, "sections", "levels")
	if at(s, "split") != "levels" || at(s, "header", "for") != "cost" || at(s, "header", "height") != 11.0 {
		t.Errorf("levels %v", s)
	}
	items := at(s, "items").([]any)
	if len(items) != 3 || at(items[0], "part") != "class_class_stage" || at(items[1], "format") != "{}:" || at(items[2], "format") != "Level {}" || at(items[2], "anchor") != "top" {
		t.Errorf("stage items %v", items)
	}
	// With no reference the reminder runs down to the divider under it
	if at(l, "textBoxes", "description", "rect", "height") != 14.0 {
		t.Errorf("description %v", at(l, "textBoxes", "description"))
	}
}

func TestAnalyzeFixedKinds(t *testing.T) {
	leveler := build(t, 100, 140,
		group("Text and Icons",
			group("Leveler Text",
				pointText("Bottom Level", 10, 110, 6, "BelerenProxy-Bold", "center"),
				pointText("Bottom Level Label", 10, 104, 3, "PlantinMTPro-Regular", "center"),
				pointText("Bottom Power / Toughness", 85, 110, 6, "BelerenProxy-Bold", "center"),
				ref("Textbox Reference - Levels Z+", 5, 100, 95, 120),
				paraText("Rules Text - Levels Z+", 20, 102, 50, 10, 5, "PlantinMTPro-Regular"),
				pointText("Middle Level", 10, 90, 6, "BelerenProxy-Bold", "center"),
				pointText("Middle Level Label", 10, 84, 3, "PlantinMTPro-Regular", "center"),
				pointText("Middle Power / Toughness", 85, 90, 6, "BelerenProxy-Bold", "center"),
				ref("Textbox Reference - Level X-Y", 5, 80, 95, 100),
				paraText("Rules Text - Levels X-Y", 20, 82, 50, 10, 5, "PlantinMTPro-Regular"),
				pointText("Top Power / Toughness", 85, 72, 6, "BelerenProxy-Bold", "center"),
				ref("Textbox Reference - Level Text", 5, 60, 95, 80),
				paraText("Rules Text - Level Up", 8, 62, 70, 10, 5, "PlantinMTPro-Regular"),
			),
		),
		layer("Layer 1", false),
	)
	an, l := analyzed(t, "leveler.psd", leveler)
	for _, k := range []string{"level_up", "pt", "level_label_1", "level_1", "level_1_text", "level_1_pt", "level_label_2", "level_2", "level_2_text", "level_2_pt"} {
		if at(l, "textBoxes", k) == nil {
			t.Errorf("no %s box", k)
		}
	}
	if at(l, "textBoxes", "level_label_2", "draws") != "level_label" || at(l, "textBoxes", "level_1_text", "rect", "y") != 80.0 || at(l, "textBoxes", "level_1_text", "text", "vAlign") != "center" {
		t.Errorf("leveler boxes %v", at(l, "textBoxes"))
	}
	if strings.Contains(strings.Join(an.Notes, "\n"), "no rules text") {
		t.Errorf("notes %v", an.Notes)
	}

	proto := build(t, 100, 140,
		group("Text and Icons",
			pointText("Prototype Power / Toughness", 85, 80, 6, "BelerenProxy-Bold", "center"),
			pointText("Prototype Mana Cost", 90, 66, 6, "PlantinMTPro-Regular", "right"),
			paraText("Prototype Rules Text", 8, 62, 70, 30, 5, "PlantinMTPro-Regular"),
			paraText("Mutate", 8, 40, 70, 10, 5, "PlantinMTPro-Regular"),
			ref("Mutate Reference", 5, 38, 95, 58),
			ref("Textbox Reference", 5, 85, 95, 125),
		),
		group("Prototype Manabox", node{name: "Shape", kind: psd.KindGroup, children: []node{ref("3", 60, 60, 92, 70), ref("2", 70, 60, 92, 70)}}, layer("W", true), layer("Gold", true)),
		group("Prototype Textbox", colors("W", "U", "Gold")...),
		group("Pinlines & Textbox", colors("W", "U")...),
		layer("Layer 1", false),
	)
	an, l = analyzed(t, "prototype.psd", proto)
	if got := stackOf(l); !reflect.DeepEqual(got, []string{"pinlines_textbox", "prototype_textbox", "prototype_manabox"}) {
		t.Errorf("stack %v", got)
	}
	mb := an.Recipe.Parts.Get("prototype_prototype_manabox")
	if mb.Slot != "prototype" || !reflect.DeepEqual(mb.Render.Show, [][]string{{"Prototype Manabox", "Shape"}, {"Prototype Manabox", "Shape", "3"}}) {
		t.Errorf("mana box %+v %v", mb, mb.Render.Show)
	}
	if at(l, "textBoxes", "prototype", "rect", "height") != 23.0 || at(l, "textBoxes", "prototype_mana", "text", "align") != "right" || at(l, "textBoxes", "prototype_pt") == nil {
		t.Errorf("prototype boxes %v", at(l, "textBoxes"))
	}
	if at(l, "textBoxes", "mutate", "rect", "y") != 38.0 {
		t.Errorf("mutate box %v", at(l, "textBoxes", "mutate"))
	}

	battle := build(t, 140, 100,
		group("Text and Icons",
			pointText("Defense", 120, 90, 6, "BelerenProxy-Bold", "center"),
			paraText("Rules Text", 20, 62, 70, 10, 5, "PlantinMTPro-Regular"),
			ref("Textbox Reference", 15, 60, 110, 95),
		),
		group("Defense Box", shown(layer("Defense Box", false), image.Rect(110, 80, 130, 96))),
		layer("Layer 1", false),
	)
	_, l = analyzed(t, "battle.psd", battle)
	if at(l, "textBoxes", "oracle", "flow", "avoidLayer") != "defense_box" || at(l, "textBoxes", "defense", "rect", "width") != 20.0 {
		t.Errorf("battle boxes %v", at(l, "textBoxes"))
	}
	if at(l, "serves", 0, "kind") != "battle" || at(l, "canvas", "rotate") != 270.0 {
		t.Errorf("battle layout %v %v", at(l, "serves"), at(l, "canvas"))
	}

	planar := build(t, 140, 100,
		group("Legal", pointText("Artist", 70, 95, 3, "Proxyglyph", "center"), pointText("Set", 70, 98, 2, "Gotham-Medium", "center")),
		group("Text and Icons",
			paraText("Chaos Ability", 25, 80, 90, 20, 4, "PlantinMTPro-Regular"),
			paraText("Static Ability", 20, 65, 90, 30, 4, "PlantinMTPro-Regular"),
			shown(pointText("Chaos Symbol", 18, 84, 5, "Proxyglyph", "left"), image.Rect(18, 80, 23, 85)),
		),
		layer("Frame Top", false),
		layer("Layer 1", false),
	)
	an, l = analyzed(t, "planar.psd", planar)
	if at(l, "canvas", "rotate") != nil || at(l, "canvas", "dpi") != 27.0 {
		t.Errorf("planar canvas %v, want it kept landscape", at(l, "canvas"))
	}
	if got := stackOf(l); got[len(got)-1] != "chaos_symbol@chaos" {
		t.Errorf("stack %v", got)
	}
	if at(l, "textBoxes", "oracle", "rect", "height") != 14.0 || at(l, "textBoxes", "chaos", "rect", "y") != 80.0 {
		t.Errorf("planar boxes %v", at(l, "textBoxes"))
	}
	if at(l, "textBoxes", "collector") != nil || at(l, "textBoxes", "artist", "text", "align") != "center" {
		t.Errorf("planar legal %v", at(l, "textBoxes"))
	}
	if an.Recipe.Parts.Get("planar_chaos_symbol") == nil {
		t.Error("no chaos symbol part")
	}
}

func TestAnalyzeTwoFacedKinds(t *testing.T) {
	mdfc := build(t, 100, 140,
		group("Text and Icons",
			group("MDFC Front",
				pointText("Right", 50, 128, 5, "TimesNewRomanPSMT", "right"),
				pointText("Left", 12, 128, 4, "BelerenProxy-Bold", "left"),
				group("Top", colors("W", "U", "Gold")...),
				group("Bottom", colors("W", "U", "Gold")...),
			),
		),
		group("Pinlines & Textbox", colors("W", "U")...),
		layer("Layer 1", false),
	)
	_, l := analyzed(t, "mdfc-front.psd", mdfc)
	if got := stackOf(l); !reflect.DeepEqual(got, []string{"pinlines_textbox", "mdfc_bottom/other", "mdfc_top"}) {
		t.Errorf("stack %v", got)
	}
	if at(l, "textBoxes", "flipside_type", "flow", "clearOf") != "flipside_mana" || at(l, "textBoxes", "flipside_mana", "rect", "width") != 38.0 {
		t.Errorf("flipside boxes %v %v", at(l, "textBoxes", "flipside_type"), at(l, "textBoxes", "flipside_mana"))
	}

	pwtf := build(t, 100, 140,
		group("Transform",
			group("Front", layer("sunmoondfc", true), layer("compasslanddfc", true)),
			group("Circle", layer("Backing Circle", false)),
		),
		layer("Layer 1", false),
		shown(ref("BG", 0, 0, 0, 0), image.Rect(0, 0, 100, 140)),
	)
	an, l := analyzed(t, "pw-tf-front.psd", pwtf)
	if got := stackOf(l); !reflect.DeepEqual(got, []string{"bg", "transform_circle", "transform_icon_front"}) {
		t.Errorf("stack %v", got)
	}
	if got := an.Recipe.Parts.Get("pw_tf_front_transform_icon_front").Render.Variants.Keys(); !reflect.DeepEqual(got, []string{"compasslanddfc", "sunmoondfc"}) {
		t.Errorf("icons %v", got)
	}
	// The icon group has neither of the two icons the engine bundles a
	// fallback for, so both fall back rather than drawing nothing for a card
	// that needs one, such as a transform planeswalker with no named frame
	// effect, which defaults to the plain triangle (convertdfc)
	if got := an.Recipe.Parts.Get("pw_tf_front_transform_icon_front").Render.AssetVariants; !reflect.DeepEqual(got, []string{"convertdfc", "upsidedowndfc"}) {
		t.Errorf("asset fallbacks %v", got)
	}

	adv := build(t, 100, 140,
		group("Text and Icons",
			paraText("Rules Text - Adventure", 10, 100, 35, 20, 4, "PlantinMTPro-Regular"),
			pointText("Typeline - Adventure", 10, 96, 4, "BelerenProxy-Bold", "left"),
			pointText("Mana Cost - Adventure", 48, 90, 4, "PlantinMTPro-Regular", "right"),
			pointText("Card Name - Adventure", 10, 90, 4, "BelerenProxy-Bold", "left"),
			ref("Textbox Reference - Adventure", 8, 98, 49, 125),
			pointText("Card Name", 12, 20, 8, "BelerenProxy-Bold", "left"),
		),
		layer("Layer 1", false),
	)
	_, l = analyzed(t, "adventure.psd", adv)
	if at(l, "serves", 0, "role") != "adventure" || at(l, "textBoxes", "title", "face") != "first" {
		t.Errorf("adventure layout %v %v", at(l, "serves"), at(l, "textBoxes", "title"))
	}
	for k, draws := range map[string]string{"adventure_title": "title", "adventure_mana": "mana", "adventure_type": "type", "adventure_oracle": "oracle"} {
		if at(l, "textBoxes", k, "draws") != draws || at(l, "textBoxes", k, "face") != "second" {
			t.Errorf("%s %v", k, at(l, "textBoxes", k))
		}
	}
	if at(l, "textBoxes", "adventure_oracle", "rect", "y") != 98.0 || at(l, "textBoxes", "adventure_title", "flow", "clearOf") != "adventure_mana" {
		t.Errorf("adventure boxes %v", at(l, "textBoxes"))
	}

	token := build(t, 100, 140,
		group("Text and Icons", pointText("Card Name", 50, 20, 8, "BelerenSmallCaps-Bold", "center"), ref("Name Reference Non-Legendary", 20, 10, 80, 25), ref("Name Reference Legendary", 5, 8, 95, 26)),
		group("Frame",
			group("Non-Legendary", group("Creature", colors("W", "U")...), group("Non-Creature", colors("W", "U")...)),
			group("Legendary", group("Creature", colors("W", "U")...), group("Non-Creature", colors("W", "U")...)),
		),
		group("Textbox",
			group("Full", layer("Fade", false), pointText("Typeline", 50, 100, 5, "BelerenSmallCaps-Bold", "center"), ref("Typeline Reference", 5, 95, 95, 102), ref("Textbox Reference", 45, 104, 55, 130), paraText("Rules Text - Noncreature", 10, 105, 80, 20, 5, "PlantinMTPro-Regular")),
			group("None", layer("Fade", false)),
		),
		layer("Layer 1", false),
	)
	token.Root.Find("Textbox", "Full").Visible = false
	token.Root.Find("Textbox", "None").Visible = false
	an, l = analyzed(t, "token.psd", token)
	want := []string{"textbox", "frame@nonlegendary", "frame_creature@nonlegendary,creature", "frame_legendary@legendary", "frame_legendary_creature@legendary,creature"}
	if got := stackOf(l); !reflect.DeepEqual(got, want) {
		t.Errorf("stack\n got %v\nwant %v", got, want)
	}
	tb := an.Recipe.Parts.Get("token_textbox").Render
	if !reflect.DeepEqual(tb.Show, [][]string{{"Textbox", "Full"}}) || !reflect.DeepEqual(tb.Hide, [][]string{{"Textbox", "Full", "Typeline"}}) {
		t.Errorf("token textbox shows %v hides %v", tb.Show, tb.Hide)
	}
	if at(l, "textBoxes", "oracle", "rect", "x") != 10.0 || at(l, "textBoxes", "oracle", "text", "align") != "center" || at(l, "textBoxes", "title", "when", 0, "rect", "width") != 90.0 {
		t.Errorf("token boxes %v", at(l, "textBoxes"))
	}
}

func TestContractFollowsTheLayouts(t *testing.T) {
	beneath := build(t, 100, 140, group("Background", colors("W", "U", "B")...), ref("Art Frame", 10, 25, 90, 75), layer("Layer 1", false))
	an, _ := analyzed(t, "normal.psd", beneath)
	if c := contractOf(an); c != "1.2" {
		t.Errorf("contract %q for art beneath the stack, want 1.2", c)
	}
	above := build(t, 100, 140, ref("Art Frame", 10, 25, 90, 75), layer("Layer 1", false), group("Background", colors("W", "U", "B")...))
	an, l := analyzed(t, "normal.psd", above)
	if c := contractOf(an); c != "1.0" || at(l, "art", 0, "after") != "background" {
		t.Errorf("contract %q with art %v, want 1.0 for art above an entry", c, at(l, "art"))
	}
}

func TestCutOutClearsWhatTheLayerCovers(t *testing.T) {
	full := image.Rect(0, 0, 40, 20)
	doc := proxyshoptest.Write(40, 20,
		proxyshoptest.Named(proxyshoptest.Pixels(image.Rect(10, 0, 20, 20), func(x, y int) color.NRGBA {
			return color.NRGBA{A: uint8(255 * (x - 10) / 10)}
		}), "Line", true),
		proxyshoptest.Named(proxyshoptest.Pixels(full, func(x, y int) color.NRGBA { return color.NRGBA{200, 10, 10, 255} }), "Box", false),
	)
	f := openPSD(t, doc)
	img := image.NewNRGBA(full)
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+3] = 200, 255
	}
	find := func(path []string) (*psd.Layer, error) { return f.Root.Find(path...), nil }
	if err := cutOut(f, psdrender.New(f), RenderCut{Target: []string{"Line"}}, full, []*image.NRGBA{img}, find); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ x, a int }{{5, 255}, {10, 255}, {15, 128}, {19, 26}, {25, 255}} {
		if got := int(img.NRGBAAt(c.x, 5).A); got < c.a-2 || got > c.a+2 {
			t.Errorf("alpha at %d is %d, want %d", c.x, got, c.a)
		}
	}
}

func TestAnalyzeVerticalMode(t *testing.T) {
	shape := func(children ...node) node { return group("Shape", children...) }
	f := build(t, 200, 280,
		group("Text and Icons",
			pointText("Card Name", 30, 30, 8, "BelerenProxy-Bold", "left"),
			pointText("Typeline", 30, 250, 8, "BelerenProxy-Bold", "left"),
		),
		group("Class",
			ref("Textbox Reference", 110, 64, 190, 240),
			paraText("Reminder Text", 112, 42, 76, 14, 6, "PlantinMTPro-Italic"),
			shown(layer("Divider", false), image.Rect(108, 58, 192, 62)),
			paraText("Text", 112, 66, 76, 8, 8, "PlantinMTPro-Regular"),
			group("Stage",
				shown(paraText("Cost", 112, 98, 30, 6, 4, "PlantinMTPro-Regular"), image.Rect(113, 99, 120, 103)),
				shown(paraText("Level", 150, 98, 38, 6, 4, "PlantinMTPro-Bold"), image.Rect(160, 99, 186, 104)),
				shown(layer("Arena Asset", false), image.Rect(110, 96, 190, 110)),
			),
		),
		group("Saga",
			ref("Description Reference", 25, 40, 108, 90),
			ref("Textbox Reference", 32, 95, 108, 240),
			paraText("Reminder Text", 27, 43, 78, 30, 6, "PlantinMTPro-Italic"),
			shown(layer("Divider", false), image.Rect(31, 92, 108, 96)),
			paraText("Text", 36, 108, 60, 6, 6, "PlantinMTPro-Regular"),
			node{name: "I", kind: psd.KindPixel, hidden: true, rect: image.Rect(16, 103, 33, 122)},
			group("Stripe", shape(ref("Normal", 24, 96, 26, 227))),
			shown(layer("Normal", true), image.Rect(17, 57, 32, 240)),
			group("Banner", shape(ref("Normal", 17, 58, 32, 239))),
		),
		group("Textbox", append(colors("W", "U", "B"), shape(shown(ref("Textbox", 0, 0, 0, 0), image.Rect(25, 40, 108, 242))))...),
		group("Name & Title Boxes", append(colors("W", "U"), shape(
			shown(layer("Bottom", true), image.Rect(20, 220, 180, 240)),
			layer("Normal", true),
		))...),
		group("Banner Top", shown(layer("Layer 18", false), image.Rect(17, 55, 26, 95))),
		group("Background", colors("W", "U", "B")...),
		ref("Art Frame Right", 108, 40, 194, 242),
		ref("Art Frame Left", 25, 40, 109, 242),
		layer("Layer 1", false),
	)
	f.Root.Find("Saga").Visible = false
	f.Root.Find("Class").Visible = false
	an, err := Analyze([]Source{{Key: "vertical_mode", Name: "vertical-mode.psd", File: f}}, Options{ID: "test.kind", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	layout := func(key string) map[string]any {
		var l map[string]any
		if err := json.Unmarshal(an.Recipe.Layouts.Get(key), &l); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return l
	}
	index := func(stack []string, id string) int {
		for i, s := range stack {
			if strings.SplitN(s, "@", 2)[0] == id {
				return i
			}
		}
		return -1
	}

	// One document makes a layout for each kind
	saga := layout("vertical_mode_saga")
	if at(saga, "serves", 0, "kind") != "saga" || at(saga, "art", 0, "x") != 108.0 {
		t.Errorf("saga serves %v and art %v", at(saga, "serves"), at(saga, "art"))
	}
	if at(saga, "sections", "chapters", "rect", "x") != 32.0 || at(saga, "textBoxes", "description", "rect", "y") != 40.0 {
		t.Errorf("saga chapters %v, description %v", at(saga, "sections", "chapters"), at(saga, "textBoxes", "description"))
	}
	stack := stackOf(saga)
	b := index(stack, "saga_banner")
	if b < 0 || b < index(stack, "textbox") || index(stack, "saga_divider") != b+1 || index(stack, "banner_top") < 0 {
		t.Errorf("saga stack %v, want the banner in the Saga group's place with its divider above it", stack)
	}
	banner := an.Recipe.Parts.Get("vertical_mode_saga_saga_banner")
	var w, wu RenderVariant
	json.Unmarshal(banner.Render.Variants.Get("w"), &w)
	json.Unmarshal(banner.Render.Variants.Get("wu"), &wu)
	if len(w.Fills) != 2 || w.Fills[0].Color != "#f1e1c1" || w.Fills[1].Color != "#ebd19c" || !reflect.DeepEqual(w.Fills[1].Into, []string{"Saga", "Stripe"}) {
		t.Errorf("banner w %+v", w)
	}
	if len(wu.Fills) != 2 || wu.Fills[0].Gradient == nil || wu.Fills[1].Color != "#2a2a2a" {
		t.Errorf("banner wu %+v, want the stripe of a pair dark", wu)
	}
	if !reflect.DeepEqual(banner.Render.Hide, [][]string{{"Saga", "Divider"}}) {
		t.Errorf("banner hides %v", banner.Render.Hide)
	}

	class := layout("vertical_mode_class")
	if at(class, "serves", 0, "kind") != "class" || at(class, "art", 0, "x") != 25.0 {
		t.Errorf("class serves %v and art %v", at(class, "serves"), at(class, "art"))
	}
	stack = stackOf(class)
	if index(stack, "banner_top") >= 0 || index(stack, "saga_banner") >= 0 {
		t.Errorf("class stack %v, want no banner", stack)
	}
	if at(class, "sections", "levels", "rect", "x") != 110.0 {
		t.Errorf("class levels %v", at(class, "sections", "levels"))
	}
	// The textbox moves to the right half, centered on the art frame there
	tb := an.Recipe.Parts.Get("vertical_mode_class_textbox")
	if tb == nil || tb.Crop == nil || tb.Crop.X0 != 25 || tb.Crop.X1 != 108 {
		t.Fatalf("class textbox %+v", tb)
	}
	if e := at(class, "stack", index(stack, "textbox"), "at"); at(e, "x") != 110.0 || at(e, "y") != 40.0 {
		t.Errorf("class textbox at %v, want 110, 40", e)
	}

	// Name & Title Boxes/Shape has "Bottom" visible and "Normal" hidden, the
	// way vertical-mode.psd itself saves it even though neither
	// ClassVectorTemplate nor SagaVectorTemplate ever shows Bottom. Both
	// layouts' part must show Normal and hide Bottom regardless
	for _, name := range []string{"vertical_mode_saga_name_title_boxes", "vertical_mode_class_name_title_boxes"} {
		ntb := an.Recipe.Parts.Get(name)
		if ntb == nil || ntb.Render == nil {
			t.Fatalf("%s: %+v", name, ntb)
		}
		want := []string{"Name & Title Boxes", "Shape", "Normal"}
		drop := []string{"Name & Title Boxes", "Shape", "Bottom"}
		if !containsPath(ntb.Render.Show, want) {
			t.Errorf("%s show %v, want %v shown", name, ntb.Render.Show, want)
		}
		if !containsPath(ntb.Render.Hide, drop) {
			t.Errorf("%s hide %v, want %v hidden", name, ntb.Render.Hide, drop)
		}
	}
}

func containsPath(paths [][]string, want []string) bool {
	for _, p := range paths {
		if reflect.DeepEqual(p, want) {
			return true
		}
	}
	return false
}

func TestAnalyzeStorybook(t *testing.T) {
	page := func(name string, r image.Rectangle) node {
		return group(name, append(colors("W", "U", "R"), shown(layer("Shape", false), r))...)
	}
	f := build(t, 200, 280,
		group("Text and Icons",
			pointText("Card Name", 30, 30, 8, "BelerenProxy-Bold", "left"),
			pointText("Card Name - Adventure", 30, 190, 6, "BelerenProxy-Bold", "left"),
			ref("Textbox Reference - Adventure", 25, 200, 100, 260),
		),
		group("Storybook",
			group("Pinlines", shown(layer("Shape", false), image.Rect(0, 262, 200, 264))),
			group("Shading & Outlines", shown(layer("Lines", false), image.Rect(20, 180, 180, 265))),
			page("Adventure Name", image.Rect(22, 182, 100, 196)),
			page("Left", image.Rect(22, 182, 100, 262)),
			page("Right", image.Rect(100, 182, 178, 262)),
			group("Wings", append(colors("W", "U"), shown(layer("Shape", false), image.Rect(18, 184, 182, 250)))...),
		),
		group("Textbox", colors("W", "U", "R")...),
		group("Background", colors("W", "U", "R")...),
		ref("Art Frame", 25, 40, 175, 170),
		layer("Layer 1", false),
	)
	an, l := analyzed(t, "normal-vector.psd", f)
	if at(l, "serves", 0, "role") != "adventure" {
		t.Errorf("serves %v, want an adventure", at(l, "serves"))
	}
	stack := stackOf(l)
	want := []string{"background", "storybook_wings", "storybook_right", "storybook_left/second", "storybook_adventure_name/second", "storybook_shading_outlines", "storybook_pinlines"}
	if !reflect.DeepEqual(stack, want) {
		t.Errorf("stack %v, want %v with no root textbox", stack, want)
	}
	if p := an.Recipe.Parts.Get("normal_vector_storybook_right"); p.Slot != "pinlines" {
		t.Errorf("the right page reads %q", p.Slot)
	}
	if p := an.Recipe.Parts.Get("normal_vector_storybook_left"); p.Slot != "twins" {
		t.Errorf("the left page reads %q", p.Slot)
	}
	var wu, w RenderVariant
	wings := an.Recipe.Parts.Get("normal_vector_storybook_wings")
	json.Unmarshal(wings.Render.Variants.Get("wu"), &wu)
	json.Unmarshal(wings.Render.Variants.Get("w"), &w)
	if len(w.Fills) != 1 || w.Fills[0].Color != "#d5cbb5" || len(wu.Fills) != 1 || wu.Fills[0].Gradient == nil {
		t.Errorf("wings w %+v, wu %+v", w, wu)
	}
	if p := an.Recipe.Parts.Get("normal_vector_storybook_pinlines"); p == nil || p.Render.Variants.Get("r") == nil {
		t.Errorf("storybook pinlines %+v", p)
	}
}

func TestAnalyzeCenteredLegal(t *testing.T) {
	center := func(name string, y, size float64) node {
		return pointText(name, 100, y, size, "Gotham-Book", "center")
	}
	f := build(t, 200, 280,
		group("Legal",
			center("Artist", 250, 6),
			center("Set", 260, 4),
			group("Collector", center("Top", 250, 6), center("Bottom", 260, 4)),
		),
		group("Text and Icons",
			pointText("Card Name", 20, 20, 8, "BelerenProxy-Bold", "left"),
			pointText("Typeline", 20, 160, 7, "BelerenProxy-Bold", "left"),
			ref("Textbox Reference", 20, 170, 180, 240),
		),
		layer("Layer 1", false),
	)
	_, l := analyzed(t, "classic.psd", f)
	boxes := at(l, "textBoxes")
	if at(boxes, "artist", "text", "align") != "center" || at(boxes, "artist", "rect", "y") != 250.0 {
		t.Errorf("artist %v, want it centered on the upper line", at(boxes, "artist"))
	}
	c, s := at(boxes, "collector"), at(boxes, "set")
	if at(c, "text", "align") != "right" || at(s, "text", "align") != "left" || at(c, "rect", "y") != 260.0 || at(s, "rect", "y") != 260.0 {
		t.Errorf("collector %v and set %v, want them on the lower line either side of the middle", c, s)
	}
	cx := at(c, "rect", "x").(float64) + at(c, "rect", "width").(float64)
	if cx > 100 || at(s, "rect", "x").(float64) < 100 {
		t.Errorf("collector ends at %v and set starts at %v, want the middle between them", cx, at(s, "rect", "x"))
	}
	if at(boxes, "copyright") != nil {
		t.Errorf("copyright %v, want none on a classic frame", at(boxes, "copyright"))
	}
}

func TestIndicatorKeepsOnlyColors(t *testing.T) {
	f := build(t, 100, 140,
		group("Text and Icons", pointText("Card Name", 12, 20, 8, "BelerenProxy-Bold", "left")),
		group("Color Indicator", colors("W", "U", "WU", "Gold")...),
		layer("Layer 1", false),
	)
	f.Root.Find("Color Indicator").Visible = false
	an, _ := analyzed(t, "normal.psd", f)
	p := an.Recipe.Parts.Get("normal_color_indicator")
	if p == nil || p.Render.Variants.Get("gold") != nil || p.Render.Variants.Get("wu") == nil {
		t.Errorf("indicator %+v, want its colors and no gold", p)
	}
}
