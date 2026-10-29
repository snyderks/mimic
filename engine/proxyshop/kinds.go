package proxyshop

import (
	"encoding/json"
	"image"
	"math"
	"strings"

	"github.com/odevine/mimic/engine/psd"
)

// A card kind such as a planeswalker or a saga keeps its frame and text in
// groups of its own, which the usual reading of the root layers does not know.
// Each kind below recognizes its PSD by those groups, claims them before the
// root layers are read, and adds their parts, text boxes and sections once the
// stack is known. A PSD can be of more than one kind, such as a modal double
// faced planeswalker

type kind struct {
	match  func(st *layoutState) bool
	before func(a *analyzer, st *layoutState) error
	after  func(a *analyzer, st *layoutState) error
}

var kinds = []kind{
	{byFile(isPlaneswalker), (*analyzer).planeswalkerBefore, (*analyzer).planeswalkerAfter},
	{isChapters, (*analyzer).chaptersBefore, (*analyzer).chaptersAfter},
	{byFile(isLeveler), nil, (*analyzer).levelerAfter},
	{byFile(isMutate), nil, (*analyzer).mutateAfter},
	{byFile(isPrototype), (*analyzer).prototypeBefore, (*analyzer).prototypeAfter},
	{byFile(isBattle), nil, (*analyzer).battleAfter},
	{byFile(isPlanar), nil, (*analyzer).planarAfter},
	{byFile(isAdventure), (*analyzer).adventureBefore, (*analyzer).adventureAfter},
	{byFile(isToken), (*analyzer).tokenBefore, (*analyzer).tokenAfter},
	{byFile(hasMDFC), (*analyzer).mdfcBefore, (*analyzer).mdfcAfter},
	{byFile(hasRootTransform), (*analyzer).transformBefore, (*analyzer).transformAfter},
}

// byFile matches a kind by the document's layers alone
func byFile(match func(*psd.File) bool) func(*layoutState) bool {
	return func(st *layoutState) bool { return match(st.f) }
}

// kindBefore reads what a card kind's layout needs before the usual reading of
// the root layers, and claims the layers it takes over
func (a *analyzer) kindBefore(st *layoutState) error {
	if st.claimed == nil {
		st.claimed = map[*psd.Layer]bool{}
	}
	for _, k := range kinds {
		if k.before != nil && k.match(st) {
			if err := k.before(a, st); err != nil {
				return err
			}
		}
	}
	return nil
}

// kindAfter adds a card kind's sections and text boxes once the stack is known
func (a *analyzer) kindAfter(st *layoutState) error {
	for _, k := range kinds {
		if k.after != nil && k.match(st) {
			if err := k.after(a, st); err != nil {
				return err
			}
		}
	}
	return nil
}

// rootGroup is the document's root layer of the name when it is a group
func rootGroup(f *psd.File, name string) *psd.Layer {
	if g := f.Root.Find(name); g != nil && g.IsGroup() {
		return g
	}
	return nil
}

// findLayer finds a layer by name anywhere under g, top of the panel first
func findLayer(g *psd.Layer, name string) *psd.Layer {
	if g == nil {
		return nil
	}
	for _, c := range g.Children {
		if c.Name == name {
			return c
		}
	}
	for _, c := range g.Children {
		if c.IsGroup() {
			if l := findLayer(c, name); l != nil {
				return l
			}
		}
	}
	return nil
}

// readGroup reads the frame groups inside g the way the root layers are read,
// each as a part whose name and stack id start with prefix and whose entry
// draws only on cond. It takes the entries out of the stack and returns them,
// so the caller places them where g sits
func (a *analyzer) readGroup(st *layoutState, g *psd.Layer, prefix, cond string) ([]stackEntry, error) {
	start, firstPart := len(st.entries), len(a.recipe.Parts)
	for i := len(g.Children) - 1; i >= 0; i-- {
		if err := a.rootLayer(st, g.Children[i]); err != nil {
			return nil, err
		}
	}
	return a.takeEntries(st, start, firstPart, prefix, cond), nil
}

