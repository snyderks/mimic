package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odevine/mimic/engine/author"
)

const (
	sourceFile = "template.json"
	layoutsDir = "layouts"
)

// sourceTemplate is a built template source folder, read from disk
type sourceTemplate struct {
	// dir is the folder's path, for error messages and for copying its files
	dir string
	src author.Source
	// layouts is every file in the layouts folder, by name (without .json), as the
	// bytes on disk, unparsed beyond what readSourceTemplate needs to find them
	layouts map[string]json.RawMessage
}

// readSourceTemplate reads a template's source folder: template.json and every
// file in its layouts folder
func readSourceTemplate(dir string) (*sourceTemplate, error) {
	raw, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	var src author.Source
	if err := decodeStrict(raw, &src); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", dir, sourceFile, err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, layoutsDir))
	if err != nil {
		return nil, fmt.Errorf("%s: reading %s: %w", dir, layoutsDir, err)
	}
	layouts := map[string]json.RawMessage{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok {
			continue
		}
		lraw, err := os.ReadFile(filepath.Join(dir, layoutsDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: reading %s/%s: %w", dir, layoutsDir, e.Name(), err)
		}
		layouts[name] = json.RawMessage(lraw)
	}
	if len(layouts) == 0 {
		return nil, fmt.Errorf("%s: %s holds no layout", dir, layoutsDir)
	}
	return &sourceTemplate{dir: dir, src: src, layouts: layouts}, nil
}

// partsUsedBy is the set of part names a layout file refers to, found by walking
// its decoded JSON for every "part", "parts", and string valued "divider" field,
// wherever it occurs. This reaches a stack entry's part, a row item's part or
// parts, a row part's part, a band's top or bottom part, and a text box's flow
// divider, without needing to know a layout's full shape
func partsUsedBy(raw json.RawMessage) (map[string]bool, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	found := map[string]bool{}
	collectPartRefs(v, found)
	return found, nil
}

func collectPartRefs(v any, found map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			switch k {
			case "part", "divider":
				// "divider" is a plain part name on a text box's flow, and an object
				// (a RowPart, with its own "part" field) on a section, so only a
				// string value under it is a part reference here
				if s, ok := vv.(string); ok && s != "" {
					found[s] = true
				}
			case "parts":
				if m, ok := vv.(map[string]any); ok {
					for _, pv := range m {
						if s, ok := pv.(string); ok && s != "" {
							found[s] = true
						}
					}
				}
			}
			collectPartRefs(vv, found)
		}
	case []any:
		for _, e := range val {
			collectPartRefs(e, found)
		}
	}
}

// effectiveDir is the folder a part's images live in, the same default
// expandPart in the author package uses: the part's own name when Dir is empty
func effectiveDir(name string, p author.Part) string {
	if p.Dir != "" {
		return p.Dir
	}
	return name
}

// copyPlan is one file a part needs copied, from its source path to the same
// relative destination path in every merged or split template
type copyPlan struct {
	// dest is the path relative to the template folder, slash separated
	dest string
	// src is the absolute path to read it from
	src string
}

// copyPlansForPart lists every file a part's images live in: the files directly in
// its folder, wholesale, and any of its explicit variant overrides
func copyPlansForPart(t *sourceTemplate, name string, p author.Part) ([]copyPlan, error) {
	var plans []copyPlan
	dir := effectiveDir(name, p)
	entries, err := os.ReadDir(filepath.Join(t.dir, dir))
	switch {
	case err == nil:
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			rel := dir + "/" + e.Name()
			plans = append(plans, copyPlan{dest: rel, src: filepath.Join(t.dir, rel)})
		}
	case os.IsNotExist(err) && len(p.Variants) > 0:
		// the part draws only from explicit variant overrides
	default:
		return nil, fmt.Errorf("part %q's folder %q cannot be read: %w", name, dir, err)
	}
	for _, key := range sortedKeys(p.Variants) {
		rel := p.Variants[key]
		plans = append(plans, copyPlan{dest: rel, src: filepath.Join(t.dir, rel)})
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("part %q has no images", name)
	}
	return plans, nil
}
