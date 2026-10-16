package psd

import (
	"fmt"
)

// Effects is a layer's layer style, read from its lfx2 block. Each list holds the
// effects of one kind in file order, so a document that stacks several drop
// shadows lists them all. Opacities run from 0 to 1, sizes and distances are in
// pixels and angles in degrees, with the global light already applied where an
// effect uses it
type Effects struct {
	// Scale is the style's scale in percent, which Photoshop has already applied
	// to every size stored here
	Scale float64
	// Visible is the master switch over every effect of the layer
	Visible bool

	DropShadows      []*Shadow
	InnerShadows     []*Shadow
	OuterGlows       []*Glow
	InnerGlows       []*Glow
	Bevels           []*Bevel
	Satins           []*Satin
	ColorOverlays    []*ColorOverlay
	GradientOverlays []*GradientOverlay
	PatternOverlays  []*PatternOverlay
	Strokes          []*Stroke

	Desc *Descriptor
	list []Effect
}

// EffectKind names a kind of effect
type EffectKind int

// The effect kinds, in the order Photoshop draws them from bottom to top
const (
	EffectDropShadow EffectKind = iota
	EffectOuterGlow
	EffectBevelEmboss
	EffectSatin
	EffectInnerShadow
	EffectInnerGlow
	EffectColorOverlay
	EffectGradientOverlay
	EffectPatternOverlay
	EffectStroke
)

var effectKindNames = [...]string{
	EffectDropShadow:      "drop-shadow",
	EffectInnerShadow:     "inner-shadow",
	EffectOuterGlow:       "outer-glow",
	EffectInnerGlow:       "inner-glow",
	EffectBevelEmboss:     "bevel-emboss",
	EffectSatin:           "satin",
	EffectColorOverlay:    "color-overlay",
	EffectGradientOverlay: "gradient-overlay",
	EffectPatternOverlay:  "pattern-overlay",
	EffectStroke:          "stroke",
}

func (k EffectKind) String() string {
	if k >= 0 && int(k) < len(effectKindNames) {
		return effectKindNames[k]
	}
	return fmt.Sprintf("effect(%d)", int(k))
}

// Effect is any one effect
type Effect interface {
	Base() *EffectBase
}

// EffectBase holds what every effect has. An effect that is not Present was
// written with the style but never added to the layer, and Photoshop ignores it
type EffectBase struct {
	Kind         EffectKind
	Enabled      bool
	Present      bool
	ShowInDialog bool
	Blend        BlendMode
	Opacity      float64
	Desc         *Descriptor
}

// Base returns the common fields
func (b *EffectBase) Base() *EffectBase { return b }

// Active reports whether Photoshop draws the effect, leaving aside the layer's
// master switch
func (b *EffectBase) Active() bool { return b.Enabled && b.Present }

// Shadow is a drop shadow or an inner shadow
type Shadow struct {
	EffectBase
	Color          Color
	Angle          float64
	UseGlobalLight bool
	Distance       float64
	// Choke is the spread of a drop shadow or the choke of an inner shadow, in percent
	Choke     float64
	Size      float64
	Noise     float64
	AntiAlias bool
	Contour   *Contour
	// LayerKnocksOut hides a drop shadow under the layer's own fill
	LayerKnocksOut bool
}

// Glow is an outer or an inner glow
type Glow struct {
	EffectBase
	Color Color
	// Gradient is set when the glow is painted with a gradient rather than a color
	Gradient *Gradient
	// Choke is the spread of an outer glow or the choke of an inner glow, in percent
	Choke float64
	Size  float64
	// Range is the contour's range in percent
	Range float64
	// Jitter is in percent
	Jitter float64
	// Technique is "softer" or "precise"
	Technique string
	// Source is "center" or "edge" for an inner glow and empty otherwise
	Source    string
	Noise     float64
	AntiAlias bool
	Contour   *Contour
}

// Bevel is a bevel and emboss
type Bevel struct {
	EffectBase
	// Style is "outer", "inner", "emboss", "pillow" or "stroke"
	Style string
	// Technique is "smooth", "chisel-hard" or "chisel-soft"
	Technique string
	// Depth is in percent
	Depth float64
	// Up is true for a bevel raised toward the light
	Up             bool
	Size           float64
	Soften         float64
	Angle          float64
	Altitude       float64
	UseGlobalLight bool
	GlossContour   *Contour
	AntiAliasGloss bool
	HighlightBlend BlendMode
	HighlightColor Color
	// HighlightOpacity runs from 0 to 1
	HighlightOpacity float64
	ShadowBlend      BlendMode
	ShadowColor      Color
	// ShadowOpacity runs from 0 to 1
	ShadowOpacity float64
	// UseShape turns on the contour, which Contour and Range then shape
	UseShape         bool
	Contour          *Contour
	ContourAntiAlias bool
	Range            float64
	UseTexture       bool
	Texture          *Pattern
}

