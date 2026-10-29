package render

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/odevine/impasto/blend"
	"github.com/odevine/impasto/canvas"
	"github.com/odevine/impasto/effects"
	"github.com/odevine/impasto/raster"

	"github.com/odevine/mimic/engine/card"
	"github.com/odevine/mimic/engine/fonts"
	"github.com/odevine/mimic/engine/frame"
	"github.com/odevine/mimic/engine/manifest"
	"github.com/odevine/mimic/engine/template"
)

// sectionJob is a section as it draws for this card
type sectionJob struct {
	sec  *template.Section
	spec manifest.Section
	face string
}

// resolveSections keeps the sections whose condition holds for the face each
// reads, with each section's geometry at the render's scale
func resolveSections(layout *template.Layout, scale template.Scale, fc *faces, features frame.Features) []sectionJob {
	var out []sectionJob
	for _, s := range layout.Sections {
		_, k := fc.get(s.Spec.Face)
		if !s.Condition.Met(k, features) {
			continue
		}
		out = append(out, sectionJob{sec: s, spec: template.ScaleSection(s.Spec, scale), face: s.Spec.Face})
	}
	return out
}

// faceKey is the name of the face a box or section reads, with empty as self
func faceKey(face string) string {
	if face == "" {
		return "self"
	}
	return face
}

// keyField is the row field that sets a row apart for a split: a loyalty
// ability's cost, a class level's mana cost, or a saga's chapter. A row without
// it, such as a static ability, sits in the section's plain column
func keyField(split string) string {
	switch split {
	case card.SplitChapters:
		return card.FieldChapter
	case card.SplitLevels:
		return card.FieldMana
	}
	return card.FieldLoyalty
}

// sectionPiece is one thing a section draws: an image of a part moved with its
// row and cut to a band when cut is set, or a text in a box
type sectionPiece struct {
	key  string
	part string
	path string
	at   image.Point // the part's authored place
	dy   int         // how far the image moves down in the scaled document
	cut  *[2]int
	// text pieces
	text  bool
	spec  template.TextBoxSpec
	parts []template.TextPart
	ink   image.Rectangle // a row's text, as it was measured
}

// placedSection is everything a section draws for this card, in draw order
type placedSection struct {
	job    sectionJob
	rows   []card.Row
	images []sectionPiece
	texts  []sectionPiece
}

// hides reports whether the request leaves a piece of a section out of the
// drawing, by the section's own key or the piece's. A description names a row's
// text and the images and texts beside it in the same kinds, so a hide that
// names what a description reports leaves out what it reports
func (ps *placedSection) hides(req template.RenderRequest, kind, key string) bool {
	return req.Hides("section", ps.job.sec.Key) || req.Hides(kind, key)
}

