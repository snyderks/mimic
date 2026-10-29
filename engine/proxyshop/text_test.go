package proxyshop

import (
	"encoding/json"
	"testing"

	"github.com/odevine/mimic/engine/psd"
)

// shadowTextLayer builds a minimal textLayer carrying one active drop shadow,
// the way an old-bordered card's title, type line or artist credit does
func shadowTextLayer(fontSize, distance, angle float64) *textLayer {
	return &textLayer{
		l: &psd.Layer{
			Effects: &psd.Effects{
				Visible: true,
				DropShadows: []*psd.Shadow{{
					EffectBase: psd.EffectBase{Kind: psd.EffectDropShadow, Enabled: true, Present: true, Opacity: 1},
					Angle:      angle,
					Distance:   distance,
				}},
			},
		},
		t:   &psd.Text{},
		run: psd.TextRun{FontSize: fontSize},
	}
}

func TestShadowConvertsPhotoshopAngle(t *testing.T) {
	cases := []struct {
		name            string
		angle           float64
		wantEngineAngle float64
	}{
		// A global light of 135, the angle normal-classic.psd's title, type
		// and legal text share, casts the shadow down and to the right
		{"global light 135", 135, 315},
		// Straight down in Photoshop's own convention is 90
		{"straight down", 90, 0},
		// Photoshop's 0 points the shadow left
		{"left", 0, 90},
		// Photoshop's 180 points the shadow right
		{"right", 180, 270},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tl := shadowTextLayer(180, 8, c.angle)
			o := tl.shadow()
			if o == nil {
				t.Fatal("shadow() returned nil for an active drop shadow")
			}
			raw, err := json.Marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Distance float64 `json:"distance"`
				Angle    float64 `json:"angle"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Angle != c.wantEngineAngle {
				t.Errorf("angle = %v, want %v", got.Angle, c.wantEngineAngle)
			}
			wantDistance := round4(8.0 / 180.0)
			if got.Distance != wantDistance {
				t.Errorf("distance = %v, want %v", got.Distance, wantDistance)
			}
		})
	}
}

func TestShadowNilWhenInactiveOrAbsent(t *testing.T) {
	// No effects at all
	tl := &textLayer{l: &psd.Layer{}, t: &psd.Text{}, run: psd.TextRun{FontSize: 180}}
	if o := tl.shadow(); o != nil {
		t.Errorf("shadow() = %v, want nil with no effects", o)
	}

	// A drop shadow the layer style turned off entirely
	tl2 := shadowTextLayer(180, 8, 135)
	tl2.l.Effects.Visible = false
	if o := tl2.shadow(); o != nil {
		t.Errorf("shadow() = %v, want nil when the layer style is off", o)
	}

	// A drop shadow present in the style but not enabled on this layer
	tl3 := shadowTextLayer(180, 8, 135)
	tl3.l.Effects.DropShadows[0].Enabled = false
	if o := tl3.shadow(); o != nil {
		t.Errorf("shadow() = %v, want nil when the drop shadow is disabled", o)
	}
}

func TestBundledPlantin(t *testing.T) {
	cases := []struct {
		font string
		want bool
	}{
		{"PlantinMTPro-Regular", true},
		{"PlantinMTPro-Italic", true},
		{"PlantinMTPro-Bold", false},
		{"PlantinMTPro-BoldItalic", false},
		{"BelerenProxy-Bold", false},
		{"", false},
	}
	for _, c := range cases {
		if got := bundledPlantin(c.font); got != c.want {
			t.Errorf("bundledPlantin(%q) = %v, want %v", c.font, got, c.want)
		}
	}
}

func TestArtistPrefix(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"classic frame literal prefix", "Illus. Artist", "Illus. "},
		{"classic frame with trailing CR", "Illus. Artist\r", "Illus. "},
		{"modern frame nib glyph is not a prefix", "aArtist", ""},
		{"placeholder alone is not a prefix", "Artist", ""},
		{"unrelated text", "Something else", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tl := &textLayer{t: &psd.Text{Value: c.value}}
			if got := artistPrefix(tl); got != c.want {
				t.Errorf("artistPrefix(%q) = %q, want %q", c.value, got, c.want)
			}
		})
	}
}
