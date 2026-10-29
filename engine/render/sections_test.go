package render

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/card"
	"github.com/odevine/mimic/engine/fonts"
	"github.com/odevine/mimic/engine/frame"
	"github.com/odevine/mimic/engine/manifest"
	"github.com/odevine/mimic/engine/template"
)

// imageNameRe reads a test image's place and color from its name, as
// img/name-x-y-w-h-rrggbb.png, so a manifest under testdata needs no image files
var imageNameRe = regexp.MustCompile(`-(\d+)-(\d+)-(\d+)-(\d+)-([0-9a-f]{6})\.png$`)

// sectionTemplate loads a manifest from testdata/sections with each image it
// names drawn as a flat rectangle on a canvas the size of the layout's
func sectionTemplate(t *testing.T, name string) *template.Template {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "sections", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	m, ds := manifest.DecodeStrict(raw)
	if ds.HasErrors() {
		t.Fatalf("%s: %v", name, ds)
	}
	for _, d := range ds {
		t.Errorf("%s: unexpected diagnostic %v", name, d)
	}
	var canvas image.Point
	for _, l := range m.Layouts {
		canvas = image.Pt(l.Canvas.Width, l.Canvas.Height)
	}
	files := map[string][]byte{}
	for _, p := range m.Parts {
		for _, v := range p.Variants {
			files[v.Path] = namedImage(t, v.Path, canvas)
		}
	}
	return mustLoad(t, overlay{manifest: raw, files: files})
}

// namedImage draws the rectangle a test image's name gives on a canvas-sized PNG
func namedImage(t *testing.T, path string, canvas image.Point) []byte {
	t.Helper()
	m := imageNameRe.FindStringSubmatch(path)
	if m == nil {
		t.Fatalf("image %q does not name its place and color", path)
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	rgb, _ := hex.DecodeString(m[5])
	r := image.Rect(n(1), n(2), n(1)+n(3), n(2)+n(4))
	img := image.NewNRGBA(image.Rectangle{Max: canvas})
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, color.NRGBA{rgb[0], rgb[1], rgb[2], 0xFF})
		}
	}
	return encodePNG(t, img)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// frameKeys are the frame keys of the face a request renders
func frameKeys(req template.RenderRequest) frame.Keys {
	return frame.DeriveFace(req.FaceCard(), req.FaceSide())
}

