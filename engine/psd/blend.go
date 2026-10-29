package psd

// BlendMode is how a layer or an effect mixes with what is below it
type BlendMode int

// The blend modes Photoshop offers, in the order of its menu
const (
	BlendNormal BlendMode = iota
	BlendPassThrough
	BlendDissolve
	BlendDarken
	BlendMultiply
	BlendColorBurn
	BlendLinearBurn
	BlendDarkerColor
	BlendLighten
	BlendScreen
	BlendColorDodge
	BlendLinearDodge
	BlendLighterColor
	BlendOverlay
	BlendSoftLight
	BlendHardLight
	BlendVividLight
	BlendLinearLight
	BlendPinLight
	BlendHardMix
	BlendDifference
	BlendExclusion
	BlendSubtract
	BlendDivide
	BlendHue
	BlendSaturation
	BlendColor
	BlendLuminosity
)

var blendNames = [...]string{
	BlendNormal:       "normal",
	BlendPassThrough:  "pass-through",
	BlendDissolve:     "dissolve",
	BlendDarken:       "darken",
	BlendMultiply:     "multiply",
	BlendColorBurn:    "color-burn",
	BlendLinearBurn:   "linear-burn",
	BlendDarkerColor:  "darker-color",
	BlendLighten:      "lighten",
	BlendScreen:       "screen",
	BlendColorDodge:   "color-dodge",
	BlendLinearDodge:  "linear-dodge",
	BlendLighterColor: "lighter-color",
	BlendOverlay:      "overlay",
	BlendSoftLight:    "soft-light",
	BlendHardLight:    "hard-light",
	BlendVividLight:   "vivid-light",
	BlendLinearLight:  "linear-light",
	BlendPinLight:     "pin-light",
	BlendHardMix:      "hard-mix",
	BlendDifference:   "difference",
	BlendExclusion:    "exclusion",
	BlendSubtract:     "subtract",
	BlendDivide:       "divide",
	BlendHue:          "hue",
	BlendSaturation:   "saturation",
	BlendColor:        "color",
	BlendLuminosity:   "luminosity",
}

// String names the mode in lower case words joined by hyphens, as in "color-burn"
func (m BlendMode) String() string {
	if m >= 0 && int(m) < len(blendNames) {
		return blendNames[m]
	}
	return "normal"
}

// MarshalText writes the mode's name
func (m BlendMode) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

// layerBlendKeys maps the four character keys of layer records
var layerBlendKeys = map[string]BlendMode{
	"pass": BlendPassThrough,
	"norm": BlendNormal,
	"diss": BlendDissolve,
	"dark": BlendDarken,
	"mul ": BlendMultiply,
	"idiv": BlendColorBurn,
	"lbrn": BlendLinearBurn,
	"dkCl": BlendDarkerColor,
	"lite": BlendLighten,
	"scrn": BlendScreen,
	"div ": BlendColorDodge,
	"lddg": BlendLinearDodge,
	"lgCl": BlendLighterColor,
	"over": BlendOverlay,
	"sLit": BlendSoftLight,
	"hLit": BlendHardLight,
	"vLit": BlendVividLight,
	"lLit": BlendLinearLight,
	"pLit": BlendPinLight,
	"hMix": BlendHardMix,
	"diff": BlendDifference,
	"smud": BlendExclusion,
	"fsub": BlendSubtract,
	"fdiv": BlendDivide,
	"hue ": BlendHue,
	"sat ": BlendSaturation,
	"colr": BlendColor,
	"lum ": BlendLuminosity,
}

// descriptorBlendKeys maps the enum values descriptors use, which are either
// terminology codes or long names
var descriptorBlendKeys = map[string]BlendMode{
	"Nrml": BlendNormal, "normal": BlendNormal,
	"passThrough": BlendPassThrough,
	"Dslv":        BlendDissolve, "dissolve": BlendDissolve,
	"Drkn": BlendDarken, "darken": BlendDarken,
	"Mltp": BlendMultiply, "multiply": BlendMultiply,
	"CBrn": BlendColorBurn, "colorBurn": BlendColorBurn,
	"linearBurn":  BlendLinearBurn,
	"darkerColor": BlendDarkerColor,
	"Lghn":        BlendLighten, "lighten": BlendLighten,
	"Scrn": BlendScreen, "screen": BlendScreen,
	"CDdg": BlendColorDodge, "colorDodge": BlendColorDodge,
	"linearDodge":  BlendLinearDodge,
	"lighterColor": BlendLighterColor,
	"Ovrl":         BlendOverlay, "overlay": BlendOverlay,
	"SftL": BlendSoftLight, "softLight": BlendSoftLight,
	"HrdL": BlendHardLight, "hardLight": BlendHardLight,
	"vividLight":  BlendVividLight,
	"linearLight": BlendLinearLight,
	"pinLight":    BlendPinLight,
	"hardMix":     BlendHardMix,
	"Dfrn":        BlendDifference, "difference": BlendDifference,
	"Xclu": BlendExclusion, "exclusion": BlendExclusion,
	"Sbtr": BlendSubtract, "blendSubtraction": BlendSubtract,
	"blendDivide": BlendDivide,
	"H   ":        BlendHue, "hue": BlendHue,
	"Strt": BlendSaturation, "saturation": BlendSaturation,
	"Clr ": BlendColor, "color": BlendColor,
	"Lmns": BlendLuminosity, "luminosity": BlendLuminosity,
}

// blendFromKey reads a layer record key, taking normal for one it does not know
func blendFromKey(k string) (BlendMode, bool) {
	m, ok := layerBlendKeys[k]
	return m, ok
}

// blendFromEnum reads a descriptor blend mode value, taking normal for one it
// does not know
func blendFromEnum(v string) BlendMode {
	return descriptorBlendKeys[v]
}