// placeSection splits the face's rules text into the section's rows, lays them
// out, and lists what each row draws
func placeSection(req template.RenderRequest, t *template.Template, layout *template.Layout, authored image.Point, job sectionJob, fc *faces, taken card.Taken, syms symbols, fr *fonts.Resolver, scale template.Scale) (*placedSection, error) {
	d, k := fc.get(job.face)
	spec := job.spec
	out := &placedSection{job: job, rows: card.Rows(spec.Split, card.SectionText(d, taken.Description))}
	if len(out.rows) == 0 {
		return out, nil
	}
	style := template.SpecOf("oracle", manifest.TextBox{Rect: spec.Rect, Text: spec.Text})
	body := manifest.Span{X: spec.Rect.X, Width: spec.Rect.Width}
	if spec.Body != nil {
		body = *spec.Body
	}
	plain := body
	if spec.Plain != nil {
		plain = *spec.Plain
	}
	key := keyField(spec.Split)
	rows := make([]template.RowText, len(out.rows))
	for i, r := range out.rows {
		col := body
		if !r.Has(key) {
			col = plain
		}
		rows[i] = template.RowText{Parts: rulesParts(r.Text, roleFor(spec.Text.Font, fr), syms, fr), X: col.X, Width: col.Width}
		if h := spec.Header; h != nil && r.Has(h.For) {
			rows[i].Header = h.Height
		}
	}
	var avoid image.Rectangle
	if spec.AvoidLayer != "" {
		if e, ok := layout.Entry(spec.AvoidLayer); ok {
			_, ek := fc.get(e.Face)
			if r, ok := template.EntryBounds(t.Assets(), e, ek, req.Features, scale, authored.X, authored.Y); ok {
				avoid = r
			}
		}
	}
	lay, err := template.LayoutSection(spec, style, avoid, rows)
	if err != nil {
		return nil, fmt.Errorf("render: laying out section %q: %w", job.sec.Key, err)
	}
	name := job.sec.Key
	n := len(lay.Rows)

	img := func(key, part string, dy int, cut *[2]int) {
		p, ok := layout.Part(part)
		if !ok {
			return
		}
		path, ok := p.Variants[k.Material(p.Slot).Key()]
		if !ok {
			if path, ok = p.Variants["any"]; !ok {
				return
			}
		}
		out.images = append(out.images, sectionPiece{key: key, part: part, path: path, at: p.Origin, dy: dy, cut: cut})
	}

	if b := spec.Bands; b != nil {
		for i := range n {
			if (i%2 == 1) != (b.Rows != "even") {
				continue
			}
			cut := [2]int{math.MinInt32, math.MaxInt32}
			if i > 0 {
				cut[0] = lay.Between(i - 1)
			}
			if i < n-1 {
				cut[1] = lay.Between(i)
			}
			bk := fmt.Sprintf("%s.bands[%d]", name, i)
			img(bk, b.Part, 0, &cut)
			if b.Top != nil && i > 0 {
				img(bk+".top", b.Top.Part, cut[0]-b.Top.Y, nil)
			}
			if b.Bottom != nil && i < n-1 {
				img(bk+".bottom", b.Bottom.Part, cut[1]-b.Bottom.Y, nil)
			}
		}
	}
	if dv := spec.Divider; dv != nil {
		for i := 0; i < n-1; i++ {
			img(fmt.Sprintf("%s.dividers[%d]", name, i), dv.Part, lay.Between(i)-dv.Y, nil)
		}
	}
	for i, r := range out.rows {
		pr := lay.Rows[i]
		rk := fmt.Sprintf("%s.rows[%d]", name, i)
		ink := image.Rect(pr.Box.X, pr.InkTop, pr.Box.X+pr.Box.Width, pr.InkBottom)
		out.texts = append(out.texts, sectionPiece{key: rk, text: true, spec: pr.Box, parts: rows[i].Parts, ink: ink})
		for j, it := range spec.Items {
			values := []string{""}
			if it.For != "" {
				values = r.Values(it.For)
			}
			anchor := pr.Center()
			switch it.Anchor {
			case "top":
				anchor = pr.Top
			case "first-line":
				anchor = pr.FirstLineCenter()
			}
			for v, value := range values {
				ik := fmt.Sprintf("%s.items[%d]", rk, j)
				if len(values) > 1 {
					ik += fmt.Sprintf("[%d]", v)
				}
				dy := anchor - it.Y + int(math.Round((float64(v)-float64(len(values)-1)/2)*float64(it.Step)))
				if it.Text != nil && it.Rect != nil {
					text := value
					if it.Format != "" {
						text = strings.ReplaceAll(it.Format, "{}", value)
					}
					rect := *it.Rect
					rect.Y += dy
					box := template.SpecOf("item", manifest.TextBox{Rect: rect, Text: *it.Text})
					p, ok := part(text, roleFor(it.Text.Font, fr), fr)
					if !ok {
						continue
					}
					p.Sym = syms.mana
					out.texts = append(out.texts, sectionPiece{key: ik, text: true, spec: box, parts: []template.TextPart{p}})
					continue
				}
				pn := it.Part
				if alt, ok := it.Parts[card.ValueKey(it.For, value)]; ok {
					pn = alt
				}
				if pn != "" {
					img(ik, pn, dy, nil)
				}
			}
		}
	}
	return out, nil
}

// rulesParts is rules text as one part, in the role with its reminder text and
// ability words in italic and its symbols drawn, the way the oracle box draws it
func rulesParts(text string, role fonts.Role, syms symbols, fr *fonts.Resolver) []template.TextPart {
	p, ok := part(text, role, fr)
	if !ok {
		return nil
	}
	p.Emph = fr.Font(fonts.BodyItalic)
	p.EmphLead = card.EmphasisWords
	p.Sym = syms.mana
	return []template.TextPart{p}
}

// imageNode is the node of a section's image, loaded when the compositor
// reaches it
func (p sectionPiece) imageNode(t *template.Template, scale template.Scale, authored image.Point) canvas.Node {
	assets := t.Assets()
	return &canvas.Layer{Mode: blend.Normal, Opacity: 1, LoadImage: func() (image.Image, image.Point, error) {
		img, at, err := template.LoadRowImage(assets, p.path, scale, p.at.X, p.at.Y, authored.X, authored.Y, p.dy, p.cut)
		if err != nil || img == nil {
			return nil, at, err
		}
		return img, at, nil
	}}
}

// textNode draws a section's text into a node of its own
func (p sectionPiece) textNode(docW, docH int) (canvas.Node, error) {
	res, err := template.RenderTextBox(p.spec, docW, docH, p.parts...)
	if err != nil {
		return nil, fmt.Errorf("render: rendering %s: %w", p.key, err)
	}
	buf, err := raster.FromImage(res.Image)
	if err != nil {
		return nil, fmt.Errorf("render: wrapping %s: %w", p.key, err)
	}
	layer := &canvas.Layer{Content: buf, Origin: res.Image.Bounds().Min, Mode: blend.Normal}
	if p.spec.Shadow != nil {
		layer.Effects = []effects.Effect{p.spec.Shadow.Effect(p.spec.FontSize)}
	}
	return layer, nil
}

// bounds is where a section's image lands in the authored canvas, from its
// visible pixels, or false when it shows none
func (p sectionPiece) bounds(t *template.Template) (image.Rectangle, bool) {
	img, err := template.LoadImage(t.Assets(), p.path)
	if err != nil {
		return image.Rectangle{}, false
	}
	r := template.OpaqueBounds(img)
	r = r.Sub(img.Bounds().Min).Add(p.at).Add(image.Pt(0, p.dy))
	if p.cut != nil {
		r = r.Intersect(image.Rect(r.Min.X, p.cut[0], r.Max.X, p.cut[1]))
	}
	return r, !r.Empty()
}
