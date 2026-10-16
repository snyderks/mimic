package proxyshop

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/odevine/mimic/engine/psd"
	"github.com/odevine/mimic/engine/psdrender"
)

// Render draws a part's variants through the compositor. Each variant draws
// Target on its own, the way Proxyshop leaves the document after it shows the
// layers and adds the fills a card needs
type Render struct {
	// Target is the path from the document root to the layer or group drawn
	Target []string `json:"target"`
	// Show and Hide set layers visible or hidden for every variant, each named
	// by its path from the document root
	Show [][]string `json:"show,omitempty"`
	Hide [][]string `json:"hide,omitempty"`
	// Masks switches on the masks of these layers, which a template keeps off
	// until a card needs them
	Masks [][]string `json:"masks,omitempty"`
	// CopyMasks copies masks between layers, as Proxyshop does from its Masks
	// group
	CopyMasks []MaskCopy `json:"copyMasks,omitempty"`
	// Variants says what each variant changes on top of the above, by key
	Variants Object `json:"variants"`
	// Cut draws each of these layers on its own and cuts its pixels out of every
	// variant, the way a knockout group in the PSD clears what it covers
	Cut []RenderCut `json:"cut,omitempty"`
	// FitPairs measures how the part's pair images mix their two colors, and
	// when a blend of the single color images draws them closely, leaves the
	// pairs out and writes that blend for the part
	FitPairs bool `json:"fitPairs,omitempty"`
	// AssetVariants names embedded fallback art for variant keys this PSD's
	// own layers do not cover, such as a transform icon group missing the
	// plain triangle a card with no named frame effect defaults to. A key
	// already produced from the PSD wins over its fallback
	AssetVariants []string `json:"assetVariants,omitempty"`
}

// RenderCut is a layer drawn on its own, with the layers in Show shown, whose
// pixels are cut out of a part's images
type RenderCut struct {
	Target []string   `json:"target"`
	Show   [][]string `json:"show,omitempty"`
}

// RenderVariant is what one variant shows, hides and adds
type RenderVariant struct {
	// Target replaces the render's target for this variant, such as one of the
	// basic land layers that sit at the document's top level
	Target []string     `json:"target,omitempty"`
	Show   [][]string   `json:"show,omitempty"`
	Hide   [][]string   `json:"hide,omitempty"`
	Fills  []RenderFill `json:"fills,omitempty"`
}

// RenderFill is a fill layer added to the document, the way Proxyshop colors a
// vector template's shapes
type RenderFill struct {
	// Into is the path of the group the fill goes on top of, the document root
	// when it is empty
	Into []string `json:"into,omitempty"`
	// Clip clips the fill to the layer below it
	Clip bool `json:"clip,omitempty"`
	// Blend is a Photoshop blend mode name such as "multiply". Empty is normal
	Blend string `json:"blend,omitempty"`
	// Color is a solid fill as "#rrggbb"
	Color string `json:"color,omitempty"`
	// Gradient is a linear gradient fill in place of Color
	Gradient *RenderGradient `json:"gradient,omitempty"`
}

// RenderGradient is a linear gradient across the canvas
type RenderGradient struct {
	// Stops are the colors in order, each at a location from 0 to 1
	Stops []GradientStop `json:"stops"`
	// Angle is in degrees counterclockwise from pointing right. Zero runs left
	// to right
	Angle float64 `json:"angle,omitempty"`
}

// GradientStop is one color of a gradient
type GradientStop struct {
	Color    string  `json:"color"`
	Location float64 `json:"location"`
}

// variants reads the render's variants in order
func (r *Render) variants() ([]string, []RenderVariant, error) {
	keys := make([]string, 0, len(r.Variants))
	vs := make([]RenderVariant, 0, len(r.Variants))
	for _, m := range r.Variants {
		var v RenderVariant
		if err := json.Unmarshal(m.Value, &v); err != nil {
			return nil, nil, fmt.Errorf("variant %q: %w", m.Key, err)
		}
		keys = append(keys, m.Key)
		vs = append(vs, v)
	}
	return keys, vs, nil
}

// renderJobs bounds how many variants draw at once, since a variant of a full
// card holds a few hundred megabytes of float pixels while it draws
const renderJobs = 3