// Satin is a satin effect
type Satin struct {
	EffectBase
	Color     Color
	Angle     float64
	Distance  float64
	Size      float64
	Invert    bool
	AntiAlias bool
	Contour   *Contour
}

// ColorOverlay fills the layer's shape with a color
type ColorOverlay struct {
	EffectBase
	Color Color
}

// GradientOverlay fills the layer's shape with a gradient
type GradientOverlay struct {
	EffectBase
	GradientSettings
}

// PatternOverlay fills the layer's shape with a pattern
type PatternOverlay struct {
	EffectBase
	Pattern *Pattern
	Scale   float64
	Align   bool
	Phase   Point
}

// Stroke outlines the layer's shape
type Stroke struct {
	EffectBase
	Size float64
	// Position is "outside", "inside" or "center"
	Position string
	// FillType is "color", "gradient" or "pattern"
	FillType string
	Color    Color
	Gradient *GradientSettings
	Pattern  *Pattern
	// PatternScale is in percent
	PatternScale float64
	Overprint    bool
}

// List returns every effect in file order
func (e *Effects) List() []Effect { return e.list }

// effectClass maps a descriptor class to the kind it describes
var effectClass = map[string]EffectKind{
	"DrSh":        EffectDropShadow,
	"IrSh":        EffectInnerShadow,
	"OrGl":        EffectOuterGlow,
	"IrGl":        EffectInnerGlow,
	"ebbl":        EffectBevelEmboss,
	"ChFX":        EffectSatin,
	"SoFi":        EffectColorOverlay,
	"GrFl":        EffectGradientOverlay,
	"patternFill": EffectPatternOverlay,
	"FrFX":        EffectStroke,
}

// effectKeyKind maps the item keys of an effects descriptor, both the single and
// the multi forms, to their kinds
var effectKeyKind = map[string]EffectKind{
	"DrSh": EffectDropShadow, "dropShadowMulti": EffectDropShadow,
	"IrSh": EffectInnerShadow, "innerShadowMulti": EffectInnerShadow,
	"OrGl": EffectOuterGlow,
	"IrGl": EffectInnerGlow,
	"ebbl": EffectBevelEmboss,
	"ChFX": EffectSatin,
	"SoFi": EffectColorOverlay, "solidFillMulti": EffectColorOverlay,
	"GrFl": EffectGradientOverlay, "gradientFillMulti": EffectGradientOverlay,
	"patternFill": EffectPatternOverlay,
	"FrFX":        EffectStroke, "frameFXMulti": EffectStroke,
}

// parseEffects reads an lfx2 or lmfx block: an effects version, a descriptor
// version and the descriptor
func parseEffects(b []byte, globalAngle, globalAltitude float64) (*Effects, error) {
	c := newCursor(b)
	c.u32()
	c.u32()
	if c.err != nil {
		return nil, fmt.Errorf("psd: effects: %w", c.err)
	}
	d := readDescriptor(c, 0)
	if c.err != nil {
		return nil, fmt.Errorf("psd: effects: %w", c.err)
	}
	return effectsFromDescriptor(d, globalAngle, globalAltitude), nil
}

func effectsFromDescriptor(d *Descriptor, globalAngle, globalAltitude float64) *Effects {
	fx := &Effects{Desc: d, Scale: d.FloatOr("Scl ", 100), Visible: d.BoolOr("masterFXSwitch", true)}
	multi := map[EffectKind]bool{}
	for _, it := range d.Items {
		if _, isList := it.Value.([]any); isList {
			if k, ok := effectKeyKind[it.Key]; ok {
				multi[k] = true
			}
		}
	}
	for _, it := range d.Items {
		var entries []*Descriptor
		switch v := it.Value.(type) {
		case *Descriptor:
			entries = []*Descriptor{v}
		case []any:
			for _, e := range v {
				if ed, ok := e.(*Descriptor); ok {
					entries = append(entries, ed)
				}
			}
		default:
			continue
		}
		for _, ed := range entries {
			kind, ok := effectClass[ed.Class]
			if !ok {
				kind, ok = effectKeyKind[it.Key]
				if !ok {
					continue
				}
			}
			if _, isList := it.Value.([]any); !isList && multi[kind] {
				continue
			}
			fx.add(kind, ed, globalAngle, globalAltitude)
		}
	}
	return fx
}

