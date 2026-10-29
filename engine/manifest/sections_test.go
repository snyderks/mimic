package manifest

import (
	"strings"
	"testing"
)

// abilities is a planeswalker section on the small manifest's parts, which
// each case below breaks one way
func abilities() map[string]any {
	return map[string]any{
		"split": "loyalty",
		"rect":  map[string]any{"x": 400, "y": 2700, "width": 2400, "height": 1200},
		"text":  map[string]any{"fontSize": 80},
		"body":  map[string]any{"x": 600, "width": 2200},
		"gap":   40,
		"items": []any{
			map[string]any{"for": "loyalty", "parts": map[string]any{"plus": "border", "minus": "divider"}, "y": 2900},
			map[string]any{"for": "loyalty", "rect": map[string]any{"x": 400, "y": 2880, "width": 150, "height": 40}, "text": map[string]any{"fontSize": 50}, "format": "{}", "y": 2900},
		},
		"divider":    map[string]any{"part": "divider", "y": 10},
		"bands":      map[string]any{"part": "border", "rows": "even"},
		"header":     map[string]any{"for": "level", "height": 100},
		"avoidLayer": "pt",
		"above":      "pinlines",
		"condition":  "rows>3",
	}
}

func withSection(t *testing.T, change func(s map[string]any)) Diagnostics {
	t.Helper()
	_, ds := Decode(edit(t, func(r map[string]any) {
		set(t, r, "contract", "1.2")
		s := abilities()
		if change != nil {
			change(s)
		}
		set(t, r, "layouts.normal.sections", map[string]any{"abilities": s})
	}))
	return ds
}

func TestASectionDecodes(t *testing.T) {
	m, ds := Decode(edit(t, func(r map[string]any) {
		set(t, r, "contract", "1.2")
		set(t, r, "layouts.normal.sections", map[string]any{"abilities": abilities()})
	}))
	if ds.HasErrors() {
		t.Fatalf("diagnostics = %v", ds)
	}
	s := m.Layouts["normal"].Sections["abilities"]
	if s.Split != "loyalty" || s.Body.X != 600 || len(s.Items) != 2 || s.Items[0].Parts["plus"] != "border" || s.Bands.Rows != "even" {
		t.Errorf("section = %+v", s)
	}
	if _, ok := find(ds, "parts.divider"); ok {
		t.Errorf("a part the section draws is reported unused: %v", ds)
	}
}

