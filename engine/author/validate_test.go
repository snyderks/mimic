package author

import (
	"encoding/json"
	"image"
	"image/color"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/odevine/mimic/engine/card"
	"github.com/odevine/mimic/engine/manifest"
)

// edited is the small template with one file replaced
func edited(t *testing.T, name, data string) fstest.MapFS {
	t.Helper()
	fsys := smallFS(t)
	fsys[name] = &fstest.MapFile{Data: []byte(data)}
	return fsys
}

func TestValidateAcceptsAGoodTemplateWithoutWritingAnything(t *testing.T) {
	if ds := Validate(smallFS(t), Options{}); ds.HasErrors() {
		t.Fatal(ds)
	}
}

// Each check an author is told about has a template that fails it
func TestValidateFindsWhatIsWrong(t *testing.T) {
	tests := []struct {
		name string
		fs   func(t *testing.T) fstest.MapFS
		path string
		text string
	}{
		{"unknown material", func(t *testing.T) fstest.MapFS {
			f := smallFS(t)
			f["bg/purple.png"] = f["bg/w.png"]
			return f
		}, "parts.bg.variants.purple", "not a material"},
		{"a part with no images", func(t *testing.T) fstest.MapFS {
			return edited(t, "template.json", strings.Replace(stdTemplate, `"bg":   {"slot": "background", "origin": {"x": 4, "y": 6}},`, `"bg": {"slot": "background", "dir": "empty"}, `, 1))
		}, "parts.bg.dir", "cannot be read"},
		{"a folder with no images", func(t *testing.T) fstest.MapFS {
			f := smallFS(t)
			f["notes/readme.txt"] = &fstest.MapFile{Data: []byte("words")}
			f["template.json"] = &fstest.MapFile{Data: []byte(strings.Replace(stdTemplate, `"bg":   {"slot": "background", "origin": {"x": 4, "y": 6}},`, `"bg": {"slot": "background", "dir": "notes"}, `, 1))}
			return f
		}, "parts.bg", "no images"},
		{"a missing file", func(t *testing.T) fstest.MapFS {
			return edited(t, "template.json", strings.Replace(stdTemplate, `"marks/a.png"`, `"marks/gone.png"`, 1))
		}, "parts.mark", "gone.png"},
		{"images of different sizes", func(t *testing.T) fstest.MapFS {
			f := smallFS(t)
			f["bg/r.png"] = &fstest.MapFile{Data: pngOf(t, 30, image.Rect(1, 1, 5, 5), color.NRGBA{A: 255})}
			return f
		}, "parts.bg", "one size"},
		{"two layouts serving one shape", func(t *testing.T) fstest.MapFS {
			f := smallFS(t)
			f["layouts/again.json"] = f["layouts/normal.json"]
			return f
		}, "serves", "both serve"},
		{"a stack naming a missing part", func(t *testing.T) fstest.MapFS {
			return edited(t, "layouts/normal.json", strings.Replace(stdLayout, `"part": "bg"}`, `"part": "ghost"}`, 1))
		}, "stack[0].part", "ghost"},
		{"a feature reference to an undeclared feature", func(t *testing.T) fstest.MapFS {
			return edited(t, "layouts/normal.json", strings.Replace(stdLayout, `{"part": "bg"}`, `{"part": "bg", "condition": "feature:glow"}`, 1))
		}, "stack[0].condition", "glow"},
		{"an unknown field in template.json", func(t *testing.T) fstest.MapFS {
			return edited(t, "template.json", strings.Replace(stdTemplate, `"name": "Small"`, `"name": "Small", "colour": "red"`, 1))
		}, "template.json", "colour"},
		{"an unknown field in a layout", func(t *testing.T) fstest.MapFS {
			return edited(t, "layouts/normal.json", strings.Replace(stdLayout, `"canvas"`, `"colour": 1, "canvas"`, 1))
		}, "layouts.normal.colour", "not a field"},
		{"an unknown field inside a text box", func(t *testing.T) fstest.MapFS {
			return edited(t, "layouts/normal.json", strings.Replace(stdLayout, `"fontSize": 12`, `"fontSize": 12, "weight": "bold"`, 1))
		}, "", "weight"},
		{"a bad version", func(t *testing.T) fstest.MapFS {
			return edited(t, "template.json", strings.Replace(stdTemplate, `"1.2.3"`, `"1.2"`, 1))
		}, "version", "not a version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := Validate(tt.fs(t), Options{})
			if !ds.HasErrors() {
				t.Fatalf("no error in %v", ds)
			}
			wantError(t, ds, tt.path, tt.text)
		})
	}
}

