package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitClean(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	out := filepath.Join(base, "out")

	buildTemplate(t, src, "tpl.src",
		[]testPart{{name: "a_x", dir: "a/x", content: 1}, {name: "b_y", dir: "b/y", content: 2}},
		map[string][]string{"one": {"a_x"}, "two": {"b_y"}})

	summary, err := splitSource(src, []string{"one"}, splitOptions{out: out, id: "tpl.one", name: "One"})
	if err != nil {
		t.Fatalf("splitSource: %v", err)
	}
	if summary == "" {
		t.Error("splitSource returned an empty summary")
	}

	s := readMergedSource(t, out)
	if s.ID != "tpl.one" || s.Name != "One" {
		t.Errorf("id/name = %q/%q, want tpl.one/One", s.ID, s.Name)
	}
	if len(s.Parts) != 1 {
		t.Errorf("parts = %v, want only a_x", s.Parts)
	}
	if _, ok := s.Parts["a_x"]; !ok {
		t.Error("split template is missing part a_x")
	}
	if _, ok := s.Parts["b_y"]; ok {
		t.Error("split template should not carry the unused part b_y")
	}

	if _, err := os.Stat(filepath.Join(out, "layouts", "one.json")); err != nil {
		t.Errorf("expected layouts/one.json to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "layouts", "two.json")); !os.IsNotExist(err) {
		t.Errorf("layouts/two.json should not have been pulled out, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "a", "x", "any.png")); err != nil {
		t.Errorf("expected a/x/any.png to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "b", "y")); !os.IsNotExist(err) {
		t.Errorf("b/y should not have been copied, stat err = %v", err)
	}
}

func TestSplitUnknownLayoutRefused(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	out := filepath.Join(base, "out")

	buildTemplate(t, src, "tpl.src", []testPart{{name: "a_x", dir: "a/x", content: 1}}, map[string][]string{"one": {"a_x"}})

	_, err := splitSource(src, []string{"one", "ghost"}, splitOptions{out: out, id: "tpl.one", name: "One"})
	if err == nil {
		t.Fatal("splitSource: want an error for a layout the source does not have, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "ghost") {
		t.Errorf("error %q does not name the missing layout", got)
	}
	if _, statErr := os.Stat(filepath.Join(out, sourceFile)); statErr == nil {
		t.Error("a refused split should not write template.json")
	}
}