// buildRendered draws every variant of a render part, crops them all to the
// pixels any of them covers, and writes them
func buildRendered(doc *psd.File, np NamedPart, rd *psdrender.Renderer, put func(string, []byte), note func(string, ...any)) (*Rect, Object, error) {
	p := np.Rule
	r := p.Render
	find := func(path []string) (*psd.Layer, error) {
		if len(path) == 0 {
			return doc.Root, nil
		}
		l := doc.Root.Find(path...)
		if l == nil {
			return nil, fmt.Errorf("the source has no layer %s", strings.Join(path, " / "))
		}
		return l, nil
	}
	target, err := find(r.Target)
	if err != nil {
		return nil, nil, err
	}
	keys, vs, err := r.variants()
	if err != nil {
		return nil, nil, err
	}
	omit := map[string]bool{}
	for _, k := range p.Omit {
		omit[k] = true
	}

	base := map[*psd.Layer]bool{}
	masks := map[*psd.Layer]bool{}
	for _, path := range r.Show {
		l, err := find(path)
		if err != nil {
			return nil, nil, err
		}
		base[l] = true
	}
	for _, path := range r.Hide {
		l, err := find(path)
		if err != nil {
			return nil, nil, err
		}
		base[l] = false
	}
	for _, path := range r.Masks {
		l, err := find(path)
		if err != nil {
			return nil, nil, err
		}
		masks[l] = true
	}

	maskFrom := map[*psd.Layer]*psd.Layer{}
	vectorFrom := map[*psd.Layer]*psd.Layer{}
	hides := map[*psd.Layer]bool{}
	for _, mc := range r.CopyMasks {
		from, err := find(mc.From)
		if err != nil {
			return nil, nil, err
		}
		to, err := find(mc.To)
		if err != nil {
			return nil, nil, err
		}
		if mc.Vector {
			vectorFrom[to] = from
		} else {
			maskFrom[to] = from
		}
		if mc.HidesEffects {
			hides[to] = true
		}
	}

	type job struct {
		key    string
		target *psd.Layer
		o      *psdrender.Overrides
	}
	var jobs []job
	for i, v := range vs {
		if omit[keys[i]] {
			continue
		}
		o := &psdrender.Overrides{Visible: map[*psd.Layer]bool{}, Mask: masks, VectorMask: masks, Insert: map[*psd.Layer][]*psdrender.Inserted{},
			MaskFrom: maskFrom, VectorMaskFrom: vectorFrom, MaskHidesEffects: hides}
		for l, on := range base {
			o.Visible[l] = on
		}
		for _, path := range v.Show {
			l, err := find(path)
			if err != nil {
				return nil, nil, fmt.Errorf("variant %q: %w", keys[i], err)
			}
			o.Visible[l] = true
		}
		for _, path := range v.Hide {
			l, err := find(path)
			if err != nil {
				return nil, nil, fmt.Errorf("variant %q: %w", keys[i], err)
			}
			o.Visible[l] = false
		}
		for _, f := range v.Fills {
			into, err := find(f.Into)
			if err != nil {
				return nil, nil, fmt.Errorf("variant %q: %w", keys[i], err)
			}
			ins, err := insertedFill(f, doc)
			if err != nil {
				return nil, nil, fmt.Errorf("variant %q: %w", keys[i], err)
			}
			if into == doc.Root {
				into = nil
			}
			o.Insert[into] = append(o.Insert[into], ins)
		}
		vt := target
		if len(v.Target) > 0 {
			if vt, err = find(v.Target); err != nil {
				return nil, nil, fmt.Errorf("variant %q: %w", keys[i], err)
			}
		}
		jobs = append(jobs, job{keys[i], vt, o})
	}
	if len(jobs) == 0 {
		return nil, nil, fmt.Errorf("no variant to draw")
	}

	canvas := image.Rect(0, 0, doc.Width, doc.Height)
	region := canvas
	if p.Crop != nil {
		region = p.Crop.image()
	} else {
		region = image.Rectangle{}
		for _, j := range jobs {
			region = region.Union(extent(j.target, canvas))
		}
	}
	imgs := make([]*image.NRGBA, len(jobs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	sem := make(chan struct{}, renderJobs)
	for i, j := range jobs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			buf, err := rd.Layer(j.target, region, j.o)
			if err != nil {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("variant %q: %w", j.key, err)
				}
				mu.Unlock()
				return
			}
			imgs[i] = buf.NRGBA()
		})
	}
	wg.Wait()
	if first != nil {
		return nil, nil, first
	}

	for _, c := range r.Cut {
		if err := cutOut(doc, rd, c, region, imgs, find); err != nil {
			return nil, nil, err
		}
	}

	var blend Object
	if r.FitPairs {
		byKey := map[string]*image.NRGBA{}
		for i, j := range jobs {
			byKey[j.key] = imgs[i]
		}
		if fit := fitPairs(byKey, doc.Width); fit != nil {
			if fit.Drop {
				note("%s: blends its pairs, %s", np.Name, fit)
				blend = fit.blend()
				keep := jobs[:0]
				kept := imgs[:0]
				for i, j := range jobs {
					if len(j.key) == 2 && isColorCombo(j.key) {
						if l, r := pairRun(j.key); byKey[l] != nil && byKey[r] != nil {
							continue
						}
					}
					keep = append(keep, j)
					kept = append(kept, imgs[i])
				}
				jobs, imgs = keep, kept
			} else {
				note("%s: keeps its pair images, since the closest blend is off, %s", np.Name, fit)
			}
		}
	}

	crop := region
	if p.Crop == nil {
		crop = image.Rectangle{}
		for _, img := range imgs {
			crop = crop.Union(opaqueBounds(img))
		}
		if crop.Empty() {
			note("%s: every variant is empty", np.Name)
			crop = image.Rect(region.Min.X, region.Min.Y, region.Min.X+1, region.Min.Y+1)
		}
	}
	var out []variant
	for i, j := range jobs {
		img := imgs[i]
		out = append(out, variant{j.key, func() (*image.NRGBA, error) { return cropTo(img, crop), nil }})
	}
	have := map[string]bool{}
	for _, j := range jobs {
		have[j.key] = true
	}
	for _, key := range r.AssetVariants {
		if have[key] || omit[key] {
			continue
		}
		path, ok := transformIconAssets[key]
		if !ok {
			return nil, nil, fmt.Errorf("assetVariants: %q has no embedded fallback", key)
		}
		out = append(out, variant{key, func() (*image.NRGBA, error) { return loadAssetFitted(path, crop) }})
	}
	if err := encodeAll(out, func(key string, data []byte) {
		put(p.Dir+"/"+key+".png", data)
	}); err != nil {
		return nil, nil, err
	}
	return rectOf(crop), blend, nil
}