func TestEachSectionMistakeIsReportedAtItsPath(t *testing.T) {
	const at = "layouts.normal.sections.abilities"
	cases := []struct {
		name   string
		change func(s map[string]any)
		path   string
		sev    Severity
		msg    string
	}{
		{"an unknown split", func(s map[string]any) { s["split"] = "verses" }, at + ".split", Error, "is not a split"},
		{"no rect", func(s map[string]any) { delete(s, "rect") }, at + ".rect", Error, "is not a size"},
		{"no font size", func(s map[string]any) { s["text"] = map[string]any{} }, at + ".text.fontSize", Error, "not a size above 0"},
		{"an empty column", func(s map[string]any) { s["plain"] = map[string]any{"x": 1, "width": 0} }, at + ".plain", Error, "not on the canvas"},
		{"a negative gap", func(s map[string]any) { s["gap"] = -1 }, at + ".gap", Error, "not a gap"},
		{"a negative share", func(s map[string]any) { s["spread"] = map[string]any{"inside": -1} }, at + ".spread", Error, "negative"},
		{"a header for nothing", func(s map[string]any) { s["header"] = map[string]any{"for": "toughness", "height": 5} }, at + ".header.for", Error, "not a row field"},
		{"a header of no height", func(s map[string]any) { s["header"] = map[string]any{"for": "loyalty"} }, at + ".header.height", Error, "not a height"},
		{"an item for nothing", func(s map[string]any) { item(s)["for"] = "power" }, at + ".items[0].for", Error, "not a row field"},
		{"an item with no part", func(s map[string]any) { item(s)["parts"] = map[string]any{"plus": "nope"} }, at + ".items[0].parts.plus", Error, "no part is named"},
		{"a cost keyed by number", func(s map[string]any) { item(s)["parts"] = map[string]any{"+1": "border"} }, at + ".items[0].parts.+1", Warning, "keyed by its sign"},
		{"an item of both kinds", func(s map[string]any) { item(s)["text"] = map[string]any{"fontSize": 5} }, at + ".items[0]", Error, "names both"},
		{"an item of neither kind", func(s map[string]any) { delete(item(s), "parts") }, at + ".items[0]", Error, "names neither"},
		{"a text item with no rect", func(s map[string]any) { delete(s["items"].([]any)[1].(map[string]any), "rect") }, at + ".items[1]", Error, "needs both"},
		{"a format on an image", func(s map[string]any) { item(s)["format"] = "{}" }, at + ".items[0].format", Warning, "prints no text"},
		{"an unknown anchor", func(s map[string]any) { item(s)["anchor"] = "bottom" }, at + ".items[0].anchor", Error, "not center, top or first-line"},
		{"parts by value without a field", func(s map[string]any) { delete(item(s), "for") }, at + ".items[0].parts", Error, "needs the field"},
		{"a divider with no part", func(s map[string]any) { s["divider"] = map[string]any{"part": "nope"} }, at + ".divider.part", Error, "no part is named"},
		{"bands on every third row", func(s map[string]any) { s["bands"] = map[string]any{"part": "border", "rows": "third"} }, at + ".bands.rows", Error, "not odd or even"},
		{"avoiding no entry", func(s map[string]any) { s["avoidLayer"] = "nope" }, at + ".avoidLayer", Error, "no stack entry"},
		{"above no entry", func(s map[string]any) { s["above"] = "nope" }, at + ".above", Error, "no stack entry"},
		{"a bad condition", func(s map[string]any) { s["condition"] = "rows>many" }, at + ".condition", Error, "compares with a number"},
		{"a bad face", func(s map[string]any) { s["face"] = "third" }, at + ".face", Error, "is not a face"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ds := withSection(t, c.change)
			d, ok := find(ds, c.path)
			if !ok {
				t.Fatalf("nothing at %s in %v", c.path, ds)
			}
			if d.Severity != c.sev || !strings.Contains(d.Message, c.msg) {
				t.Errorf("%s = %v, want a %v containing %q", c.path, d, c.sev, c.msg)
			}
		})
	}
}

func item(s map[string]any) map[string]any { return s["items"].([]any)[0].(map[string]any) }

func TestSectionsNeedContractOnePointTwo(t *testing.T) {
	_, ds := Decode(edit(t, func(r map[string]any) {
		set(t, r, "layouts.normal.sections", map[string]any{"abilities": abilities()})
	}))
	if d, ok := find(ds, "layouts.normal.sections"); !ok || d.Severity != Error || !strings.Contains(d.Message, "need contract 1.2") {
		t.Errorf("diagnostics = %v", ds)
	}
}

func TestArtWithNoAfterSitsBeneathTheStack(t *testing.T) {
	m, ds := Decode(edit(t, func(r map[string]any) {
		set(t, r, "layouts.normal.art[0].after", gone)
	}))
	if ds.HasErrors() {
		t.Fatalf("diagnostics = %v", ds)
	}
	if a := m.Layouts["normal"].Art[0]; a.After != "" {
		t.Errorf("art = %+v", a)
	}
	_, ds = Decode(edit(t, func(r map[string]any) {
		set(t, r, "layouts.normal.art[0].after", "nope")
	}))
	if d, ok := find(ds, "layouts.normal.art[0].after"); !ok || d.Severity != Error {
		t.Errorf("an art window after no entry: %v", ds)
	}
}
