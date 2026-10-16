package studio

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/odevine/mimic/engine/card"
	"github.com/odevine/mimic/engine/frame"
	"github.com/odevine/mimic/engine/manifest"
	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/cardlist"
	"github.com/odevine/mimic/ui/internal/catalog"
	"github.com/odevine/mimic/ui/internal/draftedit"
	"github.com/odevine/mimic/ui/internal/pipeline"
	"github.com/odevine/mimic/ui/internal/workspace"
)

func describeFor(t *testing.T, svc *Service, id, preset string) DescribeView {
	t.Helper()
	v, err := svc.Describe(DescribeRequest{ID: id, Card: presetCard(t, preset)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func item(t *testing.T, v DescribeView, kind, key string) ItemView {
	t.Helper()
	for _, it := range v.Items {
		if it.Kind == kind && it.Key == key {
			return it
		}
	}
	t.Fatalf("no %s %q in %+v", kind, key, v.Items)
	return ItemView{}
}

func TestDescribeListsWhatTheDraftDrawsForTheTestCard(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	v := describeFor(t, svc, sum.ID, "monocolored")

	if v.Layout != "normal" || v.Authored != (SizeView{272, 370}) || v.Delivered != v.Authored || !v.Editable || v.Reason != "" {
		t.Fatalf("view = %+v", v)
	}
	if bg := item(t, v, "layer", "background"); !bg.Drawn || bg.Delivered != (RectView{0, 0, 272, 370}) {
		t.Errorf("background = %+v", bg)
	}
	// The title is set on its baseline, so it is shown where its text is, above the
	// box's top, and the oracle text fills its box
	if title := item(t, v, "text", "title"); !title.Drawn || !title.Ink || title.Delivered.Width == 0 || title.Authored != title.Delivered {
		t.Errorf("title = %+v", title)
	}
	if oracle := item(t, v, "text", "oracle"); !oracle.Drawn || oracle.Ink {
		t.Errorf("oracle = %+v", oracle)
	}
	if pt := item(t, v, "text", "pt"); !pt.Drawn {
		t.Errorf("pt = %+v, want drawn for a creature", pt)
	}
	// An art window is there with no art, and says so
	if art := item(t, v, "art", "0"); art.Drawn || art.Why == "" || art.After != "background" {
		t.Errorf("art = %+v", art)
	}
	land := describeFor(t, svc, sum.ID, "land")
	if pt := item(t, land, "text", "pt"); pt.Drawn || pt.Why == "" {
		t.Errorf("a land's pt = %+v, want undrawn with a reason", pt)
	}
}

func TestDescribeSaysWhenALayoutCannotBeEdited(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	layouts := filepath.Join(dir, sum.ID, "layouts")
	raw, _ := os.ReadFile(filepath.Join(layouts, "normal.json"))
	// The starter's layout becomes the base for another shape, and the layout the
	// card renders with builds on it
	base := strings.Replace(string(raw), `"role": "single"`, `"role": "split"`, 1)
	if err := os.WriteFile(filepath.Join(layouts, "base.json"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layouts, "normal.json"), []byte(`{"serves": [{"role": "single", "kind": "standard"}], "extends": ["base"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Describe(DescribeRequest{ID: sum.ID, Card: presetCard(t, "monocolored")})
	if err != nil {
		t.Fatal(err)
	}
	if v.Layout != "normal" || v.Editable || !strings.Contains(v.Reason, "extends") {
		t.Fatalf("view = %+v", v)
	}
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: "title", DX: 1}}}); err == nil || !strings.Contains(err.Error(), "extends") {
		t.Errorf("an edit to a layout that extends another gave %v", err)
	}
}

func TestApplyMovesAnElementSavesItAndRendersFromTheNewFile(t *testing.T) {
	svc, ws, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	c := presetCard(t, "monocolored")
	before := renderDraft(t, svc, ws, sum.ID, c)
	was := describeFor(t, svc, sum.ID, "monocolored")
	title := item(t, was, "text", "title")

	res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: "title", DX: 20, DY: 15}}})
	if err != nil {
		t.Fatal(err)
	}
	// The starter sets its title on the middle of its capitals, which an older engine does
	// not read, so the first edit of its layout names the release that does
	if res.Rev != 1 || len(res.Inverse) != 2 || res.Inverse[0].Op != draftedit.OpUnset || res.Inverse[0].Path[0] != "minEngine" {
		t.Fatalf("result = %+v", res)
	}
	now := describeFor(t, svc, sum.ID, "monocolored")
	moved := item(t, now, "text", "title")
	if moved.Delivered.X != title.Delivered.X+20 || moved.Delivered.Y != title.Delivered.Y+15 || now.Rev != 1 {
		t.Errorf("title moved from %+v to %+v at rev %d", title.Delivered, moved.Delivered, now.Rev)
	}
	if samePixels(before, renderDraft(t, svc, ws, sum.ID, c)) {
		t.Error("the next render did not show the move")
	}
	if _, err := os.Stat(filepath.Join(dir, sum.ID, "layouts", "normal.json")); err != nil {
		t.Fatal(err)
	}

	// The inverse puts it back, and the file reads as it did
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: res.Inverse}); err != nil {
		t.Fatal(err)
	}
	if back := item(t, describeFor(t, svc, sum.ID, "monocolored"), "text", "title"); back.Delivered != title.Delivered {
		t.Errorf("after undo the title is at %+v, want %+v", back.Delivered, title.Delivered)
	}
	if !samePixels(before, renderDraft(t, svc, ws, sum.ID, c)) {
		t.Error("undoing the move did not restore the render")
	}
}

func TestApplyRefusesWhatCannotBeDoneAndChangesNothing(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	file := filepath.Join(dir, sum.ID, "layouts", "normal.json")
	original, _ := os.ReadFile(file)
	move := func(key string, dx int) ApplyRequest {
		return ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: key, DX: dx}}}
	}

	for name, req := range map[string]ApplyRequest{
		"nothing to do":            {ID: sum.ID},
		"a text box that is gone":  move("gone", 1),
		"a draft that is gone":     {ID: "gone", Ops: move("title", 1).Ops},
		"a move off the canvas":    move("title", 10_000_000),
		"a layout that is not one": {ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "../template", Kind: draftedit.KindText, Key: "title"}}},
	} {
		_, err := svc.Apply(req)
		if err == nil {
			t.Errorf("%s: the change was made", name)
		} else if k := apierr.KindOf(err); k != apierr.BadRequest && k != apierr.NotFound {
			t.Errorf("%s: error kind %v: %v", name, k, err)
		}
	}
	if now, _ := os.ReadFile(file); string(now) != string(original) {
		t.Error("a refused change altered the layout")
	}
	if svc.rev(sum.ID) != 0 {
		t.Errorf("a refused change counted as a revision: %d", svc.rev(sum.ID))
	}
}

func TestApplyAllowsAnEditToADraftThatAlreadyDoesNotLoad(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	// A broken description does not stop a template loading, but a broken layout
	// does, and a draft in that state is for the JSON to fix, not for an edit to
	// refuse over a problem it did not cause
	tpl := filepath.Join(dir, sum.ID, "template.json")
	raw, _ := os.ReadFile(tpl)
	if err := os.WriteFile(tpl, []byte(strings.Replace(string(raw), `"contract": "1.1"`, `"contract": "9.0"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: "title", DX: 1}}})
	if err != nil {
		t.Fatalf("an edit to a draft that did not load was refused: %v", err)
	}
}

func setOp(kind, key, value string, path ...string) draftedit.Op {
	return draftedit.Op{Op: draftedit.OpSet, Layout: "normal", Kind: kind, Key: key, Path: path, Value: json.RawMessage(value)}
}

func TestVocabularyNamesOnlyWhatTheEngineAccepts(t *testing.T) {
	svc, _, _ := newService(t)
	v := svc.Vocabulary()
	for _, m := range v.BlendModes {
		if _, err := manifest.ParseMode(m); err != nil {
			t.Errorf("blend mode %q: %v", m, err)
		}
	}
	if len(v.BlendModes) != 16 {
		t.Errorf("%d blend modes, want the engine's 16", len(v.BlendModes))
	}
	// A count such as rows is listed by its name and compares with a number
	for _, c := range v.Conditions {
		if _, err := frame.Compile(c); err != nil {
			if _, cerr := frame.Compile(c + ">3"); cerr != nil {
				t.Errorf("condition %q: %v, and as a count: %v", c, err, cerr)
			}
		}
	}
	for _, s := range v.Slots {
		if _, err := frame.ParseSlot(s); err != nil {
			t.Errorf("slot %q: %v", s, err)
		}
	}
	for _, want := range []string{"title", "oracle", "pt", "mana"} {
		if !slices.Contains(v.Draws, want) {
			t.Errorf("draws lacks %q", want)
		}
	}
	if got := strings.Join(v.FontRoles, ","); got != "title,body,body-italic,mana,small-caps,info" {
		t.Errorf("font roles = %s", got)
	}
}

func TestSourceHoldsTheFilesAsTheyAre(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	src, err := svc.Source(sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range src.Files {
		names = append(names, f.Name)
		raw, _ := os.ReadFile(filepath.Join(dir, sum.ID, filepath.FromSlash(f.Name)))
		if f.Content != string(raw) {
			t.Errorf("%s differs from the file", f.Name)
		}
	}
	if strings.Join(names, ",") != "template.json,layouts/normal.json" {
		t.Errorf("files = %v", names)
	}
	if _, err := svc.Source("gone"); apierr.KindOf(err) != apierr.NotFound {
		t.Errorf("a draft that is gone gave %v", err)
	}
}

func TestCheckFileSaysWhereTheJSONIsWrong(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	res, err := svc.CheckFile(CheckRequest{ID: sum.ID, File: "layouts/normal.json", Content: "{\n  \"serves\": [\n    ,\n  ]\n}"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Writable || len(res.Problems) != 1 || res.Problems[0].Line != 3 || res.Problems[0].Column < 1 {
		t.Fatalf("result = %+v, want one problem on line 3", res)
	}
}

func TestCheckFileSaysWhatTheTextWouldDo(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	src, _ := svc.Source(sum.ID)
	same := src.Files[1].Content

	// What is there already is fine and can be saved
	res, err := svc.CheckFile(CheckRequest{ID: sum.ID, File: "layouts/normal.json", Content: same})
	if err != nil || !res.Writable {
		t.Fatalf("unchanged text: %+v, %v", res, err)
	}
	for _, p := range res.Problems {
		if p.Severity == "error" {
			t.Errorf("an error in the file as it is: %+v", p)
		}
	}

	// Valid JSON that is not a layout that loads is reported with where, and is not
	// to be saved over a draft that loads
	bad := strings.Replace(same, `"align": "left"`, `"align": "diagonal"`, 1)
	res, _ = svc.CheckFile(CheckRequest{ID: sum.ID, File: "layouts/normal.json", Content: bad})
	if res.Writable || len(res.Problems) == 0 || !strings.Contains(res.Problems[0].Path, "align") || res.Problems[0].Severity != "error" {
		t.Fatalf("a layout that does not load: %+v", res)
	}

	// A draft that does not load already can be written to, to fix it
	if err := os.WriteFile(filepath.Join(dir, sum.ID, "layouts", "normal.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ = svc.CheckFile(CheckRequest{ID: sum.ID, File: "layouts/normal.json", Content: bad})
	if !res.Writable || len(res.Problems) == 0 {
		t.Errorf("a draft that did not load: %+v, want it writable with its problems", res)
	}

	// A file that is not one of the draft's
	res, _ = svc.CheckFile(CheckRequest{ID: sum.ID, File: "../outside.json", Content: "{}"})
	if res.Writable || len(res.Problems) != 1 {
		t.Errorf("a file outside the draft: %+v", res)
	}
}

func TestApplySetsAFieldAndTheInverseTakesItBack(t *testing.T) {
	svc, ws, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	file := filepath.Join(dir, sum.ID, "layouts", "normal.json")
	original, _ := os.ReadFile(file)
	c := presetCard(t, "monocolored")
	before := renderDraft(t, svc, ws, sum.ID, c)

	res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{setOp(draftedit.KindText, "title", `"#FF0000"`, "text", "color")}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Rev != 1 {
		t.Fatalf("result = %+v", res)
	}
	if samePixels(before, renderDraft(t, svc, ws, sum.ID, c)) {
		t.Error("the colour did not change the render")
	}
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: res.Inverse}); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(file); string(now) != string(original) {
		t.Error("undoing did not restore the file")
	}
}

func TestApplyRefusesAValueTheEngineDoesNotAcceptAndSaysWhich(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	file := filepath.Join(dir, sum.ID, "layouts", "normal.json")
	original, _ := os.ReadFile(file)
	for name, op := range map[string]draftedit.Op{
		"an alignment that is not one":  setOp(draftedit.KindText, "title", `"diagonal"`, "text", "align"),
		"a condition that is not known": setOp(draftedit.KindText, "title", `"made_up"`, "condition"),
		"a size of nothing":             setOp(draftedit.KindText, "title", `0`, "rect", "width"),
		"a color that is not one":       setOp(draftedit.KindText, "title", `"red"`, "text", "color"),
	} {
		_, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{op}})
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%s: error = %v, want the reason", name, err)
		}
	}
	if now, _ := os.ReadFile(file); string(now) != string(original) {
		t.Error("a refused change altered the layout")
	}
}

func TestApplyOfAWholeFileFixesADraftThatDoesNotLoadAndUndoBreaksItAgain(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	file := filepath.Join(dir, sum.ID, "layouts", "normal.json")
	good, _ := os.ReadFile(file)
	broken := strings.Replace(string(good), `"align": "left"`, `"align": "diagonal"`, 1)
	if err := os.WriteFile(file, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpReplace, File: "layouts/normal.json", Content: string(good)}}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || len(res.Inverse) != 1 {
		t.Fatalf("result = %+v", res)
	}
	// Undo goes back to the draft that did not load, which a whole file is let do
	back, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: res.Inverse})
	if err != nil {
		t.Fatal(err)
	}
	if back.Valid || len(back.Diagnostics) == 0 || back.Diagnostics[0].Severity != "error" {
		t.Errorf("result = %+v, want the problems of the draft that does not load", back)
	}
	if now, _ := os.ReadFile(file); string(now) != broken {
		t.Error("undo did not write the old text")
	}
}