// insertedFill turns a recipe fill into a layer the compositor adds
func insertedFill(f RenderFill, doc *psd.File) (*psdrender.Inserted, error) {
	ins := &psdrender.Inserted{Name: "fill", Clipping: f.Clip, Blend: psd.BlendNormal}
	if f.Blend != "" {
		m, ok := blendNamed(f.Blend)
		if !ok {
			return nil, fmt.Errorf("unknown blend mode %q", f.Blend)
		}
		ins.Blend = m
	}
	switch {
	case f.Gradient != nil:
		g := &psd.Gradient{Form: "custom", Smoothness: 4096}
		for _, s := range f.Gradient.Stops {
			c, err := parseColorHex(s.Color)
			if err != nil {
				return nil, err
			}
			g.Stops = append(g.Stops, psd.ColorStop{Location: int(math.Round(s.Location * 4096)), Midpoint: 50, Color: c, Type: "user"})
		}
		g.Transparency = []psd.OpacityStop{{Location: 0, Midpoint: 50, Opacity: 1}, {Location: 4096, Midpoint: 50, Opacity: 1}}
		ins.Fill = &psd.Fill{Type: "gradient", Gradient: &psd.GradientSettings{Gradient: g, Angle: f.Gradient.Angle, Style: "linear", Scale: 100}}
	case f.Color != "":
		c, err := parseColorHex(f.Color)
		if err != nil {
			return nil, err
		}
		ins.Fill = &psd.Fill{Type: "solid", Color: c}
	default:
		return nil, fmt.Errorf("a fill needs a color or a gradient")
	}
	return ins, nil
}

// blendNamed reads a blend mode by the name psd.BlendMode prints
func blendNamed(name string) (psd.BlendMode, bool) {
	for m := psd.BlendNormal; m <= psd.BlendLuminosity; m++ {
		if m.String() == name {
			return m, true
		}
	}
	return psd.BlendNormal, false
}

