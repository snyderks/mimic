package proxyshop

// darkBorderless is BorderlessVectorTemplate's dark_color_map, which colors
// the boxes of a borderless card
var darkBorderless = map[string]rgbColor{
	"w": {0x95, 0x86, 0x76}, "u": {0x04, 0x54, 0x82}, "b": {0x28, 0x25, 0x23}, "r": {0x93, 0x36, 0x2a}, "g": {0x13, 0x4f, 0x23},
	"gold": {0x9a, 0x88, 0x3f}, "land": {0xa7, 0x9c, 0x8e}, "artifact": {0x48, 0x55, 0x5c}, "colorless": {0x74, 0x72, 0x6b},
	"vehicle": {0x4c, 0x33, 0x14},
}

// crownBorderless is BorderlessVectorTemplate's crown_color_map
var crownBorderless = map[string]rgbColor{
	"w": {0xf8, 0xf4, 0xf0}, "u": {0x00, 0x6d, 0xae}, "b": {0x39, 0x34, 0x31}, "r": {0xde, 0x3c, 0x23}, "g": {0x00, 0x6d, 0x42},
	"gold": {0xef, 0xd1, 0x6b}, "land": {0xa5, 0x96, 0x84}, "artifact": {0xb5, 0xc5, 0xcd}, "colorless": {0xd6, 0xd6, 0xdc},
}

// withColors is a copy of base with some colors replaced
func withColors(base map[string]rgbColor, over map[string]rgbColor) map[string]rgbColor {
	out := make(map[string]rgbColor, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

var (
	ubPinlines = map[string]rgbColor{
		"w": {246, 247, 241}, "u": {0, 131, 193}, "b": {44, 40, 33}, "r": {237, 66, 31}, "g": {5, 129, 64},
		"gold": {239, 209, 107}, "land": {165, 150, 132}, "artifact": {227, 228, 230}, "colorless": {227, 228, 230}, "vehicle": {77, 45, 5},
	}
	lotrPinlines = map[string]rgbColor{
		"w": {230, 220, 185}, "u": {72, 142, 191}, "b": {126, 128, 127}, "r": {217, 94, 76}, "g": {97, 143, 102},
		"gold": {246, 213, 125}, "land": {181, 162, 149}, "artifact": {210, 219, 227}, "colorless": {210, 219, 227}, "vehicle": {77, 45, 5},
	}
)

// templateColors are the color maps a template's own code fills groups with,
// by the PSD's name and the group's slug. Groups not listed take Proxyshop's
// defaults
var templateColors = map[string]map[string]map[string]rgbColor{
	"borderless-vector": {
		"legendary_crown":  crownBorderless,
		"name_title_boxes": darkBorderless,
		"pt_box":           darkBorderless,
		"textbox":          darkBorderless,
	},
	"etched": {
		"pinlines": withColors(pinlineColors, map[string]rgbColor{"w": {252, 254, 255}, "land": {136, 120, 98}, "artifact": {194, 210, 221}, "colorless": {194, 210, 221}}),
	},
	"classic-modern": {
		"pinlines":        withColors(pinlineColors, map[string]rgbColor{"land": {0x60, 0x4a, 0x33}}),
		"legendary_crown": withColors(pinlineColors, map[string]rgbColor{"land": {0x60, 0x4a, 0x33}}),
	},
	"universes-beyond":  {"pinlines": ubPinlines, "legendary_crown": ubPinlines},
	"vertical-mode":     {"pinlines": ubPinlines, "legendary_crown": ubPinlines},
	"lord-of-the-rings": {"pinlines": lotrPinlines},
}