// takeEntries takes the entries added since start out of the stack, renaming
// them and the parts added since firstPart with prefix and joining cond to
// their conditions
func (a *analyzer) takeEntries(st *layoutState, start, firstPart int, prefix, cond string) []stackEntry {
	renamed := map[string]string{}
	if prefix != "" {
		for i := firstPart; i < len(a.recipe.Parts); i++ {
			np := &a.recipe.Parts[i]
			rest := strings.TrimPrefix(np.Rule.Dir, st.src.Key+"/")
			name := a.partName(st.src.Key, prefix+"_"+rest)
			renamed[np.Name] = name
			np.Name = name
			np.Rule.Dir = st.src.Key + "/" + strings.TrimPrefix(name, st.src.Key+"_")
		}
	}
	out := append([]stackEntry(nil), st.entries[start:]...)
	st.entries = st.entries[:start]
	for i := range out {
		e := &out[i]
		if n, ok := renamed[e.part]; ok {
			e.part = n
		}
		if prefix != "" {
			delete(st.ids, e.id)
			e.id = st.entryID(prefix + "_" + e.id)
		}
		e.condition = joinConditions(e.condition, cond)
	}
	return out
}

// placeEntries puts entries into the stack where the root layer they come
// from sits, above the entries of every root layer below it
func (a *analyzer) placeEntries(st *layoutState, root *psd.Layer, es []stackEntry) {
	pos := rootIndex(st.f, root)
	at := len(st.entries)
	for i, e := range st.entries {
		if rootIndex(st.f, a.entryRoot(st, e)) > pos {
			at = i
			break
		}
	}
	out := append([]stackEntry(nil), st.entries[:at]...)
	out = append(out, es...)
	st.entries = append(out, st.entries[at:]...)
	// The art follows the entry below it, which may now be one placed here
	if st.artSeen && rootIndex(st.f, st.f.Root.Find("Layer 1")) > pos && len(es) > 0 {
		if st.artAfter == "" || at > 0 && st.entries[at-1].id == st.artAfter {
			st.artAfter = es[len(es)-1].id
		}
	}
}

// rootIndex is a root layer's place from the bottom of the panel, or -1
func rootIndex(f *psd.File, l *psd.Layer) int {
	for l != nil && l.Parent != nil && l.Parent != f.Root {
		l = l.Parent
	}
	root := f.Root.Children
	for i := range root {
		if root[i] == l {
			return len(root) - 1 - i
		}
	}
	return -1
}

// entryRoot is the layer a stack entry's part draws
func (a *analyzer) entryRoot(st *layoutState, e stackEntry) *psd.Layer {
	p := a.recipe.Parts.Get(e.part)
	if p == nil || p.Render == nil || len(p.Render.Target) == 0 {
		return nil
	}
	return st.f.Root.Find(p.Render.Target...)
}

// entryIndex is the place of the entry with the id in the stack, or -1
func (st *layoutState) entryIndex(id string) int {
	for i, e := range st.entries {
		if e.id == id {
			return i
		}
	}
	return -1
}

// lastPart is the rule of the part added last
func (a *analyzer) lastPart() *PartRule { return &a.recipe.Parts[len(a.recipe.Parts)-1].Rule }

// ownBoxes reads the layout's text boxes the usual way now, so a kind can
// change them, and takes back the note that the layout has no rules text when
// the kind writes its rules in boxes or sections of its own
func (a *analyzer) ownBoxes(st *layoutState, ownRules bool) error {
	if st.boxes == nil {
		boxes, err := a.textBoxes(st)
		if err != nil {
			return err
		}
		st.boxes = boxes
	}
	if ownRules {
		a.dropRulesNote(st)
	}
	return nil
}

// addBox adds a text box to those the layout reads the usual way
func (st *layoutState) addBox(key string, box Object) {
	if st.boxes != nil {
		st.boxes.Set(key, box)
		return
	}
	st.moreBoxes.Set(key, box)
}