func effectBase(kind EffectKind, d *Descriptor) EffectBase {
	b := EffectBase{Kind: kind, Desc: d}
	b.Enabled = d.BoolOr("enab", true)
	b.Present = d.BoolOr("present", true)
	b.ShowInDialog = d.BoolOr("showInDialog", false)
	if m, ok := d.Enum("Md  "); ok {
		b.Blend = blendFromEnum(m)
	}
	b.Opacity = d.FloatOr("Opct", 100) / 100
	return b
}

var glowTechniques = map[string]string{"SfBL": "softer", "PrBL": "precise"}

var bevelStyles = map[string]string{
	"OtrB":         "outer",
	"InrB":         "inner",
	"Embs":         "emboss",
	"PlEb":         "pillow",
	"strokeEmboss": "stroke",
}

var bevelTechniques = map[string]string{
	"SfBL": "smooth",
	"PrBL": "chisel-hard",
	"Slmt": "chisel-soft",
}

func (fx *Effects) add(kind EffectKind, d *Descriptor, ga, galt float64) {
	base := effectBase(kind, d)
	var e Effect
	switch kind {
	case EffectDropShadow, EffectInnerShadow:
		s := &Shadow{EffectBase: base}
		s.Color = parseColor(d.Desc("Clr "))
		s.UseGlobalLight = d.BoolOr("uglg", false)
		s.Angle = d.FloatOr("lagl", 0)
		if s.UseGlobalLight {
			s.Angle = ga
		}
		s.Distance = d.FloatOr("Dstn", 0)
		s.Choke = d.FloatOr("Ckmt", 0)
		s.Size = d.FloatOr("blur", 0)
		s.Noise = d.FloatOr("Nose", 0)
		s.AntiAlias = d.BoolOr("AntA", false)
		s.Contour = parseContour(d.Desc("TrnS"))
		s.LayerKnocksOut = d.BoolOr("layerConceals", false)
		if kind == EffectDropShadow {
			fx.DropShadows = append(fx.DropShadows, s)
		} else {
			fx.InnerShadows = append(fx.InnerShadows, s)
		}
		e = s
	case EffectOuterGlow, EffectInnerGlow:
		g := &Glow{EffectBase: base}
		g.Color = parseColor(d.Desc("Clr "))
		g.Gradient = parseGradient(d.Desc("Grad"))
		g.Choke = d.FloatOr("Ckmt", 0)
		g.Size = d.FloatOr("blur", 0)
		g.Range = d.FloatOr("Inpr", 50)
		g.Jitter = d.FloatOr("ShdN", 0)
		g.Technique = "softer"
		if t, ok := d.Enum("GlwT"); ok {
			if n, ok := glowTechniques[t]; ok {
				g.Technique = n
			}
		}
		if kind == EffectInnerGlow {
			g.Source = "edge"
			if s, ok := d.Enum("glwS"); ok && s == "SrcC" {
				g.Source = "center"
			}
		}
		g.Noise = d.FloatOr("Nose", 0)
		g.AntiAlias = d.BoolOr("AntA", false)
		g.Contour = parseContour(d.Desc("TrnS"))
		if kind == EffectOuterGlow {
			fx.OuterGlows = append(fx.OuterGlows, g)
		} else {
			fx.InnerGlows = append(fx.InnerGlows, g)
		}
		e = g
	case EffectBevelEmboss:
		b := &Bevel{EffectBase: base}
		b.Style = "inner"
		if s, ok := d.Enum("bvlS"); ok {
			if n, ok := bevelStyles[s]; ok {
				b.Style = n
			}
		}
		b.Technique = "smooth"
		if t, ok := d.Enum("bvlT"); ok {
			if n, ok := bevelTechniques[t]; ok {
				b.Technique = n
			}
		}
		b.Depth = d.FloatOr("srgR", 100)
		dir, _ := d.Enum("bvlD")
		b.Up = dir != "Out "
		b.Size = d.FloatOr("blur", 0)
		b.Soften = d.FloatOr("Sftn", 0)
		b.UseGlobalLight = d.BoolOr("uglg", false)
		b.Angle = d.FloatOr("lagl", 0)
		b.Altitude = d.FloatOr("Lald", 0)
		if b.UseGlobalLight {
			b.Angle, b.Altitude = ga, galt
		}
		b.GlossContour = parseContour(d.Desc("TrnS"))
		b.AntiAliasGloss = d.BoolOr("antialiasGloss", false)
		if m, ok := d.Enum("hglM"); ok {
			b.HighlightBlend = blendFromEnum(m)
		}
		b.HighlightColor = parseColor(d.Desc("hglC"))
		b.HighlightOpacity = d.FloatOr("hglO", 75) / 100
		if m, ok := d.Enum("sdwM"); ok {
			b.ShadowBlend = blendFromEnum(m)
		}
		b.ShadowColor = parseColor(d.Desc("sdwC"))
		b.ShadowOpacity = d.FloatOr("sdwO", 75) / 100
		b.UseShape = d.BoolOr("useShape", false)
		b.Contour = parseContour(d.Desc("MpgS"))
		b.ContourAntiAlias = d.BoolOr("AntA", false)
		b.Range = d.FloatOr("Inpr", 50)
		b.UseTexture = d.BoolOr("useTexture", false)
		b.Texture = parsePattern(d.Desc("Ptrn"))
		fx.Bevels = append(fx.Bevels, b)
		e = b
	case EffectSatin:
		s := &Satin{EffectBase: base}
		s.Color = parseColor(d.Desc("Clr "))
		s.Angle = d.FloatOr("lagl", 0)
		s.Distance = d.FloatOr("Dstn", 0)
		s.Size = d.FloatOr("blur", 0)
		s.Invert = d.BoolOr("Invr", false)
		s.AntiAlias = d.BoolOr("AntA", false)
		s.Contour = parseContour(d.Desc("MpgS"))
		fx.Satins = append(fx.Satins, s)
		e = s
	case EffectColorOverlay:
		o := &ColorOverlay{EffectBase: base, Color: parseColor(d.Desc("Clr "))}
		fx.ColorOverlays = append(fx.ColorOverlays, o)
		e = o
	case EffectGradientOverlay:
		o := &GradientOverlay{EffectBase: base, GradientSettings: *parseGradientSettings(d)}
		fx.GradientOverlays = append(fx.GradientOverlays, o)
		e = o
	case EffectPatternOverlay:
		o := &PatternOverlay{EffectBase: base}
		o.Pattern = parsePattern(d.Desc("Ptrn"))
		o.Scale = d.FloatOr("Scl ", 100)
		o.Align = d.BoolOr("Algn", true)
		if p := d.Desc("phase"); p != nil {
			o.Phase = Point{X: p.FloatOr("Hrzn", 0), Y: p.FloatOr("Vrtc", 0)}
		}
		fx.PatternOverlays = append(fx.PatternOverlays, o)
		e = o
	case EffectStroke:
		s := &Stroke{EffectBase: base}
		s.Size = d.FloatOr("Sz  ", 0)
		s.Position = "outside"
		switch p, _ := d.Enum("Styl"); p {
		case "InsF":
			s.Position = "inside"
		case "CtrF":
			s.Position = "center"
		}
		s.FillType = "color"
		switch t, _ := d.Enum("PntT"); t {
		case "GrFl":
			s.FillType = "gradient"
			s.Gradient = parseGradientSettings(d)
		case "Ptrn":
			s.FillType = "pattern"
			s.Pattern = parsePattern(d.Desc("Ptrn"))
			s.PatternScale = d.FloatOr("Scl ", 100)
		}
		s.Color = parseColor(d.Desc("Clr "))
		s.Overprint = d.BoolOr("overprint", false)
		fx.Strokes = append(fx.Strokes, s)
		e = s
	}
	fx.list = append(fx.list, e)
}

