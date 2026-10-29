package template

import (
	"image"
	"image/draw"
	"math"
	"sort"

	"github.com/odevine/mimic/engine/frame"
	"github.com/odevine/mimic/engine/manifest"
)

// Section is a region of a layout whose rules text splits into rows, compiled
type Section struct {
	Key       string
	Spec      manifest.Section
	Condition frame.Condition
}

// compileSections compiles a layout's sections, sorted by key
func compileSections(ms map[string]manifest.Section) ([]*Section, error) {
	keys := make([]string, 0, len(ms))
	for k := range ms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*Section, 0, len(keys))
	for _, k := range keys {
		c, err := frame.Compile(ms[k].Condition)
		if err != nil {
			return nil, err
		}
		out = append(out, &Section{Key: k, Spec: ms[k], Condition: c})
	}
	return out, nil
}

// ScaleSection is a section with its pixel geometry and its text sizes at a scale
func ScaleSection(sec manifest.Section, s Scale) manifest.Section {
	out := sec
	out.Rect = scaleRect(sec.Rect, s)
	out.Text = scaleStyle(sec.Text, s)
	out.Body = scaleSpan(sec.Body, s)
	out.Plain = scaleSpan(sec.Plain, s)
	out.Gap = s.Px(sec.Gap)
	if h := sec.Header; h != nil {
		out.Header = &manifest.Header{For: h.For, Height: s.Px(h.Height)}
	}
	out.Items = make([]manifest.RowItem, len(sec.Items))
	for i, it := range sec.Items {
		it.Y = s.Px(it.Y)
		it.Step = s.Px(it.Step)
		if it.Rect != nil {
			r := scaleRect(*it.Rect, s)
			it.Rect = &r
		}
		if it.Text != nil {
			st := scaleStyle(*it.Text, s)
			it.Text = &st
		}
		out.Items[i] = it
	}
	out.Divider = scaleRowPart(sec.Divider, s)
	if b := sec.Bands; b != nil {
		out.Bands = &manifest.Bands{Part: b.Part, Rows: b.Rows, Top: scaleRowPart(b.Top, s), Bottom: scaleRowPart(b.Bottom, s)}
	}
	return out
}

func scaleRect(r manifest.Rect, s Scale) manifest.Rect {
	return manifest.Rect{X: s.Px(r.X), Y: s.Px(r.Y), Width: s.Px(r.Width), Height: s.Px(r.Height)}
}

// scaleStyle scales a text style's sizes and padding. Its spacing and tracking
// are relative to the font size already
func scaleStyle(t manifest.TextStyle, s Scale) manifest.TextStyle {
	t.FontSize = s.F(t.FontSize)
	t.MinFontSize = s.F(t.MinFontSize)
	t.PaddingX = s.Px(t.PaddingX)
	t.PaddingY = s.Px(t.PaddingY)
	return t
}

func scaleSpan(sp *manifest.Span, s Scale) *manifest.Span {
	if sp == nil {
		return nil
	}
	return &manifest.Span{X: s.Px(sp.X), Width: s.Px(sp.Width)}
}

func scaleRowPart(p *manifest.RowPart, s Scale) *manifest.RowPart {
	if p == nil {
		return nil
	}
	return &manifest.RowPart{Part: p.Part, Y: s.Px(p.Y)}
}

// RowText is the text of one row of a section, as the section lays it out:
// the parts it draws, the column it sits in, and the room a header takes above
// it
type RowText struct {
	Parts    []TextPart
	X, Width int
	Header   int
}

// PlacedRow is where a row of a section lands. Top is the top of the row with
// its header, and Box is the box its text draws in, whose text starts at its top
// and runs for Height
type PlacedRow struct {
	Top int
	Box TextBoxSpec
	// InkTop and InkBottom are the extent of the row's text
	InkTop, InkBottom int
	// LineHeight is the row's own baseline-to-baseline distance, which
	// FirstLineCenter uses. Zero for a row with nothing to draw
	LineHeight int
}

// Center is the middle of the row's text, which an item anchored at the center
// moves with
func (r PlacedRow) Center() int { return (r.InkTop + r.InkBottom) / 2 }

// FirstLineCenter is the middle of the row's opening line alone, which an item
// anchored at "first-line" moves with, such as a planeswalker's loyalty badge,
// authored beside one line and not meant to drift toward the middle of
// whichever row wraps the longest
func (r PlacedRow) FirstLineCenter() int { return r.InkTop + r.LineHeight/2 }

// SectionLayout is a section's rows as they land, and the size their text was
// set at
type SectionLayout struct {
	Size float64
	Rows []PlacedRow
}

// Between is the middle of the space between row i and the row after it, where a
// divider goes and a band ends
func (l SectionLayout) Between(i int) int {
	return (l.Rows[i].InkBottom + l.Rows[i+1].Top) / 2
}