func TestLayersAreAddedDuplicatedAndDeletedAndUndone(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	layout := func() string { return string(readDraft(t, dir, sum.ID, "layouts/normal.json")) }
	was := layout()
	apply := func(ops ...draftedit.Op) ApplyResult {
		t.Helper()
		res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: ops})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Valid {
			t.Fatalf("the draft stopped loading: %+v", res.Diagnostics)
		}
		return res
	}

	add := apply(
		draftedit.Op{Op: draftedit.OpAdd, Layout: "normal", Kind: draftedit.KindLayer, Key: "again", To: 1, Value: json.RawMessage(`{"id":"again","part":"background","opacity":0.5}`)},
		draftedit.Op{Op: draftedit.OpAdd, Layout: "normal", Kind: draftedit.KindText, Key: "note", Value: json.RawMessage(`{"draws":"oracle","text":{"fontSize":10}}`), Rect: &draftedit.Rect{X: 20, Y: 100, Width: 100, Height: 30}},
	)
	dup := apply(draftedit.Op{Op: draftedit.OpDuplicate, Layout: "normal", Kind: draftedit.KindText, Key: "oracle", As: "oracle2", DX: 0, DY: 15})
	v := describeFor(t, svc, sum.ID, "monocolored")
	if !item(t, v, "layer", "again").Drawn || !item(t, v, "text", "note").Drawn || !item(t, v, "text", "oracle2").Drawn {
		t.Errorf("the new items are not drawn: %+v", v.Items)
	}
	if o, c := item(t, v, "text", "oracle"), item(t, v, "text", "oracle2"); c.Delivered.Y != o.Delivered.Y+15 {
		t.Errorf("the copy is at %v and the original at %v", c.Delivered, o.Delivered)
	}
	del := apply(draftedit.Op{Op: draftedit.OpDelete, Layout: "normal", Kind: draftedit.KindText, Key: "oracle"})
	if v := describeFor(t, svc, sum.ID, "monocolored"); len(v.Items) == 0 {
		t.Fatal("nothing is described")
	} else {
		for _, it := range v.Items {
			if it.Kind == "text" && it.Key == "oracle" {
				t.Error("the deleted box is still described")
			}
		}
	}
	for _, inverse := range [][]draftedit.Op{del.Inverse, dup.Inverse, add.Inverse} {
		apply(inverse...)
	}
	if layout() != was {
		t.Errorf("undoing everything did not restore the layout:\n%s", layout())
	}

	// A layer of a part the template does not have would stop it loading, so it is refused
	_, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpAdd, Layout: "normal", Kind: draftedit.KindLayer, Key: "ghost", To: 1, Value: json.RawMessage(`{"id":"ghost","part":"nothere"}`)}}})
	if err == nil || !strings.Contains(err.Error(), "That is not allowed") || layout() != was {
		t.Errorf("err = %v, layout changed = %v", err, layout() != was)
	}
}

