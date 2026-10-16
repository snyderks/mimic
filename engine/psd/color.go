package psd

import "math"

// Color is a color from a descriptor, converted to sRGB channels from 0 to 255.
// Model names how it was stored: "rgb", "gray", "hsb", "cmyk", "lab" or "book".
// A book color carries its book and name and no channels of its own, and a cmyk
// color is converted without a profile
type Color struct {
	R, G, B float64
	Model   string
	Book    string
	Name    string
	Desc    *Descriptor
}

// parseColor reads a color descriptor of class RGBC, Grsc, HSBC, CMYC, LbCl or BkCl
func parseColor(d *Descriptor) Color {
	if d == nil {
		return Color{}
	}
	c := Color{Desc: d}
	switch d.Class {
	case "Grsc":
		g := 255 * (1 - d.FloatOr("Gry ", 0)/100)
		c.Model, c.R, c.G, c.B = "gray", g, g, g
	case "HSBC":
		h, _ := d.Float("H   ")
		s := d.FloatOr("Strt", 0) / 100
		v := d.FloatOr("Brgh", 0) / 100
		r, g, b := hsbToRGB(h, s, v)
		c.Model, c.R, c.G, c.B = "hsb", r*255, g*255, b*255
	case "CMYC":
		cy := d.FloatOr("Cyn ", 0) / 100
		m := d.FloatOr("Mgnt", 0) / 100
		y := d.FloatOr("Ylw ", 0) / 100
		k := d.FloatOr("Blck", 0) / 100
		c.Model = "cmyk"
		c.R = 255 * (1 - cy) * (1 - k)
		c.G = 255 * (1 - m) * (1 - k)
		c.B = 255 * (1 - y) * (1 - k)
	case "LbCl":
		r, g, b := labToSRGB(d.FloatOr("Lmnc", 0), d.FloatOr("A   ", 0), d.FloatOr("B   ", 0))
		c.Model, c.R, c.G, c.B = "lab", r*255, g*255, b*255
	case "BkCl":
		c.Model = "book"
		c.Book, _ = d.String("Bk  ")
		c.Name, _ = d.String("Nm  ")
	default:
		c.Model = "rgb"
		if d.Has("redFloat") {
			c.R = d.FloatOr("redFloat", 0) * 255
			c.G = d.FloatOr("greenFloat", 0) * 255
			c.B = d.FloatOr("blueFloat", 0) * 255
		} else {
			c.R = d.FloatOr("Rd  ", 0)
			c.G = d.FloatOr("Grn ", 0)
			c.B = d.FloatOr("Bl  ", 0)
		}
	}
	return c
}

func hsbToRGB(h, s, v float64) (r, g, b float64) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}

// labToSRGB converts a D50 Lab color to sRGB through a Bradford adaptation to D65
func labToSRGB(l, a, bb float64) (r, g, b float64) {
	fy := (l + 16) / 116
	fx := fy + a/500
	fz := fy - bb/200
	inv := func(t float64) float64 {
		if t3 := t * t * t; t3 > 216.0/24389 {
			return t3
		}
		return (116*t - 16) / (24389.0 / 27)
	}
	x := inv(fx) * 0.96422
	y := inv(fy)
	z := inv(fz) * 0.82521
	x, y, z = 0.9555766*x-0.0230393*y+0.0631636*z,
		-0.0282895*x+1.0099416*y+0.0210077*z,
		0.0122982*x-0.0204830*y+1.3299098*z
	lin := [3]float64{
		3.2404542*x - 1.5371385*y - 0.4985314*z,
		-0.9692660*x + 1.8760108*y + 0.0415560*z,
		0.0556434*x - 0.2040259*y + 1.0572252*z,
	}
	for i, v := range lin {
		v = min(max(v, 0), 1)
		if v <= 0.0031308 {
			v *= 12.92
		} else {
			v = 1.055*math.Pow(v, 1/2.4) - 0.055
		}
		lin[i] = v
	}
	return lin[0], lin[1], lin[2]
}

// Gradient is a gradient definition. Locations run from 0 to 4096 across the
// gradient and midpoints from 0 to 100 percent of the way to the next stop
type Gradient struct {
	Name string
	// Form is "custom" for stops or "noise" for a generated gradient
	Form string
	// Smoothness is the interpolation from 0 to 4096 of a custom gradient
	Smoothness   float64
	Stops        []ColorStop
	Transparency []OpacityStop
	Desc         *Descriptor
}

// ColorStop is a color at a place on a gradient. Type is "user", "foreground"
// or "background"
type ColorStop struct {
	Location int
	Midpoint int
	Color    Color
	Type     string
}

// OpacityStop is an opacity from 0 to 1 at a place on a gradient
type OpacityStop struct {
	Location int
	Midpoint int
	Opacity  float64
}