func parseColorHex(s string) (psd.Color, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) != 6 {
		return psd.Color{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return psd.Color{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	return psd.Color{R: float64(v >> 16 & 0xff), G: float64(v >> 8 & 0xff), B: float64(v & 0xff), Model: "rgb"}, nil
}

// extent is the part of the canvas a layer and everything in it can draw on,
// with room for the reach of its styles
func extent(l *psd.Layer, canvas image.Rectangle) image.Rectangle {
	var r image.Rectangle
	var walk func(l *psd.Layer, pad int)
	walk = func(l *psd.Layer, pad int) {
		pad += styleReach(l)
		switch {
		case l.IsGroup():
			for _, c := range l.Children {
				walk(c, pad)
			}
		case l.Kind == psd.KindSolidFill || l.Kind == psd.KindGradientFill || l.Kind == psd.KindPatternFill:
			if l.VectorMask != nil && !l.Rect.Empty() {
				r = r.Union(l.Rect.Inset(-pad))
			} else {
				r = canvas
			}
		case !l.Rect.Empty():
			r = r.Union(l.Rect.Inset(-pad))
		}
	}
	walk(l, 0)
	if r.Empty() {
		return canvas
	}
	return r.Intersect(canvas)
}

// styleReach is how far past its pixels a layer's styles can draw
func styleReach(l *psd.Layer) int {
	if l.Effects == nil {
		return 0
	}
	reach := 0.0
	for _, e := range l.Effects.List() {
		if !e.Base().Active() {
			continue
		}
		switch e := e.(type) {
		case *psd.Shadow:
			reach = math.Max(reach, e.Distance+e.Size*2)
		case *psd.Glow:
			reach = math.Max(reach, e.Size*2)
		case *psd.Stroke:
			reach = math.Max(reach, e.Size)
		case *psd.Bevel:
			reach = math.Max(reach, e.Size*2)
		}
	}
	return int(math.Ceil(reach)) + 4
}

// opaqueBounds is the smallest rectangle holding every pixel with any alpha
func opaqueBounds(img *image.NRGBA) image.Rectangle {
	b := img.Rect
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[img.PixOffset(b.Min.X, y):]
		for x := b.Min.X; x < b.Max.X; x++ {
			if row[4*(x-b.Min.X)+3] == 0 {
				continue
			}
			minX, maxX = min(minX, x), max(maxX, x+1)
			minY, maxY = min(minY, y), max(maxY, y+1)
		}
	}
	if maxX <= minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// cropTo copies img's pixels inside r to a new image whose corner is at zero
func cropTo(img *image.NRGBA, r image.Rectangle) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	in := r.Intersect(img.Rect)
	for y := in.Min.Y; y < in.Max.Y; y++ {
		s := img.PixOffset(in.Min.X, y)
		d := out.PixOffset(in.Min.X-r.Min.X, y-r.Min.Y)
		copy(out.Pix[d:d+4*in.Dx()], img.Pix[s:s+4*in.Dx()])
	}
	return out
}

// cutOut draws the cut's layer over region and scales each image's alpha by
// how much of each pixel the layer leaves uncovered
func cutOut(doc *psd.File, rd *psdrender.Renderer, c RenderCut, region image.Rectangle, imgs []*image.NRGBA, find func([]string) (*psd.Layer, error)) error {
	l, err := find(c.Target)
	if err != nil {
		return fmt.Errorf("cut: %w", err)
	}
	o := &psdrender.Overrides{Visible: map[*psd.Layer]bool{l: true}}
	for _, path := range c.Show {
		s, err := find(path)
		if err != nil {
			return fmt.Errorf("cut: %w", err)
		}
		o.Visible[s] = true
	}
	buf, err := rd.Layer(l, region, o)
	if err != nil {
		return fmt.Errorf("cut: %w", err)
	}
	cut := buf.NRGBA()
	for _, img := range imgs {
		b := img.Rect.Intersect(cut.Rect)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				ca := cut.Pix[cut.PixOffset(x, y)+3]
				if ca == 0 {
					continue
				}
				i := img.PixOffset(x, y) + 3
				img.Pix[i] = uint8((int(img.Pix[i])*(255-int(ca)) + 127) / 255)
			}
		}
	}
	return nil
}