// legacyBevelStyles maps the style byte of an lrFX bevel
var legacyBevelStyles = map[uint8]string{1: "outer", 2: "inner", 3: "emboss", 4: "pillow", 5: "stroke"}

// ParseLegacyEffects reads an lrFX block, the layer style format of Photoshop
// 5, which later versions still write beside lfx2. It holds drop and inner
// shadows, glows, a bevel and a color overlay. Sizes, angles and distances are
// 16.16 fixed point, opacities run from 0 to 255 and colors are 16 bit. The
// format has no altitude, so a bevel takes the global altitude when it uses the
// global light and 30 otherwise
func ParseLegacyEffects(b []byte, globalAngle, globalAltitude float64) (*Effects, error) {
	c := newCursor(b)
	c.u16()
	n := int(c.u16())
	fx := &Effects{Scale: 100, Visible: true}
	for i := 0; i < n && c.err == nil; i++ {
		if sig := c.key4(); sig != "8BIM" && c.err == nil {
			return nil, fmt.Errorf("psd: legacy effects: signature %q", sig)
		}
		key := c.key4()
		size := int(c.u32())
		body := c.take(size)
		if c.err != nil {
			break
		}
		if err := fx.addLegacy(key, newCursor(body), globalAngle, globalAltitude); err != nil {
			return nil, fmt.Errorf("psd: legacy effects %q: %w", key, err)
		}
	}
	if c.err != nil {
		return nil, fmt.Errorf("psd: legacy effects: %w", c.err)
	}
	return fx, nil
}