func cardFixture(t *testing.T, name string) *card.Data {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "card", "testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := card.FromScryfallJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sectionRows are the drawn text rows of a section, in order
func sectionRows(t *testing.T, d *Description, section string) []Item {
	t.Helper()
	var rows []Item
	for i := 0; ; i++ {
		it, ok := items(t, d)[fmt.Sprintf("text:%s.rows[%d]", section, i)]
		if !ok {
			return rows
		}
		rows = append(rows, it)
	}
}

// checkStacked fails unless the rows run down the region in order without
// overlapping, inside it
func checkStacked(t *testing.T, rows []Item, region image.Rectangle) {
	t.Helper()
	for i, r := range rows {
		if !r.Drawn || r.Authored.Empty() {
			t.Errorf("row %d is not drawn: %+v", i, r)
			continue
		}
		if !r.Authored.In(region) {
			t.Errorf("row %d at %v leaves the region %v", i, r.Authored, region)
		}
		if i > 0 && r.Authored.Min.Y <= rows[i-1].Authored.Max.Y {
			t.Errorf("row %d at %v does not start below row %d at %v", i, r.Authored, i-1, rows[i-1].Authored)
		}
	}
}

func center(r image.Rectangle) image.Point {
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

func renderCard(t *testing.T, tm *template.Template, req template.RenderRequest) image.Image {
	t.Helper()
	buf, err := Render(context.Background(), tm, req)
	if err != nil {
		t.Fatal(err)
	}
	return buf.ToImage(8)
}

func sameColor(t *testing.T, img image.Image, p image.Point, want string) {
	t.Helper()
	r, g, b, _ := img.At(p.X, p.Y).RGBA()
	got := fmt.Sprintf("%02x%02x%02x", r>>8, g>>8, b>>8)
	if got != want {
		t.Errorf("pixel at %v is %s, want %s", p, got, want)
	}
}

func TestPlaneswalkerAbilitiesAreRowsWithBadges(t *testing.T) {
	tm := sectionTemplate(t, "planeswalker")
	jace := template.RenderRequest{Card: cardFixture(t, "jace_the_mind_sculptor")}
	d := describe(t, tm, jace)
	got := items(t, d)
	if s := got["section:abilities"]; !s.Drawn || s.Authored != image.Rect(60, 620, 684, 1000) {
		t.Errorf("section = %+v", s)
	}
	rows := sectionRows(t, d, "abilities")
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	checkStacked(t, rows, image.Rect(60, 620, 684, 1000))
	for i, want := range []string{"badge_plus", "badge_zero", "badge_minus", "badge_minus"} {
		badge := got[fmt.Sprintf("layer:abilities.rows[%d].items[0]", i)]
		if badge.Part != want || !badge.Drawn {
			t.Errorf("row %d badge = %+v, want %s", i, badge, want)
			continue
		}
		// The badge was authored centered on y 720, the row's text center
		if dy := center(badge.Authored).Y - center(rows[i].Authored).Y; dy < -1 || dy > 1 {
			t.Errorf("row %d badge centers at %d, the row at %d", i, center(badge.Authored).Y, center(rows[i].Authored).Y)
		}
		if cost := got[fmt.Sprintf("text:abilities.rows[%d].items[1]", i)]; !cost.Drawn {
			t.Errorf("row %d has no cost text", i)
		}
	}
	// The text starts in the body column, past the badges
	if rows[0].Authored.Min.X < 100 {
		t.Errorf("row 0 starts at x %d, before the body column", rows[0].Authored.Min.X)
	}
	// Four abilities take the tall text box
	if !got["layer:textbox_tall"].Drawn || got["layer:textbox"].Drawn {
		t.Errorf("tall %v, short %v", got["layer:textbox_tall"].Drawn, got["layer:textbox"].Drawn)
	}
	// The second and fourth rows are shaded, from between the rows to the image's edge
	for _, i := range []int{1, 3} {
		band := got[fmt.Sprintf("layer:abilities.bands[%d]", i)]
		if !band.Drawn || band.Authored.Min.Y >= rows[i].Authored.Min.Y || band.Authored.Min.Y <= rows[i-1].Authored.Max.Y {
			t.Errorf("band %d at %v, row %d at %v", i, band.Authored, i, rows[i].Authored)
		}
	}
	if _, ok := got["layer:abilities.bands[0]"]; ok {
		t.Error("the first row is shaded")
	}
	if !got["text:loyalty"].Drawn {
		t.Error("the starting loyalty is not drawn")
	}

	img := renderCard(t, tm, jace)
	b1 := got["layer:abilities.rows[0].items[0]"].Authored
	sameColor(t, img, image.Pt(b1.Min.X+2, b1.Min.Y+2), "30a030")
	b3 := got["layer:abilities.rows[2].items[0]"].Authored
	sameColor(t, img, image.Pt(b3.Min.X+2, b3.Min.Y+2), "a03030")
	// Shaded under the second row, plain under the first, beside the text column
	sameColor(t, img, image.Pt(682, center(rows[1].Authored).Y), "b0a890")
	sameColor(t, img, image.Pt(682, center(rows[0].Authored).Y), "e8e0d0")
}

// TestHidingWhatASectionDrawsLeavesItOut covers a hide that names what a
// description reports for a section, by the piece and by the whole section. The
// rows are laid out either way, so what is left is in the same place
func TestHidingWhatASectionDrawsLeavesItOut(t *testing.T) {
	tm := sectionTemplate(t, "planeswalker")
	jace := template.RenderRequest{Card: cardFixture(t, "jace_the_mind_sculptor")}
	d := describe(t, tm, jace)
	got := items(t, d)
	rows := sectionRows(t, d, "abilities")
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	badge := got["layer:abilities.rows[0].items[0]"].Authored
	if badge.Empty() {
		t.Fatal("the first badge is not placed")
	}
	at := image.Pt(badge.Min.X+2, badge.Min.Y+2)
	band := image.Pt(682, center(rows[1].Authored).Y)
	plain := renderCard(t, tm, jace)
	sameColor(t, plain, at, "30a030")
	sameColor(t, plain, band, "b0a890")

	// The badge beside the first row goes, leaving the text box under it, and the
	// band under the second row stays
	one := jace
	one.Hide = []string{"layer:abilities.rows[0].items[0]"}
	img := renderCard(t, tm, one)
	sameColor(t, img, at, "f0ead6")
	sameColor(t, img, band, "b0a890")

	// The whole section goes, badges and bands with it
	all := jace
	all.Hide = []string{"section:abilities"}
	img = renderCard(t, tm, all)
	sameColor(t, img, at, "f0ead6")
	sameColor(t, img, band, "e8e0d0")

	// Hiding draws less and lays out the same, so a description still reports
	// every row where it was
	after := sectionRows(t, describe(t, tm, all), "abilities")
	if len(after) != len(rows) {
		t.Fatalf("rows with the section hidden = %d, want %d", len(after), len(rows))
	}
	for i, r := range after {
		if r.Authored != rows[i].Authored {
			t.Errorf("row %d is at %v with the section hidden, was %v", i, r.Authored, rows[i].Authored)
		}
	}
}

func TestPlaneswalkerWithThreeAbilitiesTakesTheShortBox(t *testing.T) {
	tm := sectionTemplate(t, "planeswalker")
	got := items(t, describe(t, tm, template.RenderRequest{Card: cardFixture(t, "liliana_of_the_veil")}))
	if got["layer:textbox_tall"].Drawn || !got["layer:textbox"].Drawn {
		t.Errorf("tall %v, short %v", got["layer:textbox_tall"].Drawn, got["layer:textbox"].Drawn)
	}
	if _, ok := got["text:abilities.rows[2]"]; !ok {
		t.Error("no third row")
	}
	if _, ok := got["text:abilities.rows[3]"]; ok {
		t.Error("a fourth row")
	}
}

func TestAStaticAbilitySitsInThePlainColumnWithNoBadge(t *testing.T) {
	tm := sectionTemplate(t, "planeswalker")
	valki := cardFixture(t, "valki_god_of_lies")
	d := describe(t, tm, template.RenderRequest{Card: valki, Face: 1})
	got := items(t, d)
	rows := sectionRows(t, d, "abilities")
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	if _, ok := got["layer:abilities.rows[0].items[0]"]; ok {
		t.Error("the static ability has a badge")
	}
	if x := rows[0].Authored.Min.X; x < 70 || x >= 100 {
		t.Errorf("the static ability starts at x %d, want the plain column from 70", x)
	}
	if rows[1].Authored.Min.X < 100 {
		t.Errorf("an ability starts at x %d, before the body column", rows[1].Authored.Min.X)
	}
}

func TestSagaChaptersAreRowsUnderTheDescription(t *testing.T) {
	tm := sectionTemplate(t, "saga")
	saga := template.RenderRequest{Card: cardFixture(t, "the_eldest_reborn")}
	d := describe(t, tm, saga)
	got := items(t, d)
	rows := sectionRows(t, d, "chapters")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	checkStacked(t, rows, image.Rect(80, 260, 360, 920))
	if !got["text:description"].Drawn {
		t.Error("the description is not drawn")
	}
	if o := got["text:oracle"]; o.Drawn {
		t.Errorf("the oracle box draws what the section and the description already print: %+v", o)
	}
	for i, want := range []string{"chapter_i", "chapter_ii", "chapter_iii"} {
		if it := got[fmt.Sprintf("layer:chapters.rows[%d].items[0]", i)]; it.Part != want {
			t.Errorf("row %d numeral = %q, want %q", i, it.Part, want)
		}
	}
	for i := range 2 {
		div := got[fmt.Sprintf("layer:chapters.dividers[%d]", i)]
		y := center(div.Authored).Y
		if !div.Drawn || y <= rows[i].Authored.Max.Y || y >= rows[i+1].Authored.Min.Y {
			t.Errorf("divider %d at %v, between %v and %v", i, div.Authored, rows[i].Authored, rows[i+1].Authored)
		}
	}
	// The spread gives the space between rows half as much again as above and below
	above := rows[0].Authored.Min.Y - 260
	between := rows[1].Authored.Min.Y - rows[0].Authored.Max.Y
	if between <= above {
		t.Errorf("space between rows %d is not more than above them %d", between, above)
	}
}

func TestSagaChaptersSharingARowStackTheirNumerals(t *testing.T) {
	tm := sectionTemplate(t, "saga")
	saga := &card.Data{Name: "Test Saga", TypeLine: "Enchantment — Saga", Layout: "saga",
		OracleText: "(Reminder.)\nI, II — Draw a card.\nIII — Discard a card."}
	got := items(t, describe(t, tm, template.RenderRequest{Card: saga}))
	one, two := got["layer:chapters.rows[0].items[0][0]"], got["layer:chapters.rows[0].items[0][1]"]
	if one.Part != "chapter_i" || two.Part != "chapter_ii" {
		t.Fatalf("numerals = %q and %q", one.Part, two.Part)
	}
	if two.Authored.Min.Y-one.Authored.Min.Y != 66 {
		t.Errorf("numerals sit %d apart, want the step of 66", two.Authored.Min.Y-one.Authored.Min.Y)
	}
	row := got["text:chapters.rows[0]"].Authored
	if mid := (one.Authored.Min.Y + two.Authored.Max.Y) / 2; mid-center(row).Y < -1 || mid-center(row).Y > 1 {
		t.Errorf("the numerals center at %d, the row at %d", mid, center(row).Y)
	}
}

func TestClassLevelsSitUnderTheirLevelBars(t *testing.T) {
	tm := sectionTemplate(t, "class")
	class := template.RenderRequest{Card: cardFixture(t, "wizard_class")}
	d := describe(t, tm, class)
	got := items(t, d)
	rows := sectionRows(t, d, "levels")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	checkStacked(t, rows, image.Rect(372, 210, 702, 930))
	if _, ok := got["layer:levels.rows[0].items[0]"]; ok {
		t.Error("the first level has a bar")
	}
	for i := 1; i < 3; i++ {
		bar := got[fmt.Sprintf("layer:levels.rows[%d].items[0]", i)]
		if !bar.Drawn || bar.Authored.Dy() != 40 {
			t.Errorf("row %d bar = %+v", i, bar)
			continue
		}
		// The bar heads the row, in the room the header holds above its text
		if gap := rows[i].Authored.Min.Y - bar.Authored.Min.Y; gap < 40 || gap > 60 {
			t.Errorf("row %d text starts %d below its bar", i, gap)
		}
		for j := 1; j <= 2; j++ {
			if it := got[fmt.Sprintf("text:levels.rows[%d].items[%d]", i, j)]; !it.Drawn || !it.Authored.Overlaps(bar.Authored) {
				t.Errorf("row %d item %d at %v is not on its bar %v", i, j, it.Authored, bar.Authored)
			}
		}
	}
	img := renderCard(t, tm, class)
	bar := got["layer:levels.rows[1].items[0]"].Authored
	sameColor(t, img, image.Pt(bar.Max.X-3, bar.Min.Y+2), "605040")
}

func TestLevelerBoxesSplitTheTiers(t *testing.T) {
	tm := sectionTemplate(t, "leveler")
	got := items(t, describe(t, tm, template.RenderRequest{Card: cardFixture(t, "student_of_warfare")}))
	for _, key := range []string{"level_up", "level_label_1", "level_1", "level_1_text", "level_1_pt", "level_label_2", "level_2", "level_2_text", "level_2_pt", "pt"} {
		if !got["text:"+key].Drawn {
			t.Errorf("%s is not drawn: %+v", key, got["text:"+key])
		}
	}
	if got["text:oracle"].Drawn {
		t.Error("the oracle box draws the text the leveler boxes print")
	}
	if l := got["layer:tiers"]; len(l.Images) != 1 || !strings.Contains(l.Images[0], "d0c8a0") {
		t.Errorf("tiers = %+v, want the white image", l)
	}
}

// boxText is the text a box of a layout draws for a card, as Render would build it
func boxText(t *testing.T, tm *template.Template, req template.RenderRequest, key string) string {
	t.Helper()
	shapes := template.Classify(req.Card)
	layout, _ := tm.Layout(shapes[req.Face])
	geo := layout.Geometry(0)
	d := req.FaceCard()
	fc := newFaces(req, shapes, d, frameKeys(req))
	boxes := resolveBoxes(layout, geo, fc, nil)
	boxes.take(resolveSections(layout, geo.Scale, fc, nil))
	fr := fonts.NewResolver(tm.Fonts(), nil, nil)
	job := boxes.byKey[key]
	jd, _ := fc.get(job.face)
	var out []string
	for _, p := range textParts(job.draws, job.spec, jd, boxes.takenBy(job.face), symbols{}, fr, "") {
		out = append(out, p.Text)
	}
	return strings.Join(out, "|")
}

func TestMutateBoxTakesItsLineFromTheRules(t *testing.T) {
	tm := sectionTemplate(t, "mutate")
	req := template.RenderRequest{Card: cardFixture(t, "gemrazer")}
	if got := boxText(t, tm, req, "mutate"); !strings.HasPrefix(got, "Mutate {1}{G}{G} (") {
		t.Errorf("mutate = %q", got)
	}
	if got := boxText(t, tm, req, "oracle"); !strings.HasPrefix(got, "Reach, trample\n") {
		t.Errorf("oracle = %q", got)
	}
	if !items(t, describe(t, tm, req))["layer:mutate_box"].Drawn {
		t.Error("the mutate box art is not drawn")
	}
}

func TestPrototypeBoxesInThePrototypesColor(t *testing.T) {
	tm := sectionTemplate(t, "prototype")
	req := template.RenderRequest{Card: cardFixture(t, "arcane_proxy")}
	for key, want := range map[string]string{
		"prototype_mana": "{1}{U}{U}",
		"prototype_pt":   "2/1",
		"pt":             "4/3",
	} {
		if got := boxText(t, tm, req, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := boxText(t, tm, req, "oracle"); strings.Contains(got, "Prototype") {
		t.Errorf("oracle keeps the prototype line: %q", got)
	}
	if l := items(t, describe(t, tm, req))["layer:proto"]; len(l.Images) != 1 || !strings.Contains(l.Images[0], "4060c0") {
		t.Errorf("prototype box = %+v, want the blue image", l)
	}
}

func TestBattleDrawsItsDefense(t *testing.T) {
	tm := sectionTemplate(t, "battle")
	req := template.RenderRequest{Card: cardFixture(t, "invasion_of_ergamon")}
	d := describe(t, tm, req)
	if d.Delivered != image.Pt(1039, 744) {
		t.Errorf("delivered %v, want landscape", d.Delivered)
	}
	if got := boxText(t, tm, req, "defense"); got != "5" {
		t.Errorf("defense = %q", got)
	}
	if !items(t, d)["text:defense"].Drawn {
		t.Error("the defense is not drawn")
	}
}

func TestPlaneSplitsItsChaosAbility(t *testing.T) {
	tm := sectionTemplate(t, "planar")
	req := template.RenderRequest{Card: cardFixture(t, "tazeem")}
	if got := boxText(t, tm, req, "oracle"); got != "Creatures can't block." {
		t.Errorf("oracle = %q", got)
	}
	if got := boxText(t, tm, req, "chaos"); got != "Whenever chaos ensues, draw a card for each land you control." {
		t.Errorf("chaos = %q", got)
	}
	got := items(t, describe(t, tm, req))
	if !got["layer:chaos_symbol"].Drawn || !got["text:chaos"].Drawn {
		t.Error("the chaos ability is not drawn")
	}
	phenomenon := &card.Data{Name: "Test Phenomenon", TypeLine: "Phenomenon", Layout: "planar",
		OracleText: "When you encounter this, each player shuffles.\nThen planeswalk."}
	got = items(t, describe(t, tm, template.RenderRequest{Card: phenomenon}))
	if got["layer:chaos_symbol"].Drawn || got["text:chaos"].Drawn {
		t.Error("a phenomenon draws a chaos ability")
	}
}

func TestAdventureDrawsBothHalvesInTheirColumns(t *testing.T) {
	tm := sectionTemplate(t, "adventure")
	req := template.RenderRequest{Card: cardFixture(t, "bonecrusher_giant")}
	for key, want := range map[string]string{
		"title": "Bonecrusher Giant", "adventure_title": "Stomp", "adventure_mana": "{1}{R}",
		"adventure_type": "Instant — Adventure",
	} {
		if got := boxText(t, tm, req, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	got := items(t, describe(t, tm, req))
	if !got["text:oracle"].Drawn || !got["text:adventure_oracle"].Drawn || !got["layer:adventure_box"].Drawn {
		t.Errorf("oracle %v, adventure oracle %v, adventure box %v", got["text:oracle"].Drawn, got["text:adventure_oracle"].Drawn, got["layer:adventure_box"].Drawn)
	}
}

func TestModalFaceNamesTheOtherFace(t *testing.T) {
	tm := sectionTemplate(t, "mdfc")
	req := template.RenderRequest{Card: cardFixture(t, "valki_god_of_lies")}
	// Tibalt, Cosmic Impostor is "Legendary Planeswalker — Tibalt", a single
	// subtype, which flipside_type prefers over the supertype
	if got := boxText(t, tm, req, "flipside_type"); got != "Tibalt" {
		t.Errorf("flipside_type = %q", got)
	}
	if got := boxText(t, tm, req, "flipside_mana"); got != "{5}{B}{R}" {
		t.Errorf("flipside_mana = %q", got)
	}
}

func TestArtWithNoEntryToFollowSitsBeneathTheStack(t *testing.T) {
	tm := describeTemplate(t, func(m *manifest.Manifest) {
		l := m.Layouts["test"]
		l.Art = []manifest.ArtWindow{{X: 60, Y: 180, Width: 600, Height: 400}}
		half := 0.5
		l.Stack[0].Opacity = &half
		m.Layouts["test"] = l
	})
	art := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for i := range art.Pix {
		art.Pix[i] = 0xFF
	}
	for i := 0; i < len(art.Pix); i += 4 {
		art.Pix[i+1], art.Pix[i+2] = 0, 0
	}
	req := template.RenderRequest{Card: creature(card.White), Art: art}
	d := describe(t, tm, req)
	if first := d.Items[0]; first.Kind != ItemArt || first.After != "" || !first.Drawn {
		t.Errorf("first item = %+v, want the art beneath the stack", first)
	}
	img := renderCard(t, tm, req)
	// The half opaque frame lies over the red art, so the art shows through it
	r, g, _, _ := img.At(300, 300).RGBA()
	if r>>8 < g>>8+0x30 {
		t.Errorf("pixel over the art = %v, want the frame over red", img.At(300, 300))
	}
}