// editsOf is the form's values for a card, as Single sends them, before anything is
// changed
func editsOf(c card.Data) *cardlist.Edits {
	return &cardlist.Edits{Name: c.Name, ManaCost: c.ManaCost, TypeLine: c.TypeLine, Oracle: c.OracleText, Flavor: c.FlavorText, Power: c.Power, Toughness: c.Toughness, Loyalty: c.Loyalty, Artist: c.Artist, SetCode: c.SetCode, Collector: c.CollectorNumber, Rarity: c.Rarity, Released: c.ReleasedAt, Language: c.Language}
}

func TestEditsToTheTestCardChangeWhatTheDraftDraws(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	creature := presetCard(t, "monocolored")

	// With the form's own values the card is as it was, and with no edits it is too
	same, err := svc.Describe(DescribeRequest{ID: sum.ID, Card: creature, Edits: editsOf(creature)})
	if err != nil {
		t.Fatal(err)
	}
	if pt := item(t, same, "text", "pt"); !pt.Drawn {
		t.Fatalf("pt = %+v, want drawn for a creature", pt)
	}

	// Turning it into a land, with no power or toughness, leaves the box for them nothing
	// to draw
	e := editsOf(creature)
	e.TypeLine, e.Power, e.Toughness = "Land", "", ""
	land, err := svc.Describe(DescribeRequest{ID: sum.ID, Card: creature, Edits: e})
	if err != nil {
		t.Fatal(err)
	}
	if pt := item(t, land, "text", "pt"); pt.Drawn || pt.Why == "" {
		t.Errorf("pt of the edited card = %+v, want undrawn with a reason", pt)
	}
	// The card that was sent is not changed by it
	if creature.TypeLine == "Land" {
		t.Error("the edits were written into the card the caller held")
	}
}

