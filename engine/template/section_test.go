package template

import (
	"image"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/manifest"
)

func sectionRowsOf(texts ...string) []RowText {
	out := make([]RowText, len(texts))
	for i, s := range texts {
		out[i] = RowText{Parts: []TextPart{{Text: s, Src: scaleSource{}}}, X: 0, Width: 200}
	}
	return out
}

func sectionSpec(h int) (manifest.Section, TextBoxSpec) {
	sec := manifest.Section{Rect: manifest.Rect{X: 0, Y: 100, Width: 200, Height: h}, Gap: 10}
	return sec, TextBoxSpec{X: 0, Y: 100, Width: 200, Height: h, FontSize: 20, MinFontSize: 5}
}

func TestLayoutSectionSpreadsRowsEvenly(t *testing.T) {
	sec, style := sectionSpec(400)
	lay, err := LayoutSection(sec, style, image.Rectangle{}, sectionRowsOf("a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if lay.Size != 20 || len(lay.Rows) != 3 {
		t.Fatalf("size %g, %d rows", lay.Size, len(lay.Rows))
	}
	h := lay.Rows[0].InkBottom - lay.Rows[0].InkTop
	if h <= 0 {
		t.Fatalf("row height %d", h)
	}
	above := lay.Rows[0].Top - 100
	between := lay.Rows[1].Top - lay.Rows[0].InkBottom
	below := 500 - lay.Rows[2].InkBottom
	if d := above - between; d < -1 || d > 1 {
		t.Errorf("above %d, between %d", above, between)
	}
	if d := below - between; d < -1 || d > 1 {
		t.Errorf("below %d, between %d", below, between)
	}
	if mid := lay.Between(0); mid <= lay.Rows[0].InkBottom || mid >= lay.Rows[1].Top {
		t.Errorf("between rows 0 and 1 is %d", mid)
	}
}

func TestLayoutSectionWeighsTheSpaceBetweenRows(t *testing.T) {
	sec, style := sectionSpec(400)
	sec.Spread = &manifest.Spread{Outside: 1, Inside: 2}
	lay, err := LayoutSection(sec, style, image.Rectangle{}, sectionRowsOf("a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	above := lay.Rows[0].Top - 100
	between := lay.Rows[1].Top - lay.Rows[0].InkBottom
	if d := between - 2*above; d < -2 || d > 2 {
		t.Errorf("above %d, between %d, want twice", above, between)
	}
}

func TestLayoutSectionShrinksEveryRowTogether(t *testing.T) {
	sec, style := sectionSpec(120)
	long := strings.Repeat("word ", 40)
	lay, err := LayoutSection(sec, style, image.Rectangle{}, sectionRowsOf(long, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if lay.Size >= 20 || lay.Size < 5 {
		t.Fatalf("size %g, want shrunk", lay.Size)
	}
	for _, r := range lay.Rows {
		if r.Box.FontSize != lay.Size {
			t.Errorf("a row is set at %g, not the shared %g", r.Box.FontSize, lay.Size)
		}
	}
	if last := lay.Rows[1]; last.InkBottom > 220 {
		t.Errorf("the last row ends at %d, past the region", last.InkBottom)
	}
}

func TestLayoutSectionHoldsRoomForAHeader(t *testing.T) {
	sec, style := sectionSpec(400)
	rows := sectionRowsOf("a", "b")
	rows[1].Header = 50
	lay, err := LayoutSection(sec, style, image.Rectangle{}, rows)
	if err != nil {
		t.Fatal(err)
	}
	if r := lay.Rows[1]; r.InkTop-r.Top != 50 {
		t.Errorf("row 1 text starts %d below its top, want the header's 50", r.InkTop-r.Top)
	}
}

// TestLayoutSectionSettlesRowsAroundAnAvoidedRectangle covers a rectangle that
// makes a row wrap narrower and grow, which moves the rows under it down. A
// single pass measures those rows at the tops they had before the rectangle, so
// the rows settle only because the pass repeats until the tops stop moving
func TestLayoutSectionSettlesRowsAroundAnAvoidedRectangle(t *testing.T) {
	sec, style := sectionSpec(400)
	rows := sectionRowsOf(strings.Repeat("word ", 12), "b", "c")
	avoid := image.Rect(60, 100, 200, 200)
	plain, err := LayoutSection(sec, style, image.Rectangle{}, rows)
	if err != nil {
		t.Fatal(err)
	}
	lay, err := LayoutSection(sec, style, avoid, rows)
	if err != nil {
		t.Fatal(err)
	}

	// The rectangle has to rewrap the first row and make it taller, or the rest
	// of the test covers nothing
	was := plain.Rows[0].InkBottom - plain.Rows[0].InkTop
	got := lay.Rows[0].InkBottom - lay.Rows[0].InkTop
	if got <= was {
		t.Fatalf("row 0 is %d tall beside the rectangle and %d without it, want taller", got, was)
	}

	// Every row lands at the top it was measured at, so none is laid out for a
	// place it does not end up in
	tops := make([]int, len(lay.Rows))
	for i, r := range lay.Rows {
		tops[i] = r.Box.Y
	}
	again, err := layoutRows(sec, style, &placedAvoid{rect: avoid, tops: tops}, rows)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range again.Rows {
		if r.Box.Y != tops[i] || r.InkTop != lay.Rows[i].InkTop || r.InkBottom != lay.Rows[i].InkBottom {
			t.Errorf("row %d measured at %d lands at %d, %d..%d, want %d..%d", i, tops[i], r.Box.Y,
				r.InkTop, r.InkBottom, lay.Rows[i].InkTop, lay.Rows[i].InkBottom)
		}
	}

	// One pass puts a row somewhere it was not measured for, which is what
	// repeating the pass settles
	first := make([]int, len(plain.Rows))
	for i, r := range plain.Rows {
		first[i] = r.Box.Y
	}
	one, err := layoutRows(sec, style, &placedAvoid{rect: avoid, tops: first}, rows)
	if err != nil {
		t.Fatal(err)
	}
	if one.Rows[1].Box.Y == lay.Rows[1].Box.Y {
		t.Error("one pass settles this fixture, so it does not cover the repeat")
	}
}

func TestScaleSectionScalesItsPixels(t *testing.T) {
	sec := manifest.Section{
		Rect: manifest.Rect{X: 100, Y: 200, Width: 300, Height: 400}, Text: manifest.TextStyle{FontSize: 40},
		Gap: 20, Header: &manifest.Header{For: "loyalty", Height: 60},
		Items:   []manifest.RowItem{{Y: 1000, Step: 50, Rect: &manifest.Rect{X: 10, Y: 20, Width: 30, Height: 40}}},
		Divider: &manifest.RowPart{Part: "d", Y: 500},
	}
	got := ScaleSection(sec, 0.5)
	if got.Rect.X != 50 || got.Text.FontSize != 20 || got.Gap != 10 || got.Header.Height != 30 ||
		got.Items[0].Y != 500 || got.Items[0].Step != 25 || got.Items[0].Rect.Width != 15 || got.Divider.Y != 250 {
		t.Errorf("scaled = %+v", got)
	}
	if sec.Items[0].Y != 1000 || sec.Header.Height != 60 {
		t.Error("scaling changed the section it was given")
	}
}
