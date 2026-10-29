// Package template defines what a loaded card template is. A template is read
// from an asset provider, which holds its resolved manifest and its images, and
// compiled once into layouts a render reads without parsing anything. A library
// holds the templates a program has loaded, and adds, replaces, and removes them
// while it runs
package template

import (
	"fmt"
	"image"
	"io"
	"sort"
	"sync"

	"github.com/odevine/mimic/engine/card"
	"github.com/odevine/mimic/engine/fonts"
	"github.com/odevine/mimic/engine/frame"
	"github.com/odevine/mimic/engine/manifest"
)

// ProgressFunc reports render progress: a human-readable step and a fraction in
// [0,1]. It may be called many times per step, and is called from the render
// goroutine, so an implementation that touches UI state marshals it itself
type ProgressFunc func(step string, frac float64)

// RenderRequest is everything a render needs to know about one card, beside the
// template it is drawn with
type RenderRequest struct {
	Card *card.Data
	// Face picks which of Classify(Card)'s images to render, 0 for the front.
	// Only a double-faced card has more than one
	Face int
	Art  image.Image // nil renders without art
	// SetSymbol is the card's set expansion symbol, nil to render without one
	SetSymbol *card.SetSymbol
	// Features holds the value chosen for each of the template's features
	Features frame.Features
	// Progress, when set, receives step updates during the render. Nil disables
	// reporting, which is the common case for a batch or a headless render
	Progress ProgressFunc
	// FontPool is the set of font files the user has made available. Nil
	// offers none, so every role draws with its own font. Like FontOverrides it
	// is caller configuration rather than the card's own data, so it travels on
	// the request instead of in a template
	FontPool *fonts.Pool
	// FontOverrides assigns a font from FontPool to a role, by the role's name
	// and the font's "Family Style" name. A role with no entry, or whose font is
	// not in the pool, draws with its own font
	FontOverrides map[string]string
	// Copyright is the boilerplate line a card's bottom carries. An empty
	// Copyright builds the printed one from the card's own year
	Copyright string
	// DPI is the resolution to render at. It is clamped to the range the
	// template supports, so a preview at a fraction of the authored resolution
	// costs a fraction of the work while laying out identically. Zero renders at
	// the template's own resolution
	DPI int
	// Hide names items of the layout to leave out of the drawing, each as the kind, a
	// colon and the item's key, the way a description names them: "layer:frame" for a stack
	// entry's id, "art:0" for an art window's index, "symbol:set" for a symbol's name and
	// "text:title" for a text box's key. What a section draws is named the same way, as
	// "text:abilities.rows[1]" for a row's text and "layer:abilities.bands[1]" for an image
	// between rows, and "section:abilities" leaves out everything one section draws. A
	// hidden item is only not drawn. The rest of the card is laid out as it would be with
	// the item there, so text still steps around a layer that is hidden and a box still
	// gives way to one, and a description reports the item as it would draw. A name that
	// matches nothing hides nothing
	Hide []string
	// Show names stack entries to draw even where their condition does not hold, as
	// "layer:crown". A forced entry is there for the rest of the card, so text steps around
	// it and a box ducks for it, and a description reports it drawn. It still draws nothing
	// when no image of its part fits the card, or its opacity is 0, and an entry named by
	// Hide as well is not drawn. Only layers can be shown, and any other name is ignored
	Show []string
}

// Hides reports whether the request leaves an item out of the drawing
func (r RenderRequest) Hides(kind, key string) bool { return named(r.Hide, kind, key) }

// Shows reports whether the request draws a stack entry whatever its condition says
func (r RenderRequest) Shows(key string) bool { return named(r.Show, "layer", key) }

// named reports whether a list holds the name of an item, which is its kind, a colon and
// its key
func named(list []string, kind, key string) bool {
	for _, h := range list {
		if len(h) == len(kind)+1+len(key) && h[:len(kind)] == kind && h[len(kind)] == ':' && h[len(kind)+1:] == key {
			return true
		}
	}
	return false
}

// Report forwards a progress update when a callback is set, so a render's call
// sites stay one line and nil-safe
func (r RenderRequest) Report(step string, frac float64) {
	if r.Progress != nil {
		r.Progress(step, frac)
	}
}

