package psd

import "fmt"

// Point is a position in document pixels, or a pair of values where a field says so
type Point struct{ X, Y float64 }

// VectorMask is a layer's vector mask or a shape layer's outline, read from its
// vmsk or vsms block
type VectorMask struct {
	Inverted  bool
	NotLinked bool
	Disabled  bool
	Subpaths  []Subpath
	// InitialFill is 1 when the fill starts with every pixel, so the first
	// subpath subtracts, and 0 when it starts empty
	InitialFill int
	// HasFillRule is set when the path carries a fill rule record
	HasFillRule bool
}

// Subpath is one closed or open run of knots
type Subpath struct {
	Closed bool
	Knots  []Knot
	// Operation combines the subpath with those before it: 1 add, 2 subtract,
	// 3 intersect, 0 exclude, and -1 where the file does not say
	Operation int
}

// Knot is a point on a bezier path with the control points before and after it,
// in document pixels
type Knot struct {
	Anchor, In, Out Point
	Linked          bool
}

// parseVectorMask reads a vmsk or vsms block. Path records store fractions of the
// document height and width as 8.24 fixed point numbers, which come out here
// scaled to the document's pixels
func parseVectorMask(b []byte, width, height int) (*VectorMask, error) {
	c := newCursor(b)
	version := c.u32()
	flags := c.u32()
	if c.err != nil {
		return nil, fmt.Errorf("psd: vector mask: %w", c.err)
	}
	if version != 3 {
		return nil, fmt.Errorf("psd: vector mask version %d", version)
	}
	vm := &VectorMask{
		Inverted:  flags&1 != 0,
		NotLinked: flags&2 != 0,
		Disabled:  flags&4 != 0,
	}
	vm.Subpaths, vm.InitialFill, vm.HasFillRule = parsePathRecords(c, width, height)
	if c.err != nil {
		return nil, fmt.Errorf("psd: vector mask: %w", c.err)
	}
	return vm, nil
}

// parsePathRecords reads 26 byte path records until fewer than 26 bytes remain
func parsePathRecords(c *cursor, width, height int) (subs []Subpath, initialFill int, fillRule bool) {
	point := func(r *cursor) Point {
		y := float64(r.i32()) / (1 << 24)
		x := float64(r.i32()) / (1 << 24)
		return Point{X: x * float64(width), Y: y * float64(height)}
	}
	cur := -1
	for c.remaining() >= 26 && c.err == nil {
		sel := c.u16()
		rec := newCursor(c.take(24))
		switch sel {
		case 0, 3:
			rec.u16()
			op := int(rec.i16())
			if op > 3 || op < -1 {
				op = -1
			}
			subs = append(subs, Subpath{Closed: sel == 0, Operation: op})
			cur = len(subs) - 1
		case 1, 2, 4, 5:
			if cur < 0 {
				subs = append(subs, Subpath{Closed: sel < 3, Operation: -1})
				cur = 0
			}
			k := Knot{Linked: sel == 1 || sel == 4}
			k.In = point(rec)
			k.Anchor = point(rec)
			k.Out = point(rec)
			subs[cur].Knots = append(subs[cur].Knots, k)
		case 6:
			fillRule = true
		case 8:
			initialFill = int(rec.u16())
		}
	}
	return subs, initialFill, fillRule
}
