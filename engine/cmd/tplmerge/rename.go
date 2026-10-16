package main

import (
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/odevine/mimic/engine/author"
)

// findCollidingPartNames is every part name used by more than one source whose
// definitions do not all agree, by the sources that use it. A name used by
// more than one source where every one of them defines it identically, images
// included, is not a collision: it is the same part, shared on purpose.
// Agreeing Dir, Slot, Origin, and Blend is not enough on its own: two sources
// can name a part identically, with identical metadata, and still hold
// different images under it, so every pair is also checked file by file
func findCollidingPartNames(templates []*sourceTemplate, usedParts []map[string]bool) (map[string][]int, error) {
	users := map[string][]int{}
	for i, used := range usedParts {
		for name := range used {
			users[name] = append(users[name], i)
		}
	}
	colliding := map[string][]int{}
	for name, idxs := range users {
		if len(idxs) < 2 {
			continue
		}
		firstIdx := idxs[0]
		for _, i := range idxs[1:] {
			same, err := partsIdentical(templates[firstIdx], name, templates[i], name)
			if err != nil {
				return nil, err
			}
			if !same {
				colliding[name] = idxs
				break
			}
		}
	}
	return colliding, nil
}

// partsIdentical reports whether two sources' parts, named na in ta and nb in
// tb, agree on both metadata and the actual bytes of every image they hold
func partsIdentical(ta *sourceTemplate, na string, tb *sourceTemplate, nb string) (bool, error) {
	pa, pb := ta.src.Parts[na], tb.src.Parts[nb]
	if !reflect.DeepEqual(pa, pb) {
		return false, nil
	}
	plansA, err := copyPlansForPart(ta, na, pa)
	if err != nil {
		return false, err
	}
	plansB, err := copyPlansForPart(tb, nb, pb)
	if err != nil {
		return false, err
	}
	if len(plansA) != len(plansB) {
		return false, nil
	}
	sort.Slice(plansA, func(i, j int) bool { return plansA[i].dest < plansA[j].dest })
	sort.Slice(plansB, func(i, j int) bool { return plansB[i].dest < plansB[j].dest })
	for i := range plansA {
		if plansA[i].dest != plansB[i].dest {
			return false, nil
		}
		ha, err := sha256File(plansA[i].src)
		if err != nil {
			return false, err
		}
		hb, err := sha256File(plansB[i].src)
		if err != nil {
			return false, err
		}
		if ha != hb {
			return false, nil
		}
	}
	return true, nil
}

// findCollidingLayoutNames is every layout file name used by more than one
// source whose files do not all agree, by the sources that use it, the same
// rule findCollidingPartNames applies to parts
func findCollidingLayoutNames(templates []*sourceTemplate) map[string][]int {
	users := map[string][]int{}
	for i, t := range templates {
		for lname := range t.layouts {
			users[lname] = append(users[lname], i)
		}
	}
	colliding := map[string][]int{}
	for lname, idxs := range users {
		if len(idxs) < 2 {
			continue
		}
		first := templates[idxs[0]].layouts[lname]
		for _, i := range idxs[1:] {
			if !jsonEqual(first, templates[i].layouts[lname]) {
				colliding[lname] = idxs
				break
			}
		}
	}
	return colliding
}

// renameNote records one collision a merge moved to a new name, for the
// summary. kind is "part" or "layout"
type renameNote struct {
	kind, oldName, newName, source string
}

// planRenames assigns every source using a colliding name (a part or a layout
// file, named by kind) a new, source specific name for it, so a name that
// means two different things keeps both, rather than one silently winning.
// Every source that uses the name is renamed, not only the second one in, so
// the result does not depend on argument order
func planRenames(srcDirs []string, colliding map[string][]int, slugs []string, kind string) (renameFor []map[string]string, notes []renameNote) {
	renameFor = make([]map[string]string, len(srcDirs))
	for _, name := range sortedIntMapKeys(colliding) {
		for _, i := range colliding[name] {
			if renameFor[i] == nil {
				renameFor[i] = map[string]string{}
			}
			newName := slugs[i] + "_" + name
			renameFor[i][name] = newName
			notes = append(notes, renameNote{kind: kind, oldName: name, newName: newName, source: srcDirs[i]})
		}
	}
	return renameFor, notes
}

// renamedPart is p with its folder, and any explicit variant path, moved under a
// slug so its images no longer share a destination with another source's part of
// the same original name
func renamedPart(slug, name string, p author.Part) author.Part {
	out := p
	out.Dir = path.Join(slug, effectiveDir(name, p))
	if len(p.Variants) > 0 {
		variants := make(map[string]string, len(p.Variants))
		for k, v := range p.Variants {
			variants[k] = path.Join(slug, v)
		}
		out.Variants = variants
	}
	return out
}

// prefixPlans moves every copy plan's destination under slug, to match a part
// that renamedPart moved
func prefixPlans(plans []copyPlan, slug string) []copyPlan {
	out := make([]copyPlan, len(plans))
	for i, pl := range plans {
		out[i] = copyPlan{dest: path.Join(slug, pl.dest), src: pl.src}
	}
	return out
}

// renamePartRefs rewrites every part reference a layout's decoded JSON holds,
// through the same "part", "parts", and "divider" fields partsUsedBy reads, and
// reports whether it changed anything
func renamePartRefs(v any, rename map[string]string) bool {
	changed := false
	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			switch k {
			case "part", "divider":
				if s, ok := vv.(string); ok {
					if nn, ok := rename[s]; ok && nn != s {
						val[k] = nn
						changed = true
					}
				}
			case "parts":
				if m, ok := vv.(map[string]any); ok {
					for mk, pv := range m {
						if s, ok := pv.(string); ok {
							if nn, ok := rename[s]; ok && nn != s {
								m[mk] = nn
								changed = true
							}
						}
					}
				}
			}
			if renamePartRefs(vv, rename) {
				changed = true
			}
		}
	case []any:
		for _, e := range val {
			if renamePartRefs(e, rename) {
				changed = true
			}
		}
	}
	return changed
}

// uniqueSlugs gives every source folder a short, stable, and distinct name to
// namespace a renamed part by, derived from the folder's own base name
func uniqueSlugs(dirs []string) []string {
	out := make([]string, len(dirs))
	seen := map[string]int{}
	for i, d := range dirs {
		base := slugify(filepath.Base(d))
		slug := base
		for {
			n := seen[slug]
			seen[slug] = n + 1
			if n == 0 {
				break
			}
			slug = base + "-" + strconv.Itoa(n+1)
		}
		out[i] = slug
	}
	return out
}

// slugify turns a folder's base name into lowercase letters, digits,
// underscores, and hyphens, the characters a part name may hold, so it can
// prefix one
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		out = "src"
	}
	return out
}

func sortedIntMapKeys(m map[string][]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
