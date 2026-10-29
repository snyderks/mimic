package manifest

import (
	"fmt"
	"slices"
	"strings"

	"github.com/odevine/mimic/engine/card"
)

// Limits on a layout's sections
const (
	MaxSections = 16
	MaxRowItems = 32
)

// sections checks a layout's sections against the rest of it
func (v *validator) sections(path string, l *Layout, entries, used map[string]bool) {
	if len(l.Sections) == 0 {
		return
	}
	if !contractAtLeast(v.m.Contract, 2) {
		v.c.errorf(path, "sections need contract 1.2 or newer, and this is %s", v.m.Contract)
	}
	if len(l.Sections) > MaxSections {
		v.c.errorf(path, "there are %d, over the limit of %d", len(l.Sections), MaxSections)
	}
	for _, name := range sortedKeys(l.Sections) {
		v.section(path+"."+name, name, l.Sections[name], entries, used)
	}
}

func (v *validator) section(path, name string, s Section, entries, used map[string]bool) {
	if !boxNameRe.MatchString(name) {
		v.c.errorf(path, "%q is not a section name, which is letters, digits, and underscores", name)
	}
	if !slices.Contains(card.SplitNames(), s.Split) {
		v.c.errorf(path+".split", "%q is not a split, which is %s", s.Split, strings.Join(card.SplitNames(), ", "))
	}
	v.size(path+".rect", s.Rect.X, s.Rect.Y, s.Rect.Width, s.Rect.Height)
	v.textStyle(path+".text", s.Text)
	v.span(path+".body", s.Body)
	v.span(path+".plain", s.Plain)
	if s.Gap < 0 || s.Gap > MaxDimension {
		v.c.errorf(path+".gap", "%d is not a gap from 0 to %d", s.Gap, MaxDimension)
	}
	if sp := s.Spread; sp != nil && (sp.Outside < 0 || sp.Inside < 0) {
		v.c.errorf(path+".spread", "a share cannot be negative")
	}
	if h := s.Header; h != nil {
		v.rowField(path+".header.for", h.For, false)
		if h.Height <= 0 || h.Height > MaxDimension {
			v.c.errorf(path+".header.height", "%d is not a height from 1 to %d", h.Height, MaxDimension)
		}
	}
	if len(s.Items) > MaxRowItems {
		v.c.errorf(path+".items", "there are %d, over the limit of %d", len(s.Items), MaxRowItems)
	}
	for i, it := range s.Items {
		v.rowItem(fmt.Sprintf("%s.items[%d]", path, i), it, used)
	}
	if d := s.Divider; d != nil {
		v.rowPart(path+".divider", *d, used)
	}
	if b := s.Bands; b != nil {
		v.partRef(path+".bands.part", b.Part, used)
		switch b.Rows {
		case "", "odd", "even":
		default:
			v.c.errorf(path+".bands.rows", "%q is not odd or even", b.Rows)
		}
		if b.Top != nil {
			v.rowPart(path+".bands.top", *b.Top, used)
		}
		if b.Bottom != nil {
			v.rowPart(path+".bands.bottom", *b.Bottom, used)
		}
	}
	if s.AvoidLayer != "" && !entries[s.AvoidLayer] {
		v.c.errorf(path+".avoidLayer", "no stack entry has the id %q", s.AvoidLayer)
	}
	if s.Above != "" && !entries[s.Above] {
		v.c.errorf(path+".above", "no stack entry has the id %q", s.Above)
	}
	v.condition(path+".condition", s.Condition)
	v.face(path+".face", s.Face)
}

func (v *validator) span(path string, s *Span) {
	if s == nil {
		return
	}
	if s.Width <= 0 || s.Width > MaxDimension || s.X < -MaxDimension || s.X > MaxDimension {
		v.c.errorf(path, "a column at %d, %d wide, is not on the canvas", s.X, s.Width)
	}
}

// RowAnchors lists where an item beside a row can sit, with empty as the middle
// of the row's text. The schema an editor checks a layout against reads the same
// list, so an anchor the validator takes is one the editor offers
func RowAnchors() []string { return []string{"", "center", "top", "first-line"} }

// rowField checks a row field an item or a header names, which may be empty
// only where empty means every row
func (v *validator) rowField(path, field string, emptyOK bool) {
	if field == "" && emptyOK {
		return
	}
	if !slices.Contains(card.RowFields(), field) {
		v.c.errorf(path, "%q is not a row field, which is %s", field, strings.Join(card.RowFields(), ", "))
	}
}

func (v *validator) rowItem(path string, it RowItem, used map[string]bool) {
	v.rowField(path+".for", it.For, true)
	image := it.Part != "" || len(it.Parts) > 0
	text := it.Text != nil || it.Rect != nil
	switch {
	case image && text:
		v.c.errorf(path, "an item draws an image or a text, and this names both")
	case !image && !text:
		v.c.errorf(path, "an item draws an image or a text, and this names neither")
	case text && (it.Text == nil || it.Rect == nil):
		v.c.errorf(path, "a text item needs both a rect and a text style")
	case text:
		v.size(path+".rect", it.Rect.X, it.Rect.Y, it.Rect.Width, it.Rect.Height)
		v.textStyle(path+".text", *it.Text)
	}
	if it.Part != "" {
		v.partRef(path+".part", it.Part, used)
	}
	if len(it.Parts) > 0 && it.For == "" {
		v.c.errorf(path+".parts", "picking an image by value needs the field the value is of, in for")
	}
	keys := card.ValueKeys(it.For)
	for _, key := range sortedKeys(it.Parts) {
		v.partRef(path+".parts."+key, it.Parts[key], used)
		if it.For == card.FieldLoyalty && !slices.Contains(keys, key) {
			v.c.warnf(path+".parts."+key, "a loyalty cost is keyed by its sign, which is %s, so %q never draws", strings.Join(keys, ", "), key)
		}
	}
	if it.Format != "" && image {
		v.c.warnf(path+".format", "an image item prints no text, so the format does nothing")
	}
	if !slices.Contains(RowAnchors(), it.Anchor) {
		v.c.errorf(path+".anchor", "%q is not center, top or first-line", it.Anchor)
	}
	if it.Step < 0 || it.Step > MaxDimension {
		v.c.errorf(path+".step", "%d is not a step from 0 to %d", it.Step, MaxDimension)
	}
}

func (v *validator) rowPart(path string, p RowPart, used map[string]bool) {
	v.partRef(path+".part", p.Part, used)
	if p.Y < -MaxDimension || p.Y > MaxDimension {
		v.c.errorf(path+".y", "%d is off the canvas", p.Y)
	}
}

// partRef checks that a section names a part the template has, which it then
// counts as used, since a section draws it and not the stack
func (v *validator) partRef(path, name string, used map[string]bool) {
	if _, ok := v.m.Parts[name]; !ok {
		v.c.errorf(path, "no part is named %q", name)
		return
	}
	used[name] = true
}