func parseGradient(d *Descriptor) *Gradient {
	if d == nil {
		return nil
	}
	g := &Gradient{Desc: d, Form: "custom"}
	g.Name, _ = d.String("Nm  ")
	g.Name = trimNul(g.Name)
	if f, ok := d.Enum("GrdF"); ok && f == "ClNs" {
		g.Form = "noise"
	}
	g.Smoothness = d.FloatOr("Intr", 4096)
	for _, v := range d.List("Clrs") {
		s, ok := v.(*Descriptor)
		if !ok {
			continue
		}
		st := ColorStop{Type: "user"}
		st.Location, _ = s.Int("Lctn")
		st.Midpoint, _ = s.Int("Mdpn")
		st.Color = parseColor(s.Desc("Clr "))
		switch t, _ := s.Enum("Type"); t {
		case "FrgC":
			st.Type = "foreground"
		case "BckC":
			st.Type = "background"
		}
		g.Stops = append(g.Stops, st)
	}
	for _, v := range d.List("Trns") {
		s, ok := v.(*Descriptor)
		if !ok {
			continue
		}
		st := OpacityStop{Opacity: s.FloatOr("Opct", 100) / 100}
		st.Location, _ = s.Int("Lctn")
		st.Midpoint, _ = s.Int("Mdpn")
		g.Transparency = append(g.Transparency, st)
	}
	return g
}

// GradientSettings places a gradient: a gradient fill layer, a gradient overlay
// or a stroke painted with a gradient
type GradientSettings struct {
	Gradient *Gradient
	// Angle is in degrees counterclockwise from the positive x axis
	Angle float64
	// Style is "linear", "radial", "angle", "reflected" or "diamond"
	Style string
	// Scale is in percent
	Scale   float64
	Reverse bool
	Dither  bool
	// Align aligns the gradient with the layer rather than the document
	Align bool
	// Offset is in percent of the layer's size
	Offset Point
	// Method is the interpolation method key, such as "Prcp" or "Lnr ", when present
	Method string
}

var gradientStyles = map[string]string{
	"Lnr ": "linear",
	"Rdl ": "radial",
	"Angl": "angle",
	"Rflc": "reflected",
	"Dmnd": "diamond",
}

func parseGradientSettings(d *Descriptor) *GradientSettings {
	gs := &GradientSettings{Style: "linear", Scale: 100, Align: true}
	gs.Gradient = parseGradient(d.Desc("Grad"))
	gs.Angle = d.FloatOr("Angl", 0)
	if t, ok := d.Enum("Type"); ok {
		if s, ok := gradientStyles[t]; ok {
			gs.Style = s
		}
	}
	gs.Scale = d.FloatOr("Scl ", 100)
	gs.Reverse = d.BoolOr("Rvrs", false)
	gs.Dither = d.BoolOr("Dthr", false)
	gs.Align = d.BoolOr("Algn", true)
	if o := d.Desc("Ofst"); o != nil {
		gs.Offset = Point{X: o.FloatOr("Hrzn", 0), Y: o.FloatOr("Vrtc", 0)}
	}
	gs.Method, _ = d.Enum("gs99")
	return gs
}

// Contour is a transfer curve from 0 to 255 on both axes, used by shadows, glows
// and bevels
type Contour struct {
	Name   string
	Points []ContourPoint
}

// ContourPoint is a point of a contour. Corner marks a sharp point
type ContourPoint struct {
	X, Y   float64
	Corner bool
}

func parseContour(d *Descriptor) *Contour {
	if d == nil {
		return nil
	}
	ct := &Contour{}
	ct.Name, _ = d.String("Nm  ")
	ct.Name = trimNul(ct.Name)
	for _, v := range d.List("Crv ") {
		p, ok := v.(*Descriptor)
		if !ok {
			continue
		}
		cp := ContourPoint{X: p.FloatOr("Hrzn", 0), Y: p.FloatOr("Vrtc", 0)}
		if c, ok := p.Bool("Cnty"); ok {
			cp.Corner = !c
		}
		ct.Points = append(ct.Points, cp)
	}
	return ct
}

// Pattern names a pattern by its name and identifier, which matches a pattern in
// the document's Patt, Pat2 or Pat3 block
type Pattern struct {
	Name string
	ID   string
}

func parsePattern(d *Descriptor) *Pattern {
	if d == nil {
		return nil
	}
	p := &Pattern{}
	p.Name, _ = d.String("Nm  ")
	p.Name = trimNul(p.Name)
	p.ID, _ = d.String("Idnt")
	p.ID = trimNul(p.ID)
	return p
}

func trimNul(s string) string {
	for len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return s
}