func TestValidateTellsAnAuthorEveryProblemAtOnce(t *testing.T) {
	f := smallFS(t)
	f["template.json"] = &fstest.MapFile{Data: []byte(strings.Replace(strings.Replace(stdTemplate, `"1.2.3"`, `"x"`, 1), `"marks/a.png"`, `"marks/gone.png"`, 1))}
	f["layouts/normal.json"] = &fstest.MapFile{Data: []byte(strings.Replace(stdLayout, `"part": "bg"}`, `"part": "ghost"}`, 1))}
	ds := Validate(f, Options{})
	wantError(t, ds, "version", "not a version")
	wantError(t, ds, "stack[0].part", "ghost")
}

func TestSchemasDescribeTheSourceForm(t *testing.T) {
	for name, gen := range map[string]func() ([]byte, error){"template": SourceSchema, "layout": LayoutSchema} {
		raw, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Schema     string                     `json:"$schema"`
			Type       string                     `json:"type"`
			Properties map[string]map[string]any  `json:"properties"`
			Required   []string                   `json:"required"`
			Extra      any                        `json:"additionalProperties"`
			Defs       map[string]json.RawMessage `json:"$defs"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s schema is not JSON: %v", name, err)
		}
		if doc.Schema == "" || doc.Type != "object" || doc.Extra != false {
			t.Errorf("%s schema header = %+v", name, doc)
		}
		// Every reference points at a definition that exists
		for _, ref := range refsIn(string(raw)) {
			if _, ok := doc.Defs[ref]; !ok {
				t.Errorf("%s schema refers to %q, which it does not define", name, ref)
			}
		}
		again, _ := gen()
		if string(again) != string(raw) {
			t.Errorf("%s schema is not the same twice", name)
		}
		switch name {
		case "template":
			for _, f := range []string{"id", "version", "parts", "features", "credits", "minEngine"} {
				if _, ok := doc.Properties[f]; !ok {
					t.Errorf("template schema has no %q", f)
				}
			}
			if _, ok := doc.Properties["layouts"]; ok {
				t.Error("template schema has layouts, which are files of their own")
			}
		case "layout":
			for _, f := range []string{"serves", "stack", "textBoxes", "art", "symbols", "canvas", "extends", "patch"} {
				if _, ok := doc.Properties[f]; !ok {
					t.Errorf("layout schema has no %q", f)
				}
			}
			if !strings.Contains(string(raw), `"transform_front"`) {
				t.Error("layout schema does not list the roles")
			}
		}
	}
}

// refsIn lists the definition names a schema's $ref values point at
func refsIn(s string) []string {
	var out []string
	const marker = `"$ref": "#/$defs/`
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			return out
		}
		s = s[i+len(marker):]
		out = append(out, s[:strings.Index(s, `"`)])
	}
}

// TestLayoutSchemaOffersEveryRowItemChoice guards the enums a schema-driven
// editor offers against the lists the validator checks, since an enum that
// falls behind rejects a layout the engine accepts
func TestLayoutSchemaOffersEveryRowItemChoice(t *testing.T) {
	raw, err := LayoutSchema()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	item, ok := doc.Defs["RowItem"]
	if !ok {
		t.Fatal("layout schema defines no RowItem")
	}
	if got := item.Properties["anchor"].Enum; !reflect.DeepEqual(got, manifest.RowAnchors()) {
		t.Errorf("anchor enum = %q, want %q", got, manifest.RowAnchors())
	}
	if got := item.Properties["anchor"].Enum; !slices.Contains(got, "first-line") {
		t.Errorf("anchor enum = %q, which leaves out the anchor the validator takes", got)
	}
	if got := item.Properties["for"].Enum; !reflect.DeepEqual(got, append([]string{""}, card.RowFields()...)) {
		t.Errorf("for enum = %q, want the row fields %q and empty", got, card.RowFields())
	}
}
