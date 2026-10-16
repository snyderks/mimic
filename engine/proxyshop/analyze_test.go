package proxyshop

import (
	"encoding/json"
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/psd"
)

// tree builds a document from groups and layers given top to bottom, as the
// layers panel lists them
type node struct {
	name     string
	kind     psd.Kind
	hidden   bool
	rect     image.Rectangle
	text     *psd.Text
	children []node
}

func group(name string, children ...node) node {
	return node{name: name, kind: psd.KindGroup, children: children}
}

func layer(name string, hidden bool) node {
	return node{name: name, kind: psd.KindPixel, hidden: hidden, rect: image.Rect(10, 10, 90, 130)}
}

func pointText(name string, x, y, size float64, font, just string) node {
	return node{name: name, kind: psd.KindText, rect: image.Rect(int(x), int(y-size), int(x+size*4), int(y)), text: &psd.Text{
		Transform:     [6]float64{1, 0, 0, 1, x, y},
		Justification: just,
		Runs:          []psd.TextRun{{FontSize: size, Font: font, AutoLeading: true}},
	}}
}

func build(t *testing.T, w, h int, nodes ...node) *psd.File {
	t.Helper()
	root := &psd.Layer{Kind: psd.KindGroup, Visible: true, Opacity: 255}
	var add func(parent *psd.Layer, ns []node)
	add = func(parent *psd.Layer, ns []node) {
		for _, n := range ns {
			l := &psd.Layer{Name: n.name, Kind: n.kind, Visible: !n.hidden, Opacity: 255, FillOpacity: 255, Rect: n.rect, Text: n.text, Parent: parent}
			if n.kind == psd.KindGroup {
				l.Blend = psd.BlendPassThrough
			}
			parent.Children = append(parent.Children, l)
			add(l, n.children)
		}
	}
	add(root, nodes)
	return &psd.File{Width: w, Height: h, Root: root}
}

func colors(names ...string) []node {
	var out []node
	for _, n := range names {
		out = append(out, layer(n, true))
	}
	return out
}