// LayoutSection sets the rows of a section at the largest size from the style's
// font size down to its floor at which every row, its header, and the section's
// gaps fit the region, and spreads them over it from top to bottom, sharing the
// space left by the spread's weights. avoid is a rectangle the rows' text keeps
// out of, which is laid out against where the rows land without it, and the
// zero rectangle keeps nothing out. Rows that do not fit at the floor overflow
// the region's bottom
func LayoutSection(sec manifest.Section, style TextBoxSpec, avoid image.Rectangle, rows []RowText) (SectionLayout, error) {
	if len(rows) == 0 {
		return SectionLayout{Size: style.FontSize}, nil
	}
	out, err := layoutRows(sec, style, nil, rows)
	if err != nil || avoid.Empty() {
		return out, err
	}
	// The text keeps out of the rectangle where each row landed without it, so a
	// row beside it wraps narrower and the rows settle again. Wrapping narrower
	// makes a row taller, which moves the rows under it, so each pass measures
	// against the tops the pass before it produced until they stop moving. A
	// layout that keeps changing stops at the cap and takes the last pass
	tops := make([]int, len(out.Rows))
	for i, r := range out.Rows {
		tops[i] = r.Box.Y
	}
	for range avoidPasses {
		next, err := layoutRows(sec, style, &placedAvoid{rect: avoid, tops: tops}, rows)
		if err != nil {
			return SectionLayout{}, err
		}
		out = next
		settled := true
		for i, r := range next.Rows {
			if r.Box.Y != tops[i] {
				tops[i], settled = r.Box.Y, false
			}
		}
		if settled {
			break
		}
	}
	return out, nil
}

// avoidPasses is how many times the rows are laid out again to settle around an
// avoided rectangle. The fixtures settle in two
const avoidPasses = 8

// placedAvoid is a rectangle to keep out of with the place each row's box is
// taken to start at while it is measured
type placedAvoid struct {
	rect image.Rectangle
	tops []int
}

func layoutRows(sec manifest.Section, style TextBoxSpec, avoid *placedAvoid, rows []RowText) (SectionLayout, error) {
	region := sec.Rect
	outside, inside := 1.0, 1.0
	if sp := sec.Spread; sp != nil {
		if sp.Outside > 0 {
			outside = sp.Outside
		}
		if sp.Inside > 0 {
			inside = sp.Inside
		}
	}
	n := len(rows)
	weights := 2*outside + float64(n-1)*inside
	reserved := int(math.Round(float64(sec.Gap) * weights))

	box := func(i int, size float64) TextBoxSpec {
		b := style
		b.X, b.Width = rows[i].X, rows[i].Width
		b.Y, b.Height = region.Y, region.Height
		b.FontSize, b.MinFontSize = size, size
		b.VAlign = "top"
		if avoid != nil {
			b.Y = avoid.tops[i]
			b.Avoid = avoid.rect
		}
		return b
	}
	heights := make([]int, n)
	measure := func(size float64) (int, error) {
		total := reserved
		for i, r := range rows {
			ink, err := MeasureTextBox(box(i, size), r.Parts...)
			if err != nil {
				return 0, err
			}
			heights[i] = ink.Dy()
			total += heights[i] + r.Header
		}
		return total, nil
	}

	size := style.FontSize
	total, err := measure(size)
	if err != nil {
		return SectionLayout{}, err
	}
	if floor := minFontSize(style); total > region.Height && floor < size {
		lo, hi := floor, size
		size = floor
		for i := 0; i < 10 && hi-lo > 0.25; i++ {
			mid := (lo + hi) / 2
			t, err := measure(mid)
			if err != nil {
				return SectionLayout{}, err
			}
			if t <= region.Height {
				size, lo = mid, mid
			} else {
				hi = mid
			}
		}
		if _, err := measure(size); err != nil {
			return SectionLayout{}, err
		}
	}

	used := 0
	for i, r := range rows {
		used += heights[i] + r.Header
	}
	left := max(region.Height-used, 0)
	outGap, inGap := 0.0, 0.0
	if weights > 0 {
		outGap = float64(left) * outside / weights
		inGap = float64(left) * inside / weights
	}
	padY := axisPadding(style.PaddingY, style.Padding)
	out := SectionLayout{Size: size, Rows: make([]PlacedRow, n)}
	y := float64(region.Y) + outGap
	for i, r := range rows {
		top := int(math.Round(y))
		inkTop := top + r.Header
		b := box(i, size)
		// The box runs to the region's bottom so the text never clips, and its
		// text starts at its top, which the padding holds off the ink
		b.Y = inkTop - padY
		b.Height = max(region.Y+region.Height-b.Y, heights[i]+2*padY)
		lineHeight, err := FirstLineHeight(b, r.Parts...)
		if err != nil {
			return SectionLayout{}, err
		}
		out.Rows[i] = PlacedRow{Top: top, Box: b, InkTop: inkTop, InkBottom: inkTop + heights[i], LineHeight: lineHeight}
		y += float64(r.Header+heights[i]) + inGap
	}
	return out, nil
}

// LoadRowImage loads a part's image for a section the way a stack entry loads
// one placed at (x, y) in the authored document, then moves it down by dy in the
// scaled one. With cut set it keeps only the scaled document rows from cut[0] to
// cut[1], which is how a band is cut to its row, and returns the cut as a buffer.
// It returns nil for an image that lands on no pixel
func LoadRowImage(p AssetProvider, path string, s Scale, x, y, docW, docH, dy int, cut *[2]int) (image.Image, image.Point, error) {
	img, at, err := loadLayerImage(p, path, s, x, y, docW, docH, cut == nil)
	if err != nil || img == nil {
		return nil, at, err
	}
	at.Y += dy
	if cut == nil {
		return img, at, nil
	}
	b := img.Bounds()
	keep := image.Rect(b.Min.X, b.Min.Y+cut[0]-at.Y, b.Max.X, b.Min.Y+cut[1]-at.Y).Intersect(b)
	if keep.Empty() {
		return nil, at, nil
	}
	out := image.NewRGBA(image.Rect(0, 0, keep.Dx(), keep.Dy()))
	draw.Draw(out, out.Bounds(), img, keep.Min, draw.Src)
	return out, at.Add(keep.Min.Sub(b.Min)), nil
}
