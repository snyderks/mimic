package proxyshop

import (
	"encoding/json"
	"testing"

	"github.com/odevine/mimic/engine/psd"
)

// sideText builds a minimal type layer for legalOnTheSide, whose authored
// space is sideways: tx maps to the final row and ty to position along it
func sideText(name, value string, tx, ty, fontSize float64) *psd.Layer {
	return &psd.Layer{
		Name: name,
		Text: &psd.Text{
			Transform:     [6]float64{1, 0, 0, 1, tx, ty},
			Value:         value,
			Justification: "left",
			Runs:          []psd.TextRun{{FontSize: fontSize}},
		},
	}
}

// TestLegalOnTheSidePutsArtistOnTheSetRow covers a split card whose Legal
// group has a Collector group (Top for the number, Bottom combining the set
// code, language and artist credit on one authored line) alongside the
// standalone Artist and Set layers Proxyshop draws when Collector is off. The
// artist credit must land on Bottom's row, not its own standalone row, or it
// lands on the same row as the collector number instead and overlaps it
func TestLegalOnTheSidePutsArtistOnTheSetRow(t *testing.T) {
	top := sideText("Top", "001/999 M", 345, 330, 30)
	bottom := sideText("Bottom", "SET • EN aArtist", 269, 329, 30)
	set := sideText("Set", " • EN", 272, 332, 30)
	artist := sideText("Artist", "aArtist", 346, 332, 37)
	legal := &psd.Layer{Name: "Legal", Kind: psd.KindGroup, Children: []*psd.Layer{
		{Name: "Collector", Kind: psd.KindGroup, Children: []*psd.Layer{top, bottom}},
		set, artist,
	}}
	root := &psd.Layer{Kind: psd.KindGroup, Children: []*psd.Layer{legal}}
	f := &psd.File{Width: 4440, Height: 3264, Root: root}
	st := &layoutState{src: Source{Name: "split.psd"}, f: f}
	a := &analyzer{}

	boxes := Object{}
	add := func(key string, box Object) { boxes = append(boxes, Member{Key: key, Value: mustRaw(box)}) }
	a.legalOnTheSide(st, add)

	rectOf := func(key string) (x, y int) {
		t.Helper()
		raw := boxes.Get(key)
		if raw == nil {
			t.Fatalf("no %q box", key)
		}
		var box struct {
			Rect struct{ X, Y int } `json:"rect"`
		}
		if err := json.Unmarshal(raw, &box); err != nil {
			t.Fatal(err)
		}
		return box.Rect.X, box.Rect.Y
	}

	setX, setY := rectOf("set")
	artistX, artistY := rectOf("artist")
	_, collectorY := rectOf("collector")

	if setY != artistY {
		t.Errorf("set row y=%d, artist row y=%d, want the same row", setY, artistY)
	}
	if artistX <= setX {
		t.Errorf("artist x=%d, want it to open to the right of set's x=%d", artistX, setX)
	}
	if collectorY == setY {
		t.Errorf("collector shares the set/artist row (y=%d), want its own row", collectorY)
	}
}
