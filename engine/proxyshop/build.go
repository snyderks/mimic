package proxyshop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/odevine/mimic/engine/psd"
	"github.com/odevine/mimic/engine/psdrender"
)

// Opener opens a source PSD by the file name a recipe gives it
type Opener func(name string) (*psd.File, error)

// DirOpener opens source PSDs from a folder
func DirOpener(dir string) Opener {
	return func(name string) (*psd.File, error) {
		return psd.Open(filepath.Join(dir, filepath.FromSlash(name)))
	}
}

// Output is a template's source folder, each file keyed by its slash
// separated path inside the folder
type Output struct {
	Files map[string][]byte
	// Notes are things a person reading the result should know, such as a key
	// a recipe asks for that its group does not have
	Notes []string
}

// Paths lists the files in order
func (o *Output) Paths() []string {
	out := make([]string, 0, len(o.Files))
	for p := range o.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// WriteDir writes the files under dir, leaving other files there alone
func (o *Output) WriteDir(dir string) error {
	for _, p := range o.Paths() {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, o.Files[p], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// pngEncoder pins the compression so a rebuild writes the same bytes
var pngEncoder = png.Encoder{CompressionLevel: png.BestCompression}

// Build cuts the recipe's parts from its sources and writes the template's
// source folder: template.json, a layout file per layout, and the images. The
// sources are opened one at a time, since each can hold gigabytes of pixels
func Build(r *Recipe, open Opener) (*Output, error) { return BuildProgress(r, open, nil) }

// Progress hears how far a build has come: the part it starts, and how many of
// the recipe's parts are done
type Progress func(part string, done, total int)

// BuildProgress is Build reporting each part as it starts to progress, which
// may be nil
func BuildProgress(r *Recipe, open Opener, progress Progress) (*Output, error) {
	if err := r.Check(); err != nil {
		return nil, err
	}
	out := &Output{Files: map[string][]byte{}}
	var mu sync.Mutex
	put := func(p string, data []byte) {
		mu.Lock()
		out.Files[p] = data
		mu.Unlock()
	}
	note := func(format string, args ...any) {
		mu.Lock()
		out.Notes = append(out.Notes, fmt.Sprintf(format, args...))
		mu.Unlock()
	}

	parts := Object{}
	done := 0
	crops := map[string]*Rect{}
	blends := map[string]Object{}
	for _, src := range r.sourceNames() {
		var rules []NamedPart
		for _, np := range r.Parts {
			if np.Rule.Source == src {
				rules = append(rules, np)
			}
		}
		if len(rules) == 0 {
			continue
		}
		doc, err := open(r.Sources[src])
		if err != nil {
			return nil, fmt.Errorf("opening source %q: %w", src, err)
		}
		var rd *psdrender.Renderer
		for _, np := range rules {
			if progress != nil {
				progress(np.Name, done, len(r.Parts))
			}
			done++
			var err error
			if np.Rule.Render != nil {
				if rd == nil {
					rd = psdrender.New(doc)
				}
				var crop *Rect
				var blend Object
				crop, blend, err = buildRendered(doc, np, rd, put, note)
				crops[np.Name] = crop
				if blend != nil {
					blends[np.Name] = blend
				}
			} else {
				err = buildPart(doc, np, put, note)
			}
			if err != nil {
				doc.Close()
				return nil, fmt.Errorf("part %q: %w", np.Name, err)
			}
		}
		if rd != nil {
			for _, n := range rd.Notes() {
				note("%s: %s", src, n)
			}
		}
		doc.Close()
	}
	for _, np := range r.Parts {
		rule := np.Rule
		if c := crops[np.Name]; c != nil && rule.Crop == nil {
			rule.Crop = c
		}
		if b := blends[np.Name]; b != nil && len(rule.Blend) == 0 {
			rule.Blend = mustRaw(b)
		}
		entry, err := partEntry(rule)
		if err != nil {
			return nil, fmt.Errorf("part %q: %w", np.Name, err)
		}
		if err := parts.Set(np.Name, entry); err != nil {
			return nil, err
		}
	}

	tmpl := append(Object{}, r.Template...)
	if err := tmpl.Set("parts", parts); err != nil {
		return nil, err
	}
	if err := putJSON(put, "template.json", tmpl); err != nil {
		return nil, err
	}
	for _, m := range r.Layouts {
		if err := putJSON(put, path.Join("layouts", m.Key+".json"), m.Value); err != nil {
			return nil, fmt.Errorf("layout %q: %w", m.Key, err)
		}
	}
	sort.Strings(out.Notes)
	if progress != nil {
		progress("", len(r.Parts), len(r.Parts))
	}
	return out, nil
}

func putJSON(put func(string, []byte), name string, v any) error {
	raw, err := marshalRaw(v)
	if err != nil {
		return err
	}
	formatted, err := Indent(raw)
	if err != nil {
		return err
	}
	put(name, formatted)
	return nil
}

// partEntry is a part as template.json lists it
func partEntry(p PartRule) (Object, error) {
	e := Object{}
	if err := e.Set("dir", p.Dir); err != nil {
		return nil, err
	}
	if p.Slot != "" {
		if err := e.Set("slot", p.Slot); err != nil {
			return nil, err
		}
	}
	switch {
	case len(p.Origin) > 0:
		if err := e.Set("origin", p.Origin); err != nil {
			return nil, err
		}
	case p.Crop != nil:
		if err := e.Set("origin", map[string]int{"x": p.Crop.X0, "y": p.Crop.Y0}); err != nil {
			return nil, err
		}
	}
	if len(p.Blend) > 0 {
		if err := e.Set("blend", p.Blend); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// layerSource reads a PSD layer's pixels once, however many variants use it
type layerSource struct {
	l    *psd.Layer
	once sync.Once
	img  *image.NRGBA
	err  error
}

func (s *layerSource) bounds() image.Rectangle { return s.l.Rect }

func (s *layerSource) pixels() (*image.NRGBA, error) {
	s.once.Do(func() { s.img, s.err = s.l.Image() })
	return s.img, s.err
}

// variant is one image a part writes
type variant struct {
	key  string
	make func() (*image.NRGBA, error)
}

func buildPart(doc *psd.File, np NamedPart, put func(string, []byte), note func(string, ...any)) error {
	p := np.Rule
	group := doc.Root
	if len(p.Group) > 0 {
		if group = doc.Root.Find(p.Group...); group == nil || !group.IsGroup() {
			return fmt.Errorf("the source has no group %v", p.Group)
		}
	}
	crop := image.Rect(0, 0, doc.Width, doc.Height)
	if p.Crop != nil {
		crop = p.Crop.image()
	}
	// The group's layers bottom to top, as the file stores them
	layers := make([]*psd.Layer, len(group.Children))
	for i, l := range group.Children {
		layers[len(layers)-1-i] = l
	}
	sources := map[*psd.Layer]*layerSource{}
	src := func(l *psd.Layer) *layerSource {
		s, ok := sources[l]
		if !ok {
			s = &layerSource{l: l}
			sources[l] = s
		}
		return s
	}
	find := func(name string) int {
		for i, l := range layers {
			if l.Name == name {
				return i
			}
		}
		return -1
	}
	base := func(i int) pixelSource {
		if !p.Clip || !layers[i].Clipping {
			return nil
		}
		for j := i - 1; j >= 0; j-- {
			if !layers[j].Clipping {
				return src(layers[j])
			}
		}
		return nil
	}

	var bake *bakePlanes
	if p.Bake != nil {
		i := find(p.Bake.Shape)
		if i < 0 || layers[i].IsGroup() {
			return fmt.Errorf("the bake shape %q is not a layer in the group", p.Bake.Shape)
		}
		shape, err := unionAlpha([]pixelSource{src(layers[i])}, crop)
		if err != nil {
			return err
		}
		if bake, err = prepareBake(shape, crop.Dx(), crop.Dy(), p.Bake); err != nil {
			return err
		}
	}
	finish := func(img *image.NRGBA) *image.NRGBA {
		if bake != nil {
			bake.apply(img)
		}
		return img
	}

	omit := map[string]bool{}
	for _, k := range p.Omit {
		omit[k] = true
	}
	var vs []variant
	add := func(key string, render func() (*image.NRGBA, error)) {
		if !omit[key] {
			vs = append(vs, variant{key, render})
		}
	}
	cutAt := func(i int) func() (*image.NRGBA, error) {
		l, b := src(layers[i]), base(i)
		return func() (*image.NRGBA, error) {
			img, err := cutLayer(l, crop, b)
			if err != nil {
				return nil, err
			}
			return finish(img), nil
		}
	}

	switch {
	case p.Tint != nil:
		var shapes []pixelSource
		for _, name := range p.Tint.From {
			i := find(name)
			if i < 0 || layers[i].IsGroup() {
				return fmt.Errorf("the tint source %q is not a layer in the group", name)
			}
			shapes = append(shapes, src(layers[i]))
		}
		plane, err := unionAlpha(shapes, crop)
		if err != nil {
			return err
		}
		if s := p.Tint.Stroke; s != nil {
			plane = strokePlane(plane, crop.Dx(), crop.Dy(), s.Width, s.Outside)
		}
		for _, m := range p.Tint.Colors {
			var spec string
			if err := json.Unmarshal(m.Value, &spec); err != nil {
				return fmt.Errorf("tint color %q: %w", m.Key, err)
			}
			add(m.Key, func() (*image.NRGBA, error) {
				img, err := tintImage(plane, crop.Dx(), crop.Dy(), spec)
				if err != nil {
					return nil, err
				}
				return finish(img), nil
			})
		}
	case p.All:
		seen := map[string]string{}
		for i, l := range layers {
			if l.IsGroup() {
				continue
			}
			key := NormalizeKey(l.Name)
			if prev, ok := seen[key]; ok {
				return fmt.Errorf("layers %q and %q both make the key %q", prev, l.Name, key)
			}
			seen[key] = l.Name
			add(key, cutAt(i))
		}
	default:
		for _, m := range p.Layers {
			var name string
			if err := json.Unmarshal(m.Value, &name); err != nil {
				return fmt.Errorf("layer for %q: %w", m.Key, err)
			}
			i := find(name)
			if i < 0 || layers[i].IsGroup() {
				note("%s: the group %v has no layer %q for the key %q", np.Name, p.Group, name, m.Key)
				continue
			}
			add(m.Key, cutAt(i))
		}
	}
	if len(vs) == 0 {
		return fmt.Errorf("no variant resolved to a layer")
	}
	return encodeAll(vs, func(key string, data []byte) {
		put(path.Join(p.Dir, key+".png"), data)
	})
}

// encodeAll makes and encodes the variants on every core, keeping the first
// error
func encodeAll(vs []variant, put func(string, []byte)) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	sem := make(chan struct{}, runtime.NumCPU())
	for _, v := range vs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			err := func() error {
				img, err := v.make()
				if err != nil {
					return err
				}
				var b bytes.Buffer
				if err := pngEncoder.Encode(&b, img); err != nil {
					return err
				}
				put(v.key, b.Bytes())
				return nil
			}()
			if err != nil {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("variant %q: %w", v.key, err)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return first
}
