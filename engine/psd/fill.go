package psd

import "fmt"

// Fill is the content of a fill or shape layer: a solid color, a gradient or a
// pattern
type Fill struct {
	// Type is "solid", "gradient" or "pattern"
	Type     string
	Color    Color
	Gradient *GradientSettings
	Pattern  *Pattern
	// Scale is the pattern scale in percent
	Scale float64
	Desc  *Descriptor
}

// parseFill reads a SoCo, GdFl or PtFl block, a descriptor version and descriptor
func parseFill(key string, b []byte) (*Fill, error) {
	c := newCursor(b)
	c.u32()
	d := readDescriptor(c, 0)
	if c.err != nil {
		return nil, fmt.Errorf("psd: %s fill: %w", key, c.err)
	}
	f := &Fill{Desc: d}
	switch key {
	case "SoCo":
		f.Type = "solid"
		f.Color = parseColor(d.Desc("Clr "))
	case "GdFl":
		f.Type = "gradient"
		f.Gradient = parseGradientSettings(d)
	case "PtFl":
		f.Type = "pattern"
		f.Pattern = parsePattern(d.Desc("Ptrn"))
		f.Scale = d.FloatOr("Scl ", 100)
	}
	return f, nil
}

// SmartObject is a placed or embedded layer's placement, read from SoLd, SoLE or
// PlLd
type SmartObject struct {
	// UniqueID matches the layer to its data in the document's lnk2, lnkD or lnk3 block
	UniqueID string
	// Transform is the placed content's corners in document pixels: top left,
	// top right, bottom right and bottom left as x and y pairs
	Transform [8]float64
	// NonAffineTransform is the same corners after any perspective distortion
	NonAffineTransform [8]float64
	// Width and Height are the content's own size in pixels
	Width, Height float64
	Page, Pages   int
	Desc          *Descriptor
	Warp          *Descriptor
}

// parseSmartObject reads a SoLd or SoLE block, which wraps a descriptor
func parseSmartObject(b []byte) (*SmartObject, error) {
	c := newCursor(b)
	c.key4()
	c.u32()
	c.u32()
	d := readDescriptor(c, 0)
	if c.err != nil {
		return nil, fmt.Errorf("psd: smart object: %w", c.err)
	}
	so := &SmartObject{Desc: d}
	so.UniqueID, _ = d.String("Idnt")
	if so.UniqueID == "" {
		so.UniqueID, _ = d.String("placed")
	}
	readEight(d.List("Trnf"), &so.Transform)
	readEight(d.List("nonAffineTransform"), &so.NonAffineTransform)
	if sz := d.Desc("Sz  "); sz != nil {
		so.Width = sz.FloatOr("Wdth", 0)
		so.Height = sz.FloatOr("Hght", 0)
	}
	so.Page = int(d.FloatOr("PgNm", 1))
	so.Pages = int(d.FloatOr("totalPages", 1))
	so.Warp = d.Desc("warp")
	return so, nil
}

func readEight(l []any, out *[8]float64) {
	for i := 0; i < len(l) && i < 8; i++ {
		out[i], _ = toFloat(l[i])
	}
}

// parsePlaced reads a PlLd block, the older binary form of a placed layer
func parsePlaced(b []byte) (*SmartObject, error) {
	c := newCursor(b)
	c.key4()
	c.u32()
	so := &SmartObject{}
	so.UniqueID = c.pascal(1)
	so.Page = int(c.u32())
	so.Pages = int(c.u32())
	c.u32()
	c.u32()
	for i := range so.Transform {
		so.Transform[i] = c.f64()
	}
	so.NonAffineTransform = so.Transform
	c.u32()
	c.u32()
	if c.err != nil {
		return nil, fmt.Errorf("psd: placed layer: %w", c.err)
	}
	if w := readDescriptor(c, 0); c.err == nil {
		so.Warp = w
	}
	return so, nil
}
