package psd

// Adjustment is an adjustment layer's settings. Type names the adjustment and
// the field for it is set where this package reads that kind, and Raw always
// holds the block
type Adjustment struct {
	// Type is one of "levels", "curves", "huesaturation", "selectivecolor",
	// "brightnesscontrast", "colorbalance", "blackandwhite", "photofilter",
	// "channelmixer", "colorlookup", "invert", "posterize", "threshold",
	// "gradientmap", "vibrance" or "exposure"
	Type string
	Key  string
	Raw  []byte
	// Desc is set for the adjustments stored as descriptors
	Desc *Descriptor

	Levels             []LevelsRecord
	Curves             []CurvesChannel
	HueSaturation      *HueSaturation
	SelectiveColor     *SelectiveColor
	BrightnessContrast *BrightnessContrast
	ColorBalance       *ColorBalance
	// Posterize holds the level count and Threshold the threshold level
	Posterize int
	Threshold int
}

// LevelsRecord is the levels of one channel, the first record being the
// composite. Gamma is a ratio, 1 for no change
type LevelsRecord struct {
	InputFloor, InputCeiling   int
	OutputFloor, OutputCeiling int
	Gamma                      float64
}

// CurvesChannel is the curve of one channel, 0 for the composite then the
// color channels in order. Points map input to output from 0 to 255
type CurvesChannel struct {
	Channel int
	Points  []Point
}

// HueSaturation is a hue and saturation adjustment. Ranges are the six color
// ranges, reds through magentas
type HueSaturation struct {
	Colorize                        bool
	ColorizeHue, ColorizeSaturation int
	ColorizeLightness               int
	Hue, Saturation, Lightness      int
	Ranges                          [6]HueRange
}

// HueRange is one color range: the four hue angles bounding it and its change
type HueRange struct {
	Bounds                     [4]int
	Hue, Saturation, Lightness int
}

// SelectiveColor is a selective color adjustment. Colors holds cyan, magenta,
// yellow and black in percent for reds, yellows, greens, cyans, blues,
// magentas, whites, neutrals and blacks
type SelectiveColor struct {
	Absolute bool
	Colors   [9][4]int
}

// BrightnessContrast is a brightness and contrast adjustment
type BrightnessContrast struct {
	Brightness, Contrast int
	Mean                 int
	LabOnly              bool
	Legacy               bool
}

// ColorBalance is a color balance adjustment. Each tone holds the cyan to red,
// magenta to green and yellow to blue shifts
type ColorBalance struct {
	Shadows, Midtones, Highlights [3]int
	PreserveLuminosity            bool
}

// adjustmentKeys lists the adjustment blocks in the order Photoshop's readers
// look for them, with the names of their kinds
var adjustmentKeys = []struct{ key, name string }{
	{"CgEd", "brightnesscontrast"},
	{"curv", "curves"},
	{"expA", "exposure"},
	{"levl", "levels"},
	{"vibA", "vibrance"},
	{"hue2", "huesaturation"},
	{"hue ", "huesaturation"},
	{"blnc", "colorbalance"},
	{"blwh", "blackandwhite"},
	{"phfl", "photofilter"},
	{"mixr", "channelmixer"},
	{"clrL", "colorlookup"},
	{"nvrt", "invert"},
	{"post", "posterize"},
	{"thrs", "threshold"},
	{"selc", "selectivecolor"},
	{"grdm", "gradientmap"},
	{"brit", "brightnesscontrast"},
}

