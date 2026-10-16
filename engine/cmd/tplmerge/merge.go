package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/odevine/mimic/engine/author"
	"github.com/odevine/mimic/engine/manifest"
)

// mergeOptions is a merge's flags
type mergeOptions struct {
	out, id, name, version string
	force                  bool
	// strict refuses a part name two sources define differently, instead of
	// renaming it for each source that uses it
	strict bool
}

// mergeSources combines two or more built template source folders into one at
// opt.out, and returns a short summary of what came from where
func mergeSources(srcDirs []string, opt mergeOptions) (string, error) {
	if len(srcDirs) < 2 {
		return "", fmt.Errorf("merge needs at least two source folders")
	}
	if opt.out == "" || opt.id == "" || opt.name == "" {
		return "", fmt.Errorf("merge needs -out, -id, and -name")
	}
	version := opt.version
	if version == "" {
		version = "0.1.0"
	}

	templates := make([]*sourceTemplate, len(srcDirs))
	for i, d := range srcDirs {
		t, err := readSourceTemplate(d)
		if err != nil {
			return "", err
		}
		if len(t.src.Features) > 0 || len(t.src.Fonts) > 0 || len(t.src.Roles) > 0 {
			return "", fmt.Errorf("%s defines fonts, roles, or features, which merge does not combine; add them to the merged template by hand", d)
		}
		templates[i] = t
	}

	// usedParts[i] is the set of part names source i's layouts refer to, so only
	// the parts a layout actually draws are carried into the merge
	usedParts := make([]map[string]bool, len(templates))
	for i, t := range templates {
		used := map[string]bool{}
		for lname, raw := range t.layouts {
			refs, err := partsUsedBy(raw)
			if err != nil {
				return "", fmt.Errorf("%s: layouts/%s.json: %w", t.dir, lname, err)
			}
			for p := range refs {
				used[p] = true
			}
		}
		for _, p := range sortedKeys(used) {
			if _, ok := t.src.Parts[p]; !ok {
				return "", fmt.Errorf("%s: a layout refers to part %q, which is not in %s", t.dir, p, sourceFile)
			}
		}
		usedParts[i] = used
	}

	// A part name, or a layout file name, that two sources define differently
	// either refuses the merge (-strict) or is renamed for every source that
	// uses it, so the name keeps meaning one thing and the merge does not
	// depend on argument order
	collidingParts, err := findCollidingPartNames(templates, usedParts)
	if err != nil {
		return "", err
	}
	collidingLayouts := findCollidingLayoutNames(templates)
	if (len(collidingParts) > 0 || len(collidingLayouts) > 0) && opt.strict {
		return "", collisionError(srcDirs, collidingParts, collidingLayouts)
	}
	slugs := uniqueSlugs(srcDirs)
	partRenameFor := make([]map[string]string, len(templates))
	layoutRenameFor := make([]map[string]string, len(templates))
	var notes []renameNote
	if len(collidingParts) > 0 {
		var partNotes []renameNote
		partRenameFor, partNotes = planRenames(srcDirs, collidingParts, slugs, "part")
		notes = append(notes, partNotes...)
		if err := applyRenames(templates, partRenameFor); err != nil {
			return "", err
		}
	}
	if len(collidingLayouts) > 0 {
		var layoutNotes []renameNote
		layoutRenameFor, layoutNotes = planRenames(srcDirs, collidingLayouts, slugs, "layout")
		notes = append(notes, layoutNotes...)
	}

	mergedParts, partCount, copies, err := mergePartsAndCopies(srcDirs, templates, usedParts, partRenameFor, slugs)
	if err != nil {
		return "", err
	}
	mergedLayouts, layoutCount, err := mergeLayouts(srcDirs, templates, layoutRenameFor)
	if err != nil {
		return "", err
	}

	contract, minEngine := "", ""
	var license []string
	var credits []manifest.Credit
	seenLicense := map[string]bool{}
	seenCredit := map[[2]string]bool{}
	for _, t := range templates {
		contract = highestContract(contract, t.src.Contract)
		minEngine = highestMinEngine(minEngine, t.src.MinEngine)
		if t.src.License != "" && !seenLicense[t.src.License] {
			seenLicense[t.src.License] = true
			license = append(license, t.src.License)
		}
		for _, c := range t.src.Credits {
			key := [2]string{c.Name, c.URL}
			if seenCredit[key] {
				continue
			}
			seenCredit[key] = true
			credits = append(credits, c)
		}
	}

	out := author.Source{
		Contract:  contract,
		ID:        opt.id,
		Version:   version,
		MinEngine: minEngine,
		Name:      opt.name,
		Author:    manifest.Author{Name: "mimic"},
		License:   strings.Join(license, "; "),
		Credits:   credits,
		Parts:     mergedParts,
	}

	if err := prepareOutDir(opt.out, opt.force); err != nil {
		return "", err
	}
	if err := writeSourceTemplate(opt.out, out, mergedLayouts, copies); err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "merged %d sources into %s\n", len(srcDirs), opt.out)
	for i, d := range srcDirs {
		fmt.Fprintf(&b, "  %s: %d parts, %d layouts\n", d, partCount[i], layoutCount[i])
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "  renamed %s %q to %q (from %s)\n", n.kind, n.oldName, n.newName, n.source)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// collisionError is a merge's -strict refusal, naming every colliding part and
// layout and the sources that disagree about it
func collisionError(srcDirs []string, collidingParts, collidingLayouts map[string][]int) error {
	describe := func(kind string, colliding map[string][]int) []string {
		var lines []string
		for _, name := range sortedIntMapKeys(colliding) {
			var dirs []string
			for _, i := range colliding[name] {
				dirs = append(dirs, srcDirs[i])
			}
			lines = append(lines, fmt.Sprintf("%s %q is defined differently by %s", kind, name, strings.Join(dirs, " and ")))
		}
		return lines
	}
	lines := append(describe("part", collidingParts), describe("layout", collidingLayouts)...)
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// applyRenames rewrites every layout of a source whose colliding parts were
// renamed, so its stack, row items, and dividers point at the new names
func applyRenames(templates []*sourceTemplate, renameFor []map[string]string) error {
	for i, t := range templates {
		rename := renameFor[i]
		if len(rename) == 0 {
			continue
		}
		for lname, raw := range t.layouts {
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				return fmt.Errorf("%s: layouts/%s.json: %w", t.dir, lname, err)
			}
			if !renamePartRefs(v, rename) {
				continue
			}
			out, err := json.MarshalIndent(v, "", " ")
			if err != nil {
				return fmt.Errorf("%s: layouts/%s.json: %w", t.dir, lname, err)
			}
			t.layouts[lname] = append(out, '\n')
		}
	}
	return nil
}