// fixed reads a 16.16 fixed point number
func (c *cursor) fixed() float64 { return float64(c.i32()) / 65536 }

// legacyColor reads a color space and four 16 bit components. Only RGB, the
// space Photoshop writes here, converts to channels
func (c *cursor) legacyColor() Color {
	space := c.u16()
	var v [4]uint16
	for i := range v {
		v[i] = c.u16()
	}
	if space != 0 {
		return Color{Model: fmt.Sprintf("space%d", space)}
	}
	return Color{Model: "rgb", R: float64(v[0]) / 257, G: float64(v[1]) / 257, B: float64(v[2]) / 257}
}

// legacyBlend reads a signature and a layer blend mode key
func (c *cursor) legacyBlend() BlendMode {
	c.key4()
	m, _ := blendFromKey(c.key4())
	return m
}

func (fx *Effects) addLegacy(key string, c *cursor, ga, galt float64) error {
	base := func(kind EffectKind) EffectBase {
		return EffectBase{Kind: kind, Present: true}
	}
	var e Effect
	switch key {
	case "cmnS":
		c.u32()
		fx.Visible = c.u8() != 0
		return c.err
	case "dsdw", "isdw":
		kind := EffectDropShadow
		if key == "isdw" {
			kind = EffectInnerShadow
		}
		s := &Shadow{EffectBase: base(kind)}
		c.u32()
		s.Size = c.fixed()
		s.Choke = c.fixed()
		s.Angle = c.fixed()
		s.Distance = c.fixed()
		s.Color = c.legacyColor()
		s.Blend = c.legacyBlend()
		s.Enabled = c.u8() != 0
		s.UseGlobalLight = c.u8() != 0
		s.Opacity = float64(c.u8()) / 255
		if s.UseGlobalLight {
			s.Angle = ga
		}
		if kind == EffectDropShadow {
			s.LayerKnocksOut = true
			fx.DropShadows = append(fx.DropShadows, s)
		} else {
			fx.InnerShadows = append(fx.InnerShadows, s)
		}
		e = s
	case "oglw", "iglw":
		kind := EffectOuterGlow
		if key == "iglw" {
			kind = EffectInnerGlow
		}
		g := &Glow{EffectBase: base(kind), Range: 50, Technique: "softer"}
		version := c.u32()
		g.Size = c.fixed()
		g.Choke = c.fixed()
		g.Color = c.legacyColor()
		g.Blend = c.legacyBlend()
		g.Enabled = c.u8() != 0
		g.Opacity = float64(c.u8()) / 255
		if kind == EffectInnerGlow {
			g.Source = "edge"
			if version >= 2 && c.u8() == 0 {
				g.Source = "center"
			}
			fx.InnerGlows = append(fx.InnerGlows, g)
		} else {
			fx.OuterGlows = append(fx.OuterGlows, g)
		}
		e = g
	case "bevl":
		b := &Bevel{EffectBase: base(EffectBevelEmboss), Technique: "smooth", Depth: 100, Range: 50}
		b.Opacity = 1
		c.u32()
		b.Angle = c.fixed()
		c.fixed()
		b.Size = c.fixed()
		b.HighlightBlend = c.legacyBlend()
		b.ShadowBlend = c.legacyBlend()
		b.HighlightColor = c.legacyColor()
		b.ShadowColor = c.legacyColor()
		b.Style = legacyBevelStyles[c.u8()]
		if b.Style == "" {
			b.Style = "inner"
		}
		b.HighlightOpacity = float64(c.u8()) / 255
		b.ShadowOpacity = float64(c.u8()) / 255
		b.Enabled = c.u8() != 0
		b.UseGlobalLight = c.u8() != 0
		b.Up = c.u8() == 0
		b.Altitude = 30
		if b.UseGlobalLight {
			b.Angle, b.Altitude = ga, galt
		}
		fx.Bevels = append(fx.Bevels, b)
		e = b
	case "sofi":
		o := &ColorOverlay{EffectBase: base(EffectColorOverlay)}
		c.u32()
		o.Blend = c.legacyBlend()
		o.Color = c.legacyColor()
		o.Opacity = float64(c.u8()) / 255
		o.Enabled = c.u8() != 0
		fx.ColorOverlays = append(fx.ColorOverlays, o)
		e = o
	default:
		return nil
	}
	if c.err != nil {
		return c.err
	}
	fx.list = append(fx.list, e)
	return nil
}