// Template is a template that has been read, checked, and compiled. It never
// changes after it is loaded, so any number of renders can use it at once, and a
// render that has started keeps the template it started with when a library
// replaces it
type Template struct {
	m       *manifest.Manifest
	assets  AssetProvider
	fonts   *fonts.Catalog
	layouts map[string]*Layout
	byShape map[frame.Shape]*Layout
	shapes  []frame.Shape
}

// Load reads a template from its provider, checks the manifest, and compiles it.
// The diagnostics are returned whether or not it loaded, so a caller can show the
// warnings of one that did. An error means the template did not load
func Load(p AssetProvider) (*Template, manifest.Diagnostics, error) {
	raw, err := p.ReadManifest()
	if err != nil {
		return nil, nil, err
	}
	m, ds := manifest.Decode(raw)
	if m != nil {
		ds = append(ds, unsupported(m)...)
	}
	if err := ds.Err(); err != nil {
		return nil, ds, err
	}
	cat, err := newCatalog(m, p)
	if err != nil {
		return nil, ds, fmt.Errorf("template: %w", err)
	}
	t := &Template{m: m, assets: p, fonts: cat, layouts: map[string]*Layout{}, byShape: map[frame.Shape]*Layout{}}
	parts := make(map[string]*Part, len(m.Parts))
	for name, mp := range m.Parts {
		slot, err := frame.ParseSlot(mp.Slot)
		if err != nil {
			return nil, ds, fmt.Errorf("template: part %q: %w", name, err)
		}
		part := &Part{Name: name, Slot: slot, Variants: make(map[string]string, len(mp.Variants)), Origin: image.Pt(mp.Origin.X, mp.Origin.Y), Blend: mp.Blend}
		for key, v := range mp.Variants {
			part.Variants[key] = v.Path
		}
		parts[name] = part
	}
	for name, ml := range m.Layouts {
		l, err := compileLayout(name, ml, parts)
		if err != nil {
			return nil, ds, fmt.Errorf("template: %w", err)
		}
		t.layouts[name] = l
		for _, sh := range l.Serves {
			t.byShape[sh] = l
		}
	}
	for sh := range t.byShape {
		t.shapes = append(t.shapes, sh)
	}
	sort.Slice(t.shapes, func(i, j int) bool {
		if t.shapes[i].Role != t.shapes[j].Role {
			return t.shapes[i].Role < t.shapes[j].Role
		}
		return t.shapes[i].Kind < t.shapes[j].Kind
	})
	return t, ds, nil
}

// newCatalog reads and checks the fonts a manifest bundles through its provider
// and binds its roles to them
func newCatalog(m *manifest.Manifest, p AssetProvider) (*fonts.Catalog, error) {
	defs := make(map[string]fonts.FontDef, len(m.Fonts))
	for id, f := range m.Fonts {
		defs[id] = fonts.FontDef{File: f.File, Family: f.Family, Style: f.Style, SHA256: f.SHA256}
	}
	roles := make(map[string]fonts.RoleDef, len(m.Roles))
	for name, r := range m.Roles {
		roles[name] = fonts.RoleDef{Label: r.Label, Font: r.Font, Builtin: r.Builtin}
	}
	return fonts.NewCatalog(defs, roles, p.Open)
}

// unsupported reports what a manifest can say and this engine cannot yet draw, so
// an author is told and a template does not render wrongly without a word
func unsupported(m *manifest.Manifest) manifest.Diagnostics {
	var ds manifest.Diagnostics
	add := func(path, what string) {
		ds = append(ds, manifest.Diagnostic{Severity: manifest.Error, Path: path, Message: what + " is not drawn by this engine yet"})
	}
	for ln, l := range m.Layouts {
		for i, e := range l.Stack {
			if e.Rotate != 0 {
				add(fmt.Sprintf("layouts.%s.stack[%d].rotate", ln, i), "turning an entry")
			}
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].Path < ds[j].Path })
	return ds
}

// ID is the template's id, such as "mimic.m15"
func (t *Template) ID() string { return t.m.ID }

// Manifest is the template's checked manifest, which must not be changed
func (t *Template) Manifest() *manifest.Manifest { return t.m }