// mergePartsAndCopies checks that a part name used by more than one source
// means the same thing everywhere (true once planRenames has moved every
// colliding one out of the way), checks that every file about to be copied
// agrees with every other source that would write to the same destination
// path, and returns the union of the parts actually used, how many each source
// contributed, and the files to copy.
//
// The two checks run as two independent passes over every (source, part)
// pair, deliberately not short circuited by one another: a part name and its
// Dir, Slot, Origin, and Blend agreeing does not prove its images do too, so
// the file check must still see every source's own copy of a part it only
// recognizes by a name that already checked out
func mergePartsAndCopies(srcDirs []string, templates []*sourceTemplate, usedParts []map[string]bool, renameFor []map[string]string, slugs []string) (map[string]author.Part, []int, []copyPlan, error) {
	merged := map[string]author.Part{}
	owner := map[string]int{}
	count := make([]int, len(templates))
	for i, t := range templates {
		for _, name := range sortedKeys(usedParts[i]) {
			outName, outPart := renameOutcome(renameFor, slugs, i, name, t.src.Parts[name])
			if j, ok := owner[outName]; ok {
				if !reflect.DeepEqual(merged[outName], outPart) {
					return nil, nil, nil, fmt.Errorf("part %q is defined differently by %s and %s", outName, srcDirs[j], srcDirs[i])
				}
				continue
			}
			owner[outName] = i
			merged[outName] = outPart
			count[i]++
		}
	}

	type planned struct {
		srcIdx int
		part   string
		sha    string
	}
	destPlan := map[string]planned{}
	var copies []copyPlan
	for i, t := range templates {
		for _, name := range sortedKeys(usedParts[i]) {
			outName, _ := renameOutcome(renameFor, slugs, i, name, t.src.Parts[name])
			plans, err := copyPlansForPart(t, name, t.src.Parts[name])
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", t.dir, err)
			}
			if _, ok := renameFor[i][name]; ok {
				plans = prefixPlans(plans, slugs[i])
			}
			for _, pl := range plans {
				sum, err := sha256File(pl.src)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("%s: reading %s: %w", t.dir, pl.dest, err)
				}
				if prev, ok := destPlan[pl.dest]; ok {
					if prev.sha != sum {
						return nil, nil, nil, fmt.Errorf("%s would be copied from both %s (part %q) and %s (part %q) with different content",
							pl.dest, srcDirs[prev.srcIdx], prev.part, srcDirs[i], outName)
					}
					continue
				}
				destPlan[pl.dest] = planned{srcIdx: i, part: outName, sha: sum}
				copies = append(copies, pl)
			}
		}
	}
	return merged, count, copies, nil
}