func TestEditsAreLaidOverTheCardThatIsRendered(t *testing.T) {
	svc, ws, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	c := presetCard(t, "monocolored")
	plain := renderDraft(t, svc, ws, sum.ID, c)

	// The same call with an edit to the name is a render of another card, which the
	// job names by it
	e := editsOf(c)
	e.Name = "A Very Different Name"
	started, err := svc.Render(RenderRequest{ID: sum.ID, Card: c, Edits: e})
	if err != nil {
		t.Fatal(err)
	}
	if ev := finish(t, ws, started.JobID); ev.Err != "" {
		t.Fatalf("render failed: %s", ev.Err)
	}
	j, _ := ws.Jobs.Lookup(started.JobID)
	img, name := j.Result()
	if name != "A Very Different Name" {
		t.Errorf("the render is named %q", name)
	}
	if samePixels(plain, img) {
		t.Error("an edited name left the card as it was")
	}
}

func TestTheTestCardIsKeptAsEdited(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	c := presetCard(t, "monocolored")
	e := editsOf(c)
	e.Name, e.Oracle, e.Power = "Kept Name", "A rule I wrote.", "9"
	if err := svc.SetTestCard(TestCardRequest{ID: sum.ID, Card: &c, Edits: e}); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Open(sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := v.Meta.TestCard
	if got == nil || got.Name != "Kept Name" || got.OracleText != "A rule I wrote." || got.Power != "9" {
		t.Fatalf("the kept card = %+v", got)
	}
	// What was not edited is what the card had, including the art it came with
	if got.TypeLine != c.TypeLine || got.ArtworkURL != c.ArtworkURL {
		t.Errorf("the kept card lost what was not edited: %+v", got)
	}

	// With no edits the card is kept as it is
	if err := svc.SetTestCard(TestCardRequest{ID: sum.ID, Card: &c}); err != nil {
		t.Fatal(err)
	}
	v, _ = svc.Open(sum.ID)
	if v.Meta.TestCard.Name != c.Name {
		t.Errorf("a card with no edits was kept as %q", v.Meta.TestCard.Name)
	}
}

func TestEditsKeepTheArtTheCardCameWith(t *testing.T) {
	c := presetCard(t, "monocolored")
	c.ArtworkURL = "https://cards.example/art.jpg"
	e := editsOf(c)
	e.Name = "Renamed"
	got := withEdits(c, e)
	if got.Name != "Renamed" || got.ArtworkURL != c.ArtworkURL {
		t.Errorf("edited card = %+v", got)
	}
	for _, f := range got.Faces {
		_ = f
	}
	if same := withEdits(c, nil); same.Name != c.Name || same.ArtworkURL != c.ArtworkURL {
		t.Errorf("a card with no edits changed: %+v", same)
	}
}

func TestDescribeSaysWhatAnEditorNeedsOfATextBox(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	v := describeFor(t, svc, sum.ID, "monocolored")

	// The title sits on its baseline, which is a line across the box's width at its Y
	title := item(t, v, "text", "title")
	if title.Baseline == nil || title.Baseline.Height != 0 || title.Baseline.Width <= 0 {
		t.Fatalf("title baseline = %+v, want a horizontal line", title.Baseline)
	}
	// A new template sets its title on the middle of its capitals, and says so
	if title.Anchor != "cap-center" {
		t.Errorf("title anchor = %q, want cap-center", title.Anchor)
	}
	if mana := item(t, v, "text", "mana"); mana.Anchor != "cap-center" {
		t.Errorf("mana anchor = %q, want cap-center", mana.Anchor)
	}
	var box struct{ X, Y, W int }
	src, _ := svc.Source(sum.ID)
	for _, f := range src.Files {
		if f.Name != "layouts/normal.json" {
			continue
		}
		var l struct {
			TextBoxes map[string]struct {
				Rect struct{ X, Y, Width, Height int }
			} `json:"textBoxes"`
		}
		if err := json.Unmarshal([]byte(f.Content), &l); err != nil {
			t.Fatal(err)
		}
		r := l.TextBoxes["title"].Rect
		box.X, box.Y, box.W = r.X, r.Y, r.Width
	}
	if *title.Baseline != (RectView{X: box.X, Y: box.Y, Width: box.W, Height: 0}) {
		t.Errorf("baseline %+v is not the box's own X, Y and width %+v", *title.Baseline, box)
	}
	// The oracle text is not set on its baseline
	if item(t, v, "text", "oracle").Baseline != nil {
		t.Error("a box with an area has a baseline")
	}
	// Text that draws says how it fitted, and a layer or an art window does not
	if fit := title.Fit; fit == nil || fit.Size <= 0 || fit.Wanted <= 0 || fit.Floor <= 0 || fit.Floor > fit.Wanted || fit.Lines != 1 {
		t.Errorf("title fit = %+v", fit)
	}
	if item(t, v, "layer", "background").Fit != nil || item(t, v, "art", "0").Fit != nil {
		t.Error("something that is not text has a fit")
	}
	// No override applies to the starter, and its title is narrowed to clear the mana cost
	if title.Override != nil {
		t.Errorf("override = %+v", title.Override)
	}
	if !title.Narrowed || title.Moved || title.Placed == nil || title.Placed.Width >= box.W {
		t.Errorf("title narrowed %v moved %v placed %+v, want narrower than %d", title.Narrowed, title.Moved, title.Placed, box.W)
	}
	// A box the card leaves alone has no placement to report
	if oracle := item(t, v, "text", "oracle"); oracle.Placed != nil || oracle.Moved || oracle.Narrowed {
		t.Errorf("oracle = %+v", oracle)
	}
}

func TestStretchMovesASideOfABaselineBoxAndUndoPutsItBack(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	was := item(t, describeFor(t, svc, sum.ID, "monocolored"), "text", "title").Baseline
	res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpStretch, Layout: "normal", Kind: draftedit.KindText, Key: "title", Edge: "e", DX: -20}}})
	if err != nil {
		t.Fatal(err)
	}
	now := item(t, describeFor(t, svc, sum.ID, "monocolored"), "text", "title").Baseline
	if now.X != was.X || now.Y != was.Y || now.Width != was.Width-20 {
		t.Errorf("the baseline went from %+v to %+v, want the same start and 20 shorter", *was, *now)
	}
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: res.Inverse}); err != nil {
		t.Fatal(err)
	}
	if back := item(t, describeFor(t, svc, sum.ID, "monocolored"), "text", "title").Baseline; *back != *was {
		t.Errorf("after undo the baseline is %+v, want %+v", *back, *was)
	}
}

