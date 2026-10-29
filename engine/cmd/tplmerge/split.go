package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/odevine/mimic/engine/author"
)

// splitOptions is a split's flags
type splitOptions struct {
	out, id, name, version string
	force                  bool
}

// splitSource pulls the named layouts, and only the parts they use, out of a
// built template source folder into a new one at opt.out
func splitSource(srcDir string, layoutNames []string, opt splitOptions) (string, error) {
	if len(layoutNames) == 0 {
		return "", fmt.Errorf("split needs at least one layout name")
	}
	if opt.out == "" || opt.id == "" || opt.name == "" {
		return "", fmt.Errorf("split needs -out, -id, and -name")
	}
	version := opt.version
	if version == "" {
		version = "0.1.0"
	}

	t, err := readSourceTemplate(srcDir)
	if err != nil {
		return "", err
	}

	var missing []string
	for _, name := range layoutNames {
		if _, ok := t.layouts[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("%s has no layout named %s (it has: %s)",
			srcDir, strings.Join(missing, ", "), strings.Join(sortedKeys(t.layouts), ", "))
	}

	used := map[string]bool{}
	layouts := map[string]json.RawMessage{}
	for _, name := range layoutNames {
		raw := t.layouts[name]
		layouts[name] = raw
		refs, err := partsUsedBy(raw)
		if err != nil {
			return "", fmt.Errorf("%s: layouts/%s.json: %w", srcDir, name, err)
		}
		for p := range refs {
			used[p] = true
		}
	}

	parts := map[string]author.Part{}
	var copies []copyPlan
	for _, name := range sortedKeys(used) {
		p, ok := t.src.Parts[name]
		if !ok {
			return "", fmt.Errorf("%s: a layout refers to part %q, which is not in %s", srcDir, name, sourceFile)
		}
		parts[name] = p
		plans, err := copyPlansForPart(t, name, p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", srcDir, err)
		}
		copies = append(copies, plans...)
	}

	out := author.Source{
		Contract:    t.src.Contract,
		ID:          opt.id,
		Version:     version,
		MinEngine:   t.src.MinEngine,
		Name:        opt.name,
		Description: t.src.Description,
		Author:      t.src.Author,
		License:     t.src.License,
		Credits:     t.src.Credits,
		Features:    t.src.Features,
		Parts:       parts,
		Fonts:       t.src.Fonts,
		Roles:       t.src.Roles,
	}
	for _, f := range t.src.Fonts {
		copies = append(copies, copyPlan{dest: f.File, src: filepath.Join(srcDir, f.File)})
	}

	if err := prepareOutDir(opt.out, opt.force); err != nil {
		return "", err
	}
	if err := writeSourceTemplate(opt.out, out, layouts, copies); err != nil {
		return "", err
	}

	return fmt.Sprintf("split %s into %s: %d parts, %d layouts", srcDir, opt.out, len(parts), len(layouts)), nil
}