// renameOutcome is the name and Part a used part is recorded under: renamed
// and moved under its source's slug when a collision put it in renameFor,
// otherwise unchanged
func renameOutcome(renameFor []map[string]string, slugs []string, i int, name string, p author.Part) (string, author.Part) {
	if nn, ok := renameFor[i][name]; ok {
		return nn, renamedPart(slugs[i], name, p)
	}
	return name, p
}

// mergeLayouts checks that a layout file name used by more than one source,
// once planRenames has moved every colliding one out of the way, is byte for
// byte the same file, and returns the union of layouts along with how many
// each source contributed
func mergeLayouts(srcDirs []string, templates []*sourceTemplate, renameFor []map[string]string) (map[string]json.RawMessage, []int, error) {
	merged := map[string]json.RawMessage{}
	owner := map[string]int{}
	count := make([]int, len(templates))
	for i, t := range templates {
		for _, lname := range sortedKeys(t.layouts) {
			raw := t.layouts[lname]
			outName := lname
			if nn, ok := renameFor[i][lname]; ok {
				outName = nn
			}
			if j, ok := owner[outName]; ok {
				if !jsonEqual(merged[outName], raw) {
					return nil, nil, fmt.Errorf("layout %q is defined differently by %s and %s", outName, srcDirs[j], srcDirs[i])
				}
				continue
			}
			owner[outName] = i
			merged[outName] = raw
			count[i]++
		}
	}
	return merged, count, nil
}

// jsonEqual reports whether two JSON documents decode to the same value,
// ignoring formatting
func jsonEqual(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// writeSourceTemplate writes a template.json, a layouts folder, and every part
// image copyPlan lists, to a prepared out directory
func writeSourceTemplate(out string, src author.Source, layouts map[string]json.RawMessage, copies []copyPlan) error {
	raw, err := json.MarshalIndent(src, "", " ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(out, sourceFile), raw, 0o644); err != nil {
		return err
	}
	layoutsOut := filepath.Join(out, layoutsDir)
	if err := os.MkdirAll(layoutsOut, 0o755); err != nil {
		return err
	}
	for _, name := range sortedKeys(layouts) {
		if err := os.WriteFile(filepath.Join(layoutsOut, name+".json"), layouts[name], 0o644); err != nil {
			return err
		}
	}
	for _, pl := range copies {
		if err := copyFile(pl.src, filepath.Join(out, pl.dest)); err != nil {
			return fmt.Errorf("copying %s: %w", pl.dest, err)
		}
	}
	return nil
}