func TestAnalyzeFollowsProxyshopConventions(t *testing.T) {
	f := build(t, 100, 140,
		group("Legal", pointText("Artist", 10, 135, 4, "Proxyglyph", "left")),
		group("Text and Icons",
			pointText("Card Name", 12, 20, 8, "BelerenProxy-Bold", "left"),
			pointText("Mana Cost", 88, 20, 9, "PlantinMTPro-Regular", "right"),
			pointText("Typeline", 12, 80, 7, "BelerenProxy-Bold", "left"),
			pointText("Rules Text - Noncreature", 12, 90, 6, "PlantinMTPro-Regular", "left"),
			node{name: "Textbox Reference", kind: psd.KindShape, hidden: true, rect: image.Rect(10, 85, 90, 125)},
		),
		group("PT Box", colors("W", "U", "B", "Gold")...),
		group("Border", layer("Legendary Border", true), layer("Normal Border", false), layer("Full Art Border (leave on)", false)),
		group("Legendary Crown", colors("W", "U", "B", "WU", "Artifact")...),
		group("Pinlines & Textbox", colors("W", "U", "B", "WU", "W Back", "U Back", "B Back")...),
		node{name: "Art Frame", kind: psd.KindShape, hidden: true, rect: image.Rect(10, 25, 90, 75)},
		layer("Layer 1", false),
	)
	an, err := Analyze([]Source{{Key: "normal", Name: "normal.psd", File: f}}, Options{ID: "test.normal", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, np := range an.Recipe.Parts {
		parts = append(parts, np.Name+":"+np.Rule.Slot)
	}
	want := []string{
		"normal_pinlines_textbox:pinlines", "normal_pinlines_textbox_back:pinlines",
		"normal_legendary_crown:crown",
		"normal_border_full_art:", "normal_border:", "normal_border_legendary:",
		"normal_pt_box:ptBox",
	}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("parts\n got %v\nwant %v", parts, want)
	}

	var layout struct {
		Serves []Shape `json:"serves"`
		Stack  []struct {
			ID, Part, Condition string
		} `json:"stack"`
		TextBoxes map[string]json.RawMessage `json:"textBoxes"`
		Art       []map[string]any           `json:"art"`
	}
	if err := json.Unmarshal(an.Recipe.Layouts.Get("normal"), &layout); err != nil {
		t.Fatal(err)
	}
	if layout.Serves[0] != (Shape{Role: "single", Kind: "standard"}) {
		t.Errorf("serves %v", layout.Serves)
	}
	var stack []string
	for _, e := range layout.Stack {
		stack = append(stack, e.ID+"@"+e.Condition)
	}
	wantStack := []string{"pinlines_textbox@", "pinlines_textbox_back@back", "legendary_crown@legendary", "border_full_art@", "border@nonlegendary", "border_legendary@legendary", "pt_box@creature"}
	if !reflect.DeepEqual(stack, wantStack) {
		t.Errorf("stack\n got %v\nwant %v", stack, wantStack)
	}
	for _, k := range []string{"title", "mana", "type_full", "oracle", "artist"} {
		if layout.TextBoxes[k] == nil {
			t.Errorf("no %s box among %v", k, keys(layout.TextBoxes))
		}
	}
	if len(layout.Art) != 1 || layout.Art[0]["after"] != nil || layout.Art[0]["width"] != 80.0 {
		t.Errorf("art %v, want the Art Frame's box beneath the stack", layout.Art)
	}

	// The plain colors and the Back colors are separate parts, and each variant
	// shows its own layer
	r := an.Recipe.Parts.Get("normal_pinlines_textbox").Render
	if got := r.Variants.Keys(); !reflect.DeepEqual(got, []string{"w", "u", "b", "wu"}) {
		t.Errorf("pinline variants %v", got)
	}
	var v RenderVariant
	if err := json.Unmarshal(r.Variants.Get("wu"), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Show, [][]string{{"Pinlines & Textbox", "WU"}}) {
		t.Errorf("wu shows %v", v.Show)
	}
	if !r.FitPairs {
		t.Error("a color group should fit its pairs")
	}
}

func keys(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAnalyzeNotesWhatItLeavesOut(t *testing.T) {
	f := build(t, 100, 140,
		layer("Mystery", true),
		group("PT Box Dark", colors("W", "U", "B")...),
		node{name: "Selective Color 1", kind: psd.KindAdjustment},
		group("Background", colors("W", "U", "B")...),
		layer("Layer 1", false),
	)
	an, err := Analyze([]Source{{Key: "x", Name: "x.psd", File: f}}, Options{ID: "test.t"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(an.Notes, "\n")
	for _, want := range []string{`"Mystery"`, `"PT Box Dark"`, `"Selective Color 1"`} {
		if !strings.Contains(all, want) {
			t.Errorf("notes do not mention %s:\n%s", want, all)
		}
	}
	if len(an.Recipe.Parts) != 1 {
		t.Errorf("parts %v, want only the background", an.Recipe.Parts)
	}
}

// TestBorderDropsAnUnmarkedThirdVariant covers a Border group whose normal
// and legendary layers already cover every card, plus a third sibling with no
// "(leave on)" marker, such as vertical-mode.psd's plain "Full Art Border".
// Drawing that sibling for every card would always cover whichever of the two
// real borders is correct, so Analyze leaves it out rather than guess
func TestBorderDropsAnUnmarkedThirdVariant(t *testing.T) {
	f := build(t, 100, 140,
		group("Border", layer("Legendary", true), layer("Normal", false), layer("Full Art Border", true)),
		layer("Layer 1", false),
	)
	an, err := Analyze([]Source{{Key: "normal", Name: "normal.psd", File: f}}, Options{ID: "test.normal", Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, np := range an.Recipe.Parts {
		parts = append(parts, np.Name)
	}
	want := []string{"normal_border", "normal_border_legendary"}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("parts %v, want %v (no unconditional full art border)", parts, want)
	}
	if all := strings.Join(an.Notes, "\n"); !strings.Contains(all, `"Full Art Border"`) {
		t.Errorf("notes do not say why Full Art Border was left out:\n%s", all)
	}
}

func TestVariantKey(t *testing.T) {
	for _, c := range []struct {
		name, key, suffix string
		ok                bool
	}{
		{"W", "w", "", true},
		{"GW", "wg", "", true},
		{"UBR", "ubr", "", true},
		{"Gold", "gold", "", true},
		{"Colourless", "colorless", "", true},
		{"W Back", "w", "back", true},
		{"Gold-mdfc", "gold", "mdfc", true},
		{"Artifact Snow", "artifact", "snow", true},
		{"Shape", "", "", false},
		{"Brightness", "", "", false},
		{"bug", "", "", false},
	} {
		key, suffix, ok := variantKey(c.name)
		if key != c.key || suffix != c.suffix || ok != c.ok {
			t.Errorf("variantKey(%q) = %q %q %v, want %q %q %v", c.name, key, suffix, ok, c.key, c.suffix, c.ok)
		}
	}
}

func TestGuessShapes(t *testing.T) {
	for name, want := range map[string]Shape{
		"normal.psd":                    {"single", "standard"},
		"tf-front.psd":                  {"transform_front", "standard"},
		"tf-back.psd":                   {"transform_back", "standard"},
		"mdfc-back.psd":                 {"mdfc_back", "standard"},
		"pw-tf-front-extended.psd":      {"transform_front", "planeswalker"},
		"SilvanMTG/extended-mdfc-front": {"mdfc_front", "standard"},
		"saga.psd":                      {"single", "saga"},
		"battle.psd":                    {"transform_front", "battle"},
		"basic-theros.psd":              {"single", "basic_land"},
		"split.psd":                     {"split", "standard"},
		"classic-modern.psd":            {"single", "standard"},
		"WarpDandy/ArtDecoPW.psd":       {"single", "planeswalker"},
		"class.psd":                     {"single", "class"},
	} {
		if got := GuessShapes(name); got[0] != want {
			t.Errorf("GuessShapes(%q) = %v, want %v", name, got[0], want)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Pinlines & Textbox":         "pinlines_textbox",
		"Name & Title Boxes":         "name_title_boxes",
		"Full Art Border (leave on)": "full_art_border_leave_on",
		"  ":                         "layer",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyzeBasicLands(t *testing.T) {
	f := build(t, 100, 140,
		group("Legal", pointText("Artist", 10, 135, 4, "Proxyglyph", "left")),
		layer("Plains", true),
		layer("Forest", true),
		layer("Snow-Covered Forest", true),
		layer("Wastes", true),
		node{name: "Art Frame", kind: psd.KindShape, hidden: true, rect: image.Rect(10, 25, 90, 75)},
		layer("Layer 1", false),
	)
	an, err := Analyze([]Source{{Key: "basic", Name: "basic.psd", File: f}}, Options{ID: "test.t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(an.Recipe.Parts) != 2 {
		t.Fatalf("parts %v", an.Recipe.Parts)
	}
	plain := an.Recipe.Parts[0].Rule
	if plain.Slot != "pinlines" || !reflect.DeepEqual(plain.Render.Variants.Keys(), []string{"w", "g", "colorless"}) {
		t.Errorf("basic part %+v with variants %v", plain, plain.Render.Variants.Keys())
	}
	var v RenderVariant
	if err := json.Unmarshal(plain.Render.Variants.Get("g"), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Target, []string{"Forest"}) {
		t.Errorf("the forest draws %v", v.Target)
	}
	var layout struct {
		Serves []Shape
		Stack  []struct{ ID, Condition string }
	}
	if err := json.Unmarshal(an.Recipe.Layouts.Get("basic"), &layout); err != nil {
		t.Fatal(err)
	}
	if layout.Serves[0].Kind != "basic_land" || len(layout.Stack) != 2 || layout.Stack[0].Condition != "nonsnow" || layout.Stack[1].Condition != "snow" {
		t.Errorf("layout %+v", layout)
	}
}