// addSection adds a section to the layout
func (st *layoutState) addSection(name string, s Object) {
	if st.sections == nil {
		st.sections = Object{}
	}
	st.sections.Set(name, s)
}

// paragraph is the box a paragraph type layer wraps its lines in, in document
// pixels, or the layer's pixels for point text
func (t *textLayer) paragraph() image.Rectangle {
	if !t.t.Box {
		return t.l.Rect
	}
	m := t.t.Transform
	b := t.t.BoxBounds
	x0, y0 := m[0]*b[0]+m[2]*b[1]+m[4], m[1]*b[0]+m[3]*b[1]+m[5]
	x1, y1 := m[0]*b[2]+m[2]*b[3]+m[4], m[1]*b[2]+m[3]*b[3]+m[5]
	return image.Rect(int(math.Round(x0)), int(math.Round(y0)), int(math.Round(x1)), int(math.Round(y1)))
}

// leading is the type layer's line spacing as a multiple of its size, or 0 for
// automatic leading
func (t *textLayer) leading() float64 {
	if t.run.AutoLeading || t.run.Leading <= 0 || t.run.FontSize <= 0 {
		return 0
	}
	return math.Round(t.run.Leading/t.run.FontSize*100) / 100
}

// rulesStyle is the style of a rules text layer set in a box: its size, color
// and leading, left aligned
func (t *textLayer) rulesStyle(vAlign string) Object {
	o := obj("fontSize", math.Round(t.size), "color", hexColor(t.run.FillColor), "align", "left", "vAlign", vAlign)
	if l := t.leading(); l > 0 {
		o = append(o, obj("lineSpacing", l)...)
	}
	return o
}

// areaBox is a rules box the way Proxyshop sets a text area: across the type
// layer's own paragraph and down the reference, centered in it
func (t *textLayer) areaBox(ref image.Rectangle, extra ...any) Object {
	p := t.paragraph()
	r := image.Rect(p.Min.X, ref.Min.Y, p.Max.X, ref.Max.Y)
	if ref.Empty() {
		r = p
	}
	if r.Dx() <= 0 {
		r.Min.X, r.Max.X = ref.Min.X, ref.Max.X
	}
	return append(obj("rect", rectObj(r), "text", t.rulesStyle("center"), "flow", Object{}), obj(extra...)...)
}

// pointBox is a box for a point type layer such as a P/T or a level, width
// wide across its origin by its alignment, in its own style
func (a *analyzer) pointBox(st *layoutState, t *textLayer, width int, extra ...any) Object {
	return append(obj("rect", t.baselineBox(width), "text", t.style(a, st.src.Name), "flow", Object{}), obj(extra...)...)
}

// boxRect reads the rect of a text box the layout already has
func boxRect(boxes Object, key string) (map[string]int, bool) {
	raw := boxes.Get(key)
	if raw == nil {
		return nil, false
	}
	var b struct {
		Rect map[string]int `json:"rect"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil, false
	}
	return b.Rect, true
}

// contract raises the template's contract to 1.2 when a layout uses what that
// contract added: sections, or art beneath the whole stack
func (a *analyzer) contract() error {
	need := false
	for _, m := range a.recipe.Layouts {
		var l struct {
			Sections map[string]json.RawMessage   `json:"sections"`
			Art      []map[string]json.RawMessage `json:"art"`
		}
		if err := json.Unmarshal(m.Value, &l); err != nil {
			return err
		}
		if len(l.Sections) > 0 {
			need = true
		}
		for _, w := range l.Art {
			if after, ok := w["after"]; !ok || string(after) == `""` {
				need = true
			}
		}
	}
	if !need {
		return nil
	}
	return a.recipe.Template.Set("contract", "1.2")
}

// sectionStyle is the style a section's rows start from: a rules text layer's
// size, color and leading
func (t *textLayer) sectionStyle() Object {
	o := obj("fontSize", math.Round(t.size), "color", hexColor(t.run.FillColor))
	if l := t.leading(); l > 0 {
		o = append(o, obj("lineSpacing", l)...)
	}
	return o
}