func TestARenderLeavesOutWhatItIsToldToHideAndNothingElse(t *testing.T) {
	svc, ws, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	c := presetCard(t, "monocolored")
	shown := renderDraft(t, svc, ws, sum.ID, c)
	render := func(hide, show []string) image.Image {
		t.Helper()
		started, err := svc.Render(RenderRequest{ID: sum.ID, Card: c, Hide: hide, Show: show})
		if err != nil {
			t.Fatal(err)
		}
		if ev := finish(t, ws, started.JobID); ev.Err != "" {
			t.Fatalf("render failed: %s", ev.Err)
		}
		j, _ := ws.Jobs.Lookup(started.JobID)
		img, _ := j.Result()
		return img
	}
	if samePixels(shown, render([]string{"layer:background"}, nil)) {
		t.Error("hiding the background changed nothing")
	}
	if samePixels(shown, render([]string{"text:title"}, nil)) {
		t.Error("hiding the title changed nothing")
	}
	// A name that is nothing, and a layer shown that already draws, change nothing
	if !samePixels(shown, render([]string{"layer:nothing", "text:nothing"}, []string{"layer:background"})) {
		t.Error("names that match nothing changed the render")
	}
	// The template is as it was
	if !samePixels(shown, renderDraft(t, svc, ws, sum.ID, c)) {
		t.Error("a hidden render changed the next one")
	}
	// A strip of coverage cards ignores what the card is hiding
	started, err := svc.Render(RenderRequest{ID: sum.ID, Card: c, Hide: []string{"layer:background"}, Coverage: true})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, ws, started.JobID)
	hidden, _ := ws.Jobs.Lookup(started.JobID)
	plain, err := svc.Render(RenderRequest{ID: sum.ID, Card: c, Coverage: true})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, ws, plain.JobID)
	base, _ := ws.Jobs.Lookup(plain.JobID)
	a, _ := hidden.Result()
	b, _ := base.Result()
	if !samePixels(a, b) {
		t.Error("a coverage render was changed by what the card hides")
	}
}