// Fonts is what the template says about its text roles and the fonts it bundles,
// which is the standard roles alone for a template that says nothing
func (t *Template) Fonts() *fonts.Catalog { return t.fonts }

// Assets is where the template's images are read from
func (t *Template) Assets() AssetProvider { return t.assets }

// Close releases the template's provider, when it holds anything that needs
// releasing, such as an open bundle. A render that is still using the template
// must finish first
func (t *Template) Close() error {
	if c, ok := t.assets.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Layout returns the layout that draws a shape, and reports false for one the
// template does not serve
func (t *Template) Layout(sh frame.Shape) (*Layout, bool) {
	l, ok := t.byShape[sh]
	return l, ok
}

// Serves reports whether the template draws a shape
func (t *Template) Serves(sh frame.Shape) bool {
	_, ok := t.byShape[sh]
	return ok
}

// Shapes lists every shape the template draws, sorted
func (t *Template) Shapes() []frame.Shape { return append([]frame.Shape(nil), t.shapes...) }

// Primary is the layout a program asks about the template as a whole, such as how
// big a card renders: the one for an ordinary card when there is one, and otherwise
// the first by name
func (t *Template) Primary() *Layout {
	if l, ok := t.byShape[frame.Shape{Role: frame.RoleSingle, Kind: frame.KindStandard}]; ok {
		return l
	}
	names := make([]string, 0, len(t.layouts))
	for n := range t.layouts {
		names = append(names, n)
	}
	sort.Strings(names)
	return t.layouts[names[0]]
}

// Feature is a choice a template offers the user
type Feature struct {
	Name string
	manifest.Feature
}

// Features lists what the template lets the user choose, sorted by name
func (t *Template) Features() []Feature {
	out := make([]Feature, 0, len(t.m.Features))
	for name, f := range t.m.Features {
		out = append(out, Feature{Name: name, Feature: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Info is what a program shows about a template without drawing with it
type Info struct {
	ID          string
	Name        string
	Description string
	Author      manifest.Author
	License     string
	Credits     []manifest.Credit
	Shapes      []frame.Shape
	Features    []Feature
}

// Info describes the template
func (t *Template) Info() Info {
	return Info{
		ID: t.m.ID, Name: t.m.Name, Description: t.m.Description, Author: t.m.Author,
		License: t.m.License, Credits: t.m.Credits, Shapes: t.Shapes(), Features: t.Features(),
	}
}

// UnsupportedError is what a render returns for a face the template has no layout
// for, so a batch can tell a card no template here renders from one that failed
// partway through
type UnsupportedError struct {
	Template string
	Shape    frame.Shape
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("template %q does not render %s faces", e.Template, e.Shape)
}

// Library holds the templates a program has loaded, by id. It is safe for
// concurrent use. Adding a template whose id is held replaces it, and a render
// that has started keeps the template it holds, so a replaced or removed template
// is closed by whoever loaded it once its renders are done
type Library struct {
	mu   sync.RWMutex
	byID map[string]*Template
}

// NewLibrary returns an empty library
func NewLibrary() *Library { return &Library{byID: map[string]*Template{}} }

// Add puts a template in the library, replacing the one with its id, which it
// returns, or nil when there was none
func (l *Library) Add(t *Template) (replaced *Template) {
	l.mu.Lock()
	defer l.mu.Unlock()
	replaced = l.byID[t.ID()]
	l.byID[t.ID()] = t
	return replaced
}

// Remove takes the template with the id out of the library and returns it, or
// returns nil when there is none
func (l *Library) Remove(id string) *Template {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.byID[id]
	delete(l.byID, id)
	return t
}

// Get returns the template with the id
func (l *Library) Get(id string) (*Template, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	t, ok := l.byID[id]
	return t, ok
}

// List returns every template, sorted by id
func (l *Library) List() []*Template {
	l.mu.RLock()
	out := make([]*Template, 0, len(l.byID))
	for _, t := range l.byID {
		out = append(out, t)
	}
	l.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Serving returns the templates that draw a shape, sorted by id
func (l *Library) Serving(sh frame.Shape) []*Template {
	var out []*Template
	for _, t := range l.List() {
		if t.Serves(sh) {
			out = append(out, t)
		}
	}
	return out
}
