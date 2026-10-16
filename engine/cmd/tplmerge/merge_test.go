package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/author"
	"github.com/odevine/mimic/engine/manifest"
)

// testPart describes one part of a synthetic template, for buildTemplate
type testPart struct {
	name    string
	dir     string
	variant string
	content byte
}

// buildTemplate writes a minimal but well formed template source folder: a
// template.json and a layouts folder, with one tiny PNG per part. layouts maps a
// layout file's name to the part names its stack uses
func buildTemplate(t *testing.T, root string, id string, parts []testPart, layouts map[string][]string) {
	t.Helper()
	src := author.Source{
		Contract: "1.2",
		ID:       id,
		Version:  "0.1.0",
		Name:     "template " + id,
		Author:   manifest.Author{Name: "mimic"},
		Parts:    map[string]author.Part{},
	}
	for _, p := range parts {
		src.Parts[p.name] = author.Part{Dir: p.dir}
		imgDir := filepath.Join(root, p.dir)
		if err := os.MkdirAll(imgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		variant := p.variant
		if variant == "" {
			variant = "any"
		}
		writeTestPNG(t, filepath.Join(imgDir, variant+".png"), p.content)
	}
	raw, err := json.MarshalIndent(src, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sourceFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	layoutsDirPath := filepath.Join(root, layoutsDir)
	if err := os.MkdirAll(layoutsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, used := range layouts {
		var stack []map[string]string
		for _, p := range used {
			stack = append(stack, map[string]string{"id": p, "part": p})
		}
		doc := map[string]any{
			"serves":    []map[string]string{{"role": "single", "kind": "standard"}},
			"canvas":    map[string]int{"width": 10, "height": 10},
			"stack":     stack,
			"textBoxes": map[string]any{},
		}
		raw, err := json.MarshalIndent(doc, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layoutsDirPath, name+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeTestPNG writes a tiny opaque PNG, shaded by content, so author.Validate's
// check for a visible pixel passes
func writeTestPNG(t *testing.T, path string, content byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	c := color.NRGBA{content, content, content, 255}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeLayout writes a layout file by hand, for a test that needs more than
// buildTemplate's plain stack of parts, such as a flow divider
func writeLayout(t *testing.T, root, name string, doc map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, layoutsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, layoutsDir, name+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMergedSource(t *testing.T, dir string) author.Source {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if err != nil {
		t.Fatal(err)
	}
	var s author.Source
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMergeClean(t *testing.T) {
	base := t.TempDir()
	srcA := filepath.Join(base, "a")
	srcB := filepath.Join(base, "b")
	out := filepath.Join(base, "out")

	buildTemplate(t, srcA, "tpl.a", []testPart{{name: "a_x", dir: "a/x", content: 1}}, map[string][]string{"one": {"a_x"}})
	buildTemplate(t, srcB, "tpl.b", []testPart{{name: "b_y", dir: "b/y", content: 2}}, map[string][]string{"two": {"b_y"}})

	summary, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged"})
	if err != nil {
		t.Fatalf("mergeSources: %v", err)
	}
	if summary == "" {
		t.Error("mergeSources returned an empty summary")
	}

	s := readMergedSource(t, out)
	if s.ID != "tpl.merged" || s.Name != "Merged" {
		t.Errorf("id/name = %q/%q, want tpl.merged/Merged", s.ID, s.Name)
	}
	if s.Version != "0.1.0" {
		t.Errorf("version = %q, want 0.1.0", s.Version)
	}
	if len(s.Parts) != 2 {
		t.Errorf("parts = %v, want exactly a_x and b_y", s.Parts)
	}
	if _, ok := s.Parts["a_x"]; !ok {
		t.Error("merged template is missing part a_x")
	}
	if _, ok := s.Parts["b_y"]; !ok {
		t.Error("merged template is missing part b_y")
	}

	for _, p := range []string{filepath.Join(out, "layouts", "one.json"), filepath.Join(out, "layouts", "two.json"),
		filepath.Join(out, "a", "x", "any.png"), filepath.Join(out, "b", "y", "any.png")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestMergeCollisionStrictRefused(t *testing.T) {
	base := t.TempDir()
	srcA := filepath.Join(base, "a")
	srcB := filepath.Join(base, "b")
	out := filepath.Join(base, "out")

	// Both sources define a part named "shared", pointing at different folders,
	// so the name means two different things and -strict must refuse the merge
	buildTemplate(t, srcA, "tpl.a", []testPart{{name: "shared", dir: "a/shared", content: 1}}, map[string][]string{"one": {"shared"}})
	buildTemplate(t, srcB, "tpl.b", []testPart{{name: "shared", dir: "b/shared", content: 2}}, map[string][]string{"two": {"shared"}})

	_, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged", strict: true})
	if err == nil {
		t.Fatal("mergeSources: want an error for a colliding part under -strict, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "shared") {
		t.Errorf("error %q does not name the colliding part", got)
	}
	// nothing should have been written
	if _, statErr := os.Stat(filepath.Join(out, sourceFile)); statErr == nil {
		t.Error("a refused merge should not write template.json")
	}
}

// TestMergeSameNameSameMetadataDifferentContent covers a collision a part
// name and its Dir, Slot, Origin, and Blend agreeing cannot prove away: two
// sources naming a part identically, with identical metadata, whose actual
// images differ. Nothing in the part's own definition distinguishes it, so
// finding this collision needs comparing the files themselves and not only
// the part's declared fields; by default it is auto-renamed like any other
// collision, and -strict still refuses it
func TestMergeSameNameSameMetadataDifferentContent(t *testing.T) {
	base := t.TempDir()
	srcA := filepath.Join(base, "a")
	srcB := filepath.Join(base, "b")

	buildTemplate(t, srcA, "tpl.a", []testPart{{name: "shared", dir: "shared", content: 1}}, map[string][]string{"one": {"shared"}})
	buildTemplate(t, srcB, "tpl.b", []testPart{{name: "shared", dir: "shared", content: 2}}, map[string][]string{"two": {"shared"}})

	t.Run("strict refuses", func(t *testing.T) {
		out := filepath.Join(base, "out-strict")
		_, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged", strict: true})
		if err == nil {
			t.Fatal("mergeSources: want an error for same-name same-metadata different-content parts under -strict, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "shared") {
			t.Errorf("error %q does not name the colliding part", got)
		}
	})

	t.Run("default auto-renames", func(t *testing.T) {
		out := filepath.Join(base, "out-default")
		summary, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged"})
		if err != nil {
			t.Fatalf("mergeSources: %v", err)
		}
		if !strings.Contains(summary, "renamed part") {
			t.Errorf("summary %q does not mention a rename", summary)
		}
		s := readMergedSource(t, out)
		if _, ok := s.Parts["shared"]; ok {
			t.Error("merged template should not keep the colliding name \"shared\"")
		}
		if len(s.Parts) != 2 {
			t.Errorf("parts = %v, want exactly two renamed copies of shared", s.Parts)
		}
	})
}

func TestMergeCollisionAutoRenamed(t *testing.T) {
	base := t.TempDir()
	srcA := filepath.Join(base, "a")
	srcB := filepath.Join(base, "b")
	out := filepath.Join(base, "out")

	// Both sources define a part named "shared", pointing at different folders
	// with different images, so by default (no -strict) the merge must rename
	// it for each of them rather than refuse
	buildTemplate(t, srcA, "tpl.a", []testPart{{name: "shared", dir: "a/shared", content: 1}}, map[string][]string{"one": {"shared"}})
	buildTemplate(t, srcB, "tpl.b", []testPart{{name: "shared", dir: "b/shared", content: 2}}, map[string][]string{"two": {"shared"}})

	summary, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged"})
	if err != nil {
		t.Fatalf("mergeSources: %v", err)
	}
	if !strings.Contains(summary, `renamed part "shared" to "a_shared" (from `+srcA+")") {
		t.Errorf("summary %q does not mention renaming shared to a_shared", summary)
	}
	if !strings.Contains(summary, `renamed part "shared" to "b_shared" (from `+srcB+")") {
		t.Errorf("summary %q does not mention renaming shared to b_shared", summary)
	}

	s := readMergedSource(t, out)
	if _, ok := s.Parts["shared"]; ok {
		t.Error("merged template should not keep the colliding name \"shared\"")
	}
	pa, ok := s.Parts["a_shared"]
	if !ok {
		t.Fatal("merged template is missing the renamed part a_shared")
	}
	if pa.Dir != "a/a/shared" {
		t.Errorf("a_shared.dir = %q, want a/a/shared", pa.Dir)
	}
	pb, ok := s.Parts["b_shared"]
	if !ok {
		t.Fatal("merged template is missing the renamed part b_shared")
	}
	if pb.Dir != "b/b/shared" {
		t.Errorf("b_shared.dir = %q, want b/b/shared", pb.Dir)
	}

	for _, p := range []string{filepath.Join(out, "a", "a", "shared", "any.png"), filepath.Join(out, "b", "b", "shared", "any.png")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(out, "layouts", "one.json"))
	if err != nil {
		t.Fatal(err)
	}
	var layoutOne struct {
		Stack []struct {
			Part string `json:"part"`
		} `json:"stack"`
	}
	if err := json.Unmarshal(raw, &layoutOne); err != nil {
		t.Fatal(err)
	}
	if len(layoutOne.Stack) != 1 || layoutOne.Stack[0].Part != "a_shared" {
		t.Errorf("layouts/one.json stack = %+v, want a single entry naming a_shared", layoutOne.Stack)
	}
}

// TestMergeCollisionRenamedResolves checks that after a rename, both the
// renamed part's own layout (by a plain stack entry) and another source's
// layout (by a text box's flow divider, the odd field that is a bare string
// rather than an object) still resolve, by validating the merged template the
// way a person would
func TestMergeCollisionRenamedResolves(t *testing.T) {
	base := t.TempDir()
	srcA := filepath.Join(base, "a")
	srcB := filepath.Join(base, "b")
	out := filepath.Join(base, "out")

	buildTemplate(t, srcA, "tpl.a", []testPart{{name: "shared_part", dir: "a/shared", content: 1}}, nil)
	buildTemplate(t, srcB, "tpl.b", []testPart{{name: "shared_part", dir: "b/shared", content: 2}}, nil)

	writeLayout(t, srcA, "one", map[string]any{
		"serves":    []map[string]string{{"role": "single", "kind": "standard"}},
		"canvas":    map[string]int{"width": 10, "height": 10},
		"stack":     []map[string]string{{"id": "shared_part", "part": "shared_part"}},
		"textBoxes": map[string]any{},
	})
	writeLayout(t, srcB, "two", map[string]any{
		"serves": []map[string]string{{"role": "single", "kind": "saga"}},
		"canvas": map[string]int{"width": 10, "height": 10},
		"stack":  []map[string]string{{"id": "shared_part", "part": "shared_part"}},
		"textBoxes": map[string]any{
			"oracle": map[string]any{
				"rect": map[string]int{"x": 0, "y": 0, "width": 10, "height": 10},
				"text": map[string]any{"fontSize": 5},
				"flow": map[string]any{"divider": "shared_part"},
			},
		},
	})

	summary, err := mergeSources([]string{srcA, srcB}, mergeOptions{out: out, id: "tpl.merged", name: "Merged"})
	if err != nil {
		t.Fatalf("mergeSources: %v", err)
	}
	if !strings.Contains(summary, "renamed part") {
		t.Errorf("summary %q does not mention a rename", summary)
	}

	ds := author.Validate(os.DirFS(out), author.Options{})
	for _, d := range ds {
		t.Log(d)
	}
	if ds.HasErrors() {
		t.Errorf("the merged template does not validate: %v", ds)
	}
}
