package proxyshop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"regexp"
	"sort"
	"strings"
)

// RecipeFormat is the version of the recipe format this package reads
const RecipeFormat = 1

// Recipe says how to cut a mimic template from one or more Proxyshop PSDs. It
// is plain data, so a recipe that Analyze writes can be checked in, read, and
// edited by hand before Build turns it into a template's source folder
type Recipe struct {
	Format int `json:"format"`
	// Sources names each PSD the recipe reads, by file name inside the folder
	// the PSDs are kept in
	Sources map[string]string `json:"sources"`
	// Template holds the fields of the template's template.json other than its
	// parts, in the order they are written
	Template Object `json:"template"`
	// Parts are the template's parts in the order template.json lists them
	Parts Parts `json:"parts"`
	// Layouts are the layout files written to layouts/, each as written
	Layouts Object `json:"layouts"`
}

// Parts keeps a recipe's parts in order
type Parts []NamedPart

// NamedPart is one part rule and the part name it writes
type NamedPart struct {
	Name string
	Rule PartRule
}

// UnmarshalJSON reads the parts object in order
func (p *Parts) UnmarshalJSON(data []byte) error {
	var o Object
	if err := o.UnmarshalJSON(data); err != nil {
		return err
	}
	*p = (*p)[:0]
	for _, m := range o {
		var r PartRule
		dec := json.NewDecoder(bytes.NewReader(m.Value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			return fmt.Errorf("part %q: %w", m.Key, err)
		}
		*p = append(*p, NamedPart{Name: m.Key, Rule: r})
	}
	return nil
}

// MarshalJSON writes the parts object in order
func (p Parts) MarshalJSON() ([]byte, error) {
	o := Object{}
	for _, np := range p {
		if err := o.Set(np.Name, np.Rule); err != nil {
			return nil, err
		}
	}
	return o.MarshalJSON()
}

// Get returns the rule for the part name, or nil
func (p Parts) Get(name string) *PartRule {
	for i := range p {
		if p[i].Name == name {
			return &p[i].Rule
		}
	}
	return nil
}

// Rect is a rectangle of the canvas, its far edges exclusive
type Rect struct {
	X0 int `json:"x0"`
	Y0 int `json:"y0"`
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
}

func (r Rect) image() image.Rectangle { return image.Rect(r.X0, r.Y0, r.X1, r.Y1) }

func rectOf(r image.Rectangle) *Rect { return &Rect{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y} }

// PartRule makes one part: a folder of same sized images, one per variant key
type PartRule struct {
	// Source names the entry of Recipe.Sources the part is cut from
	Source string `json:"source"`
	// Dir is the folder the images are written to, inside the template
	Dir string `json:"dir"`
	// Slot, Origin and Blend are written to the part in template.json. With
	// Crop set and no Origin, the origin is the crop's corner
	Slot   string          `json:"slot,omitempty"`
	Origin json.RawMessage `json:"origin,omitempty"`
	Blend  json.RawMessage `json:"blend,omitempty"`
	// Crop is the region of the canvas the images cover, the whole canvas when
	// it is not set
	Crop *Rect `json:"crop,omitempty"`
	// Group is the path of group names to the layers the variants come from,
	// the document root when it is empty
	Group []string `json:"group,omitempty"`
	// Layers maps a variant key to the name of its layer in the group
	Layers Object `json:"layers,omitempty"`
	// All makes a variant of every layer in the group that is not a group
	// itself, keyed by its name in lower case with color letters in WUBRG
	// order, so "RW" is "wr"
	All bool `json:"all,omitempty"`
	// Omit leaves out these keys, such as the color pairs a template blends
	// from two single color images instead
	Omit []string `json:"omit,omitempty"`
	// Clip scales a layer clipped to the one below it by that layer's alpha
	Clip bool `json:"clip,omitempty"`
	// Tint makes the variants by coloring shape layers, in place of Layers
	Tint *Tint `json:"tint,omitempty"`
	// Bake shades every variant with measured layer styles
	Bake *Bake `json:"bake,omitempty"`
	// Render draws each variant through the compositor, which follows the
	// PSD's own blend modes, masks, clipping and layer styles
	Render *Render `json:"render,omitempty"`
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)

// Load reads a recipe file
func Load(path string) (*Recipe, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse reads a recipe and checks it
func Parse(raw []byte) (*Recipe, error) {
	var r Recipe
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("reading the recipe: %w", err)
	}
	if err := r.Check(); err != nil {
		return nil, err
	}
	return &r, nil
}

// Marshal writes the recipe in the layout this package writes JSON in
func (r *Recipe) Marshal() ([]byte, error) {
	raw, err := marshalRaw(r)
	if err != nil {
		return nil, err
	}
	out, err := Indent(raw)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Check reports the first problem that would stop Build
func (r *Recipe) Check() error {
	if r.Format != RecipeFormat {
		return fmt.Errorf("the recipe is format %d, and this version reads format %d", r.Format, RecipeFormat)
	}
	if len(r.Sources) == 0 {
		return fmt.Errorf("the recipe names no source PSD")
	}
	if r.Template.Has("parts") {
		return fmt.Errorf("the recipe's template lists parts, which belong under parts")
	}
	seen := map[string]bool{}
	dirs := map[string]string{}
	for _, np := range r.Parts {
		at := "part " + np.Name
		if seen[np.Name] {
			return fmt.Errorf("%s is listed twice", at)
		}
		seen[np.Name] = true
		p := np.Rule
		if _, ok := r.Sources[p.Source]; !ok {
			return fmt.Errorf("%s names source %q, which the recipe does not list", at, p.Source)
		}
		if p.Dir == "" || strings.Contains(p.Dir, "..") || strings.HasPrefix(p.Dir, "/") {
			return fmt.Errorf("%s needs a folder inside the template, not %q", at, p.Dir)
		}
		if other, ok := dirs[p.Dir]; ok {
			return fmt.Errorf("%s writes to %s, as %s does", at, p.Dir, other)
		}
		dirs[p.Dir] = np.Name
		ways := 0
		for _, set := range []bool{len(p.Layers) > 0, p.All, p.Tint != nil, p.Render != nil} {
			if set {
				ways++
			}
		}
		if ways != 1 {
			return fmt.Errorf("%s must name its variants one way: layers, all, tint, or render", at)
		}
		var keys []string
		for _, m := range p.Layers {
			keys = append(keys, m.Key)
		}
		if p.Render != nil {
			for _, m := range p.Render.Variants {
				keys = append(keys, m.Key)
			}
		}
		for _, k := range keys {
			if !keyPattern.MatchString(k) {
				return fmt.Errorf("%s has variant key %q, which is not lower case letters, digits and underscores", at, k)
			}
		}
		if p.Crop != nil && p.Crop.image().Empty() {
			return fmt.Errorf("%s has an empty crop", at)
		}
		if p.Bake != nil && p.Render != nil {
			return fmt.Errorf("%s both bakes measured shading and renders the PSD's styles, which draw the same thing", at)
		}
	}
	return nil
}

// sourceNames lists the sources the parts read, sorted
func (r *Recipe) sourceNames() []string {
	names := make([]string, 0, len(r.Sources))
	for n := range r.Sources {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// wubrg is the color order Magic writes colors in
const wubrg = "wubrg"

// NormalizeKey turns a layer name into a variant key: lower case, spaces as
// underscores, and a name made only of color letters put in WUBRG order
func NormalizeKey(name string) string {
	k := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
	if isColorCombo(k) {
		b := []byte(k)
		sort.SliceStable(b, func(i, j int) bool {
			return strings.IndexByte(wubrg, b[i]) < strings.IndexByte(wubrg, b[j])
		})
		return string(b)
	}
	return k
}

func isColorCombo(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if strings.IndexByte(wubrg, k[i]) < 0 {
			return false
		}
	}
	return true
}