func parseAdjustment(key, name string, b []byte) *Adjustment {
	a := &Adjustment{Type: name, Key: key, Raw: b}
	c := newCursor(b)
	switch key {
	case "levl":
		a.Levels = parseLevels(c)
	case "curv":
		a.Curves = parseCurves(c)
	case "hue2", "hue ":
		h := &HueSaturation{}
		c.u16()
		h.Colorize = c.u8() != 0
		c.u8()
		h.ColorizeHue, h.ColorizeSaturation, h.ColorizeLightness = int(c.i16()), int(c.i16()), int(c.i16())
		h.Hue, h.Saturation, h.Lightness = int(c.i16()), int(c.i16()), int(c.i16())
		for i := range h.Ranges {
			for j := range h.Ranges[i].Bounds {
				h.Ranges[i].Bounds[j] = int(c.i16())
			}
			h.Ranges[i].Hue, h.Ranges[i].Saturation, h.Ranges[i].Lightness = int(c.i16()), int(c.i16()), int(c.i16())
		}
		if c.err == nil {
			a.HueSaturation = h
		}
	case "selc":
		s := &SelectiveColor{}
		c.u16()
		s.Absolute = c.u16() == 1
		c.skip(8)
		for i := range s.Colors {
			for j := range s.Colors[i] {
				s.Colors[i][j] = int(c.i16())
			}
		}
		if c.err == nil {
			a.SelectiveColor = s
		}
	case "brit":
		bc := &BrightnessContrast{Legacy: true}
		bc.Brightness, bc.Contrast, bc.Mean = int(c.i16()), int(c.i16()), int(c.i16())
		bc.LabOnly = c.u8() != 0
		if c.err == nil {
			a.BrightnessContrast = bc
		}
	case "CgEd":
		c.u32()
		d := readDescriptor(c, 0)
		if c.err == nil {
			a.Desc = d
			a.BrightnessContrast = &BrightnessContrast{
				Brightness: int(d.FloatOr("Brgh", 0)),
				Contrast:   int(d.FloatOr("Cntr", 0)),
				Mean:       int(d.FloatOr("means", 127)),
				LabOnly:    d.BoolOr("Lab ", false),
				Legacy:     d.BoolOr("useLegacy", false),
			}
		}
	case "blnc":
		cb := &ColorBalance{}
		for i := range cb.Shadows {
			cb.Shadows[i] = int(c.i16())
		}
		for i := range cb.Midtones {
			cb.Midtones[i] = int(c.i16())
		}
		for i := range cb.Highlights {
			cb.Highlights[i] = int(c.i16())
		}
		cb.PreserveLuminosity = c.u8() != 0
		if c.err == nil {
			a.ColorBalance = cb
		}
	case "post":
		a.Posterize = int(c.u16())
	case "thrs":
		a.Threshold = int(c.u16())
	case "blwh", "vibA":
		c.u32()
		if d := readDescriptor(c, 0); c.err == nil {
			a.Desc = d
		}
	case "clrL":
		c.u16()
		c.u32()
		if d := readDescriptor(c, 0); c.err == nil {
			a.Desc = d
		}
	}
	return a
}

// parseLevels reads the 29 records of a levels block and the further records of
// its Lvls extension, which holds the count of all records
func parseLevels(c *cursor) []LevelsRecord {
	c.u16()
	var out []LevelsRecord
	read := func(n int) {
		for range n {
			if c.remaining() < 10 {
				return
			}
			out = append(out, LevelsRecord{
				InputFloor:    int(c.u16()),
				InputCeiling:  int(c.u16()),
				OutputFloor:   int(c.u16()),
				OutputCeiling: int(c.u16()),
				Gamma:         float64(c.u16()) / 100,
			})
		}
	}
	read(29)
	if len(out) == 29 && c.remaining() >= 8 && string(c.take(4)) == "Lvls" {
		c.u16()
		read(int(c.u16()) - 29)
	}
	return out
}

// parseCurves reads the binary curves record and the Crv extension after it,
// which takes precedence when present
func parseCurves(c *cursor) []CurvesChannel {
	isMap := c.u8() != 0
	version := c.u16()
	bits := c.u32()
	if c.err != nil || isMap || (version != 1 && version != 4) {
		return nil
	}
	var channels []int
	if version == 1 {
		for i := range 32 {
			if bits&(1<<i) != 0 {
				channels = append(channels, i)
			}
		}
	} else {
		for i := range int(bits) {
			channels = append(channels, i)
		}
	}
	var out []CurvesChannel
	for _, ch := range channels {
		n := int(c.u16())
		cc := CurvesChannel{Channel: ch}
		for range n {
			y := float64(c.u16())
			x := float64(c.u16())
			cc.Points = append(cc.Points, Point{X: x, Y: y})
		}
		if c.err != nil {
			return out
		}
		out = append(out, cc)
	}
	if version == 1 && c.remaining() >= 10 && string(c.take(4)) == "Crv " {
		c.u16()
		n := int(c.u32())
		var ext []CurvesChannel
		for range n {
			cc := CurvesChannel{Channel: int(c.u16())}
			pts := int(c.u16())
			for range pts {
				y := float64(c.u16())
				x := float64(c.u16())
				cc.Points = append(cc.Points, Point{X: x, Y: y})
			}
			if c.err != nil {
				return out
			}
			ext = append(ext, cc)
		}
		return ext
	}
	return out
}