func TestFullDrawsAtTheResolutionTheLayoutIsAuthoredAt(t *testing.T) {
	svc, ws, _ := newService(t)
	sum, err := svc.Create(CreateRequest{Name: "Frame", DPI: 400})
	if err != nil {
		t.Fatal(err)
	}
	c := presetCard(t, "monocolored")
	preview, err := svc.Render(RenderRequest{ID: sum.ID, Card: c})
	if err != nil {
		t.Fatal(err)
	}
	full, err := svc.Render(RenderRequest{ID: sum.ID, Card: c, Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.DPI != workspace.DefaultPreviewDPI || full.DPI != 400 {
		t.Errorf("the preview drew at %d and the full size at %d, want %d and 400", preview.DPI, full.DPI, workspace.DefaultPreviewDPI)
	}
	for _, id := range []string{preview.JobID, full.JobID} {
		if j, ok := ws.Jobs.Lookup(id); ok {
			j.Cancel()
		}
	}
}

func TestOpenAndApplyCarryTheDraftsStamp(t *testing.T) {
	svc, _, dir := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	opened, err := svc.Open(sum.ID)
	if err != nil || opened.Stamp == "" {
		t.Fatalf("Open stamp = %q, %v", opened.Stamp, err)
	}
	res, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: "title", DX: 5, DY: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	now, _ := svc.Stamp(sum.ID)
	if res.Stamp != now || now == opened.Stamp {
		t.Errorf("Apply stamp %q, the draft's now %q, before %q, want the first to be the second and not the third", res.Stamp, now, opened.Stamp)
	}
	// A file written outside the app moves it, which is what the page asks after the window regains focus
	file := filepath.Join(dir, sum.ID, "layouts", "normal.json")
	raw, _ := os.ReadFile(file)
	if err := os.WriteFile(file, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.Stamp(sum.ID); after == res.Stamp {
		t.Error("a change outside the app left the stamp as it was")
	}
}

func TestChangingTheTemplatesIdTakesBackTheCopyMadeUnderTheOldOne(t *testing.T) {
	svc, _, _ := newService(t)
	sum, _ := svc.Create(CreateRequest{Name: "Frame", DPI: 100})
	if _, err := svc.Use(sum.ID); err != nil {
		t.Fatal(err)
	}
	if !catalog.IsCached("user.frame", pipeline.DraftVersion) {
		t.Fatal("Use did not install the draft")
	}
	set := draftedit.Op{Op: draftedit.OpSet, Kind: draftedit.KindTemplate, Path: []string{"id"}, Value: []byte(`"jane.frame"`)}
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{set}}); err != nil {
		t.Fatal(err)
	}
	if catalog.IsCached("user.frame", pipeline.DraftVersion) {
		t.Error("the copy under the old id is still installed")
	}
	rel, err := svc.Release(sum.ID)
	if err != nil || rel.TemplateID != "jane.frame" {
		t.Errorf("Release = %+v, %v, want the template under its new id", rel, err)
	}
	got, _ := svc.Open(sum.ID)
	if got.Draft.TemplateID != "jane.frame" {
		t.Errorf("the draft's template id is %q", got.Draft.TemplateID)
	}
	// It is made available again under the new one, and an edit that leaves the id alone takes nothing back
	if _, err := svc.Use(sum.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(ApplyRequest{ID: sum.ID, Ops: []draftedit.Op{{Op: draftedit.OpMove, Layout: "normal", Kind: draftedit.KindText, Key: "title", DX: 3, DY: 3}}}); err != nil {
		t.Fatal(err)
	}
	if !catalog.IsCached("jane.frame", pipeline.DraftVersion) {
		t.Error("an edit that left the id alone took the installed copy back")
	}
}
