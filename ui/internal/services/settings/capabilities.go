package settings

import (
	"fmt"

	"github.com/odevine/mimic/engine/version"
)

// The four gate levels a feature can sit at. Only live is usable. The other
// three render the control dimmed with a reason, and differ in what resolves
// them: waiting, upgrading the engine, or switching template
const (
	gateLive          = "live"
	gatePlanned       = "planned"
	gateNeedsEngine   = "needs-engine"
	gateNeedsTemplate = "needs-template"
)

// Gate is one feature's state as the frontend reads it
type Gate struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// feature is one row of the build-time table. minEngine, when set on a
// needs-engine feature, names the engine release that lifts the gate, so a
// build stamped with that engine or newer reports the feature live without
// anyone editing this table
type feature struct {
	key       string
	state     string
	reason    string
	minEngine string
}

// features is what this release implements. Removing a gate means changing a
// state here, never the markup, since the frontend renders what it is told
var features = []feature{
	{key: "flow.single", state: gateLive},
	{key: "flow.list", state: gateLive},
	{key: "flow.art", state: gateNeedsEngine, reason: "Needs engine support for art override. Tracked in #81"},
	{key: "flow.run", state: gateLive},

	{key: "single.printings", state: gateLive},
	{key: "single.symbols", state: gateLive},
	{key: "single.zoom", state: gateLive},
	{key: "single.compare", state: gatePlanned, reason: "Comparing against the Scryfall scan is planned. Tracked in #82"},
	{key: "single.artDrop", state: gateNeedsEngine, reason: "Needs engine support for art override. Tracked in #81"},
	{key: "single.dfc", state: gateLive},

	{key: "overrides", state: gateLive},
	{key: "overrides.global", state: gateLive},
	{key: "overrides.layers", state: gateNeedsEngine, reason: "Needs manifest introspection and layer forcing. Tracked in #83"},
	{key: "overrides.textboxes", state: gateNeedsEngine, reason: "Needs per-box overrides in RenderRequest. Tracked in #83"},
	{key: "overrides.assets", state: gatePlanned, reason: "Asset substitution is planned. Tracked in #84"},
	{key: "presets", state: gateLive},

	{key: "run.retryFailed", state: gateLive},
	{key: "list.inspector", state: gateLive},
	{key: "list.bulk", state: gateLive},
	{key: "palette", state: gatePlanned, reason: "The command palette is planned. Tracked in #82"},

	{key: "settings.resolution", state: gateLive},
	{key: "settings.bitDepth", state: gateNeedsEngine, reason: "Needs engine support for 16-bit output. Tracked in #86"},
	{key: "settings.bleed", state: gateNeedsEngine, reason: "Needs engine support for trimming the bleed. Tracked in #87"},
	{key: "settings.concurrency", state: gateLive},
	{key: "settings.imageFormat", state: gateLive},
	{key: "settings.pngCompression", state: gateLive},
	{key: "settings.layerCache", state: gateLive},
	{key: "settings.outputDir", state: gateLive},
	{key: "settings.filenameTemplate", state: gatePlanned, reason: "Runs name files like Sol Ring [C21-263], and templates for that are planned. Tracked in #82"},
	{key: "settings.runReport", state: gatePlanned, reason: "Every run writes its report, and turning that off is planned. Tracked in #82"},
	{key: "settings.scryfallRate", state: gatePlanned, reason: "Scryfall calls follow its published rate limits, and changing that is planned. Tracked in #82"},
	{key: "settings.localData", state: gateLive},
	{key: "settings.fonts", state: gateLive},
	{key: "settings.preferNonPromo", state: gatePlanned, reason: "Preferring non-promo printings is planned. Tracked in #82"},
	{key: "settings.language", state: gatePlanned, reason: "Choosing a card language is planned. Tracked in #82"},
	{key: "settings.filenameRegex", state: gatePlanned, reason: "Arrives with rendering from your art. Tracked in #81"},
	{key: "settings.density", state: gatePlanned, reason: "A denser layout is planned. Tracked in #82"},
	{key: "settings.restoreSession", state: gatePlanned, reason: "Restoring the last session is planned. Tracked in #82"},

	{key: "flow.editor", state: gateLive},
	{key: "editor.move", state: gateLive},
	{key: "editor.levers", state: gateLive},
	{key: "editor.json", state: gateLive},
	{key: "editor.assets", state: gateLive},
	{key: "editor.fonts", state: gateLive},
	{key: "editor.use", state: gateLive},
	{key: "editor.export", state: gateLive},
	{key: "editor.coverage", state: gateLive},

	{key: "templates.importProxyshop", state: gateLive},

	{key: "output.mpc", state: gateLive},
	{key: "output.pdf", state: gatePlanned, reason: "Print sheets are planned. Tracked in #85"},
}

// forced holds the keys a test build reports live whatever the table says. It
// is set once, before the app starts
var forced = map[string]bool{}

// KnownGate reports whether key is a feature the table lists
func KnownGate(key string) bool {
	for _, f := range features {
		if f.key == key {
			return true
		}
	}
	return false
}

// ForceLive makes each key report live, so a test can reach a feature that is
// not built yet. A key the table does not list is an error, which keeps a typo
// from passing for a gate that is simply closed. It replaces any earlier list
// and must be called before the app starts
func ForceLive(keys ...string) error {
	next := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !KnownGate(k) {
			return fmt.Errorf("no feature %q to force live", k)
		}
		next[k] = true
	}
	forced = next
	return nil
}

// engineSatisfies is a seam over version.Satisfies so a test can lift a
// needs-engine gate without stamping a binary
var engineSatisfies = version.Satisfies

// capabilities resolves the feature table into the map the frontend applies.
// A needs-engine feature whose minEngine the running engine satisfies is
// reported live. The active template will feed needs-template gates once a
// manifest can declare what it supports
func capabilities() map[string]Gate {
	out := make(map[string]Gate, len(features))
	for _, f := range features {
		g := Gate{State: f.state, Reason: f.reason}
		if f.state == gateNeedsEngine && f.minEngine != "" && engineSatisfies(f.minEngine) {
			g = Gate{State: gateLive}
		}
		if forced[f.key] {
			g = Gate{State: gateLive}
		}
		out[f.key] = g
	}
	return out
}
