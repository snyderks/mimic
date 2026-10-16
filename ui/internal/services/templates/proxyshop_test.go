package templates

import (
	"bytes"
	"context"
	"flag"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/odevine/mimic/engine/proxyshop"
	"github.com/odevine/mimic/engine/proxyshop/proxyshoptest"
	"github.com/odevine/mimic/engine/psd"
	"github.com/odevine/mimic/engine/template"
	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/batch"
	"github.com/odevine/mimic/ui/internal/catalog"
	"github.com/odevine/mimic/ui/internal/jobs"
	"github.com/odevine/mimic/ui/internal/pipeline"
	"github.com/odevine/mimic/ui/internal/workspace/workspacetest"
)

// proxyshopPSD is a small document laid out the way Proxyshop's templates are:
// a Background group with a hidden layer per color, an Art Frame reference and
// the art in Layer 1
func proxyshopPSD() []byte {
	const w, h = 100, 76
	frame := image.Rect(6, 6, w-6, h-6)
	window := image.Rect(16, 16, w-16, 40)
	solid := func(c color.NRGBA) proxyshoptest.Layer {
		return proxyshoptest.Pixels(frame, func(x, y int) color.NRGBA {
			if image.Pt(x, y).In(window) {
				return color.NRGBA{}
			}
			return c
		})
	}
	return proxyshoptest.Write(w, h,
		proxyshoptest.Folder("Background", false,
			proxyshoptest.Named(solid(color.NRGBA{120, 160, 120, 255}), "WU", true),
			proxyshoptest.Named(solid(color.NRGBA{0, 110, 180, 255}), "U", true),
			proxyshoptest.Named(solid(color.NRGBA{240, 238, 230, 255}), "W", true),
		),
		proxyshoptest.Named(proxyshoptest.Pixels(window, func(x, y int) color.NRGBA { return color.NRGBA{A: 255} }), "Art Frame", true),
		proxyshoptest.Named(proxyshoptest.Pixels(image.Rect(0, 0, w, h), func(x, y int) color.NRGBA { return color.NRGBA{9, 9, 9, 255} }), "Layer 1", false),
	)
}

var update = flag.Bool("update", false, "rewrite the Proxyshop PSD the end to end tests import")

// e2ePSD is the document the end to end tests offer the app to import
const e2ePSD = "../../../testassets/proxyshop/normal.psd"

func TestTheEndToEndPSDIsTheOneTheseTestsConvert(t *testing.T) {
	want := proxyshopPSD()
	if *update {
		if err := os.MkdirAll(filepath.Dir(e2ePSD), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(e2ePSD, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(e2ePSD)
	if err != nil {
		t.Fatalf("%v: run go test ./internal/services/templates -run EndToEndPSD -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not the document these tests convert: run go test ./internal/services/templates -run EndToEndPSD -update", e2ePSD)
	}
}

// writePSDs writes the document under each name in a fresh folder and returns
// their paths
func writePSDs(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	doc := proxyshopPSD()
	var out []string
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, doc, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// finish waits for a job to end and returns its events
func finish(t *testing.T, svc *Service, id string) []jobs.Event {
	t.Helper()
	j, ok := svc.ws.Jobs.Lookup(id)
	if !ok {
		t.Fatalf("no job %s", id)
	}
	deadline := time.After(2 * time.Minute)
	for {
		events, finished, wait := j.Stream(0)
		if finished {
			return events
		}
		select {
		case <-wait:
		case <-deadline:
			t.Fatal("the import did not finish")
		}
	}
}

func TestPlanImportNamesTheTemplateAfterItsFiles(t *testing.T) {
	cases := []struct {
		names []string
		id    string
		name  string
		keys  []string
		files []string
	}{
		{[]string{"normal.psd"}, "proxyshop.normal", "Normal (Proxyshop)", []string{"normal"}, []string{"normal.psd"}},
		{[]string{"Borderless Classic.PSD"}, "proxyshop.borderless-classic", "Borderless Classic (Proxyshop)", []string{"borderless_classic"}, []string{"Borderless Classic.PSD"}},
		{[]string{"tf-front.psd", "tf-back.psd"}, "proxyshop.tf", "Transform (Proxyshop)", []string{"front", "back"}, []string{"tf-front.psd", "tf-back.psd"}},
		{[]string{"mdfc-back.psd", "mdfc-front.psd"}, "proxyshop.mdfc", "MDFC (Proxyshop)", []string{"front", "back"}, []string{"mdfc-front.psd", "mdfc-back.psd"}},
		{[]string{"front.psd", "back.psd"}, "proxyshop.template", "Template (Proxyshop)", []string{"front", "back"}, []string{"front.psd", "back.psd"}},
	}
	for _, c := range cases {
		plan, err := planImport(writePSDs(t, c.names...))
		if err != nil {
			t.Errorf("%v: %v", c.names, err)
			continue
		}
		var keys, files []string
		for _, f := range plan.files {
			keys, files = append(keys, f.Key), append(files, f.Name)
		}
		if plan.id != c.id || plan.name != c.name || !slices.Equal(keys, c.keys) || !slices.Equal(files, c.files) {
			t.Errorf("%v planned %s %q %v %v, want %s %q %v %v", c.names, plan.id, plan.name, keys, files, c.id, c.name, c.keys, c.files)
		}
		if !pipeline.ValidID(plan.id) {
			t.Errorf("%v made the id %q, which the app cannot keep", c.names, plan.id)
		}
	}
}

func TestPlanImportTakesTheOneOrTwoPSDsInAFolder(t *testing.T) {
	paths := writePSDs(t, "tf-front.psd", "tf-back.psd")
	os.WriteFile(filepath.Join(filepath.Dir(paths[0]), "readme.txt"), nil, 0o644)
	plan, err := planImport([]string{filepath.Dir(paths[0])})
	if err != nil || plan.id != "proxyshop.tf" || len(plan.files) != 2 {
		t.Errorf("a folder of a front and a back planned %+v, %v", plan, err)
	}
	three := writePSDs(t, "a.psd", "b.psd", "c.psd")
	if _, err := planImport([]string{filepath.Dir(three[0])}); err == nil || !strings.Contains(err.Error(), "holds 3 PSDs") {
		t.Errorf("a folder of three: %v", err)
	}
	if _, err := planImport([]string{t.TempDir()}); err == nil || !strings.Contains(err.Error(), "holds no PSD") {
		t.Errorf("an empty folder: %v", err)
	}
}

func TestPlanImportRefusesWhatIsNotOneTemplate(t *testing.T) {
	other := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(other, []byte("x"), 0o644)
	for name, paths := range map[string][]string{
		"nothing":            nil,
		"three files":        writePSDs(t, "a.psd", "b.psd", "c.psd"),
		"not a PSD":          {other},
		"a missing file":     {filepath.Join(t.TempDir(), "gone.psd")},
		"two unrelated PSDs": writePSDs(t, "normal.psd", "saga.psd"),
		"two fronts":         writePSDs(t, "tf-front.psd", "tf-front-old.psd"),
	} {
		if _, err := planImport(paths); err == nil {
			t.Errorf("%s was planned", name)
		}
	}
}

func TestImportProxyshopInstallsAListedTemplate(t *testing.T) {
	svc := New(workspacetest.New(t))
	paths := writePSDs(t, "normal.psd")

	id, err := svc.ImportProxyshop(paths)
	if err != nil {
		t.Fatal(err)
	}
	events := finish(t, svc, id)
	last := events[len(events)-1]
	if last.Err != "" || last.Imported == nil {
		t.Fatalf("the import ended with %+v", last)
	}
	got := last.Imported
	if got.ID != "proxyshop.normal" || got.Name != "Normal (Proxyshop)" || got.Version != firstImportVersion || got.Notes == nil {
		t.Errorf("imported %+v", got)
	}
	var steps []string
	for _, e := range events[:len(events)-1] {
		steps = append(steps, e.Step)
		if e.Frac < 0 || e.Frac > 1 {
			t.Errorf("progress %v", e.Frac)
		}
	}
	if len(steps) < 3 || steps[0] != "Reading normal.psd…" || !strings.HasPrefix(steps[1], "Drawing normal_background, part 1 of") || steps[len(steps)-1] != "Installing the template…" {
		t.Errorf("steps %q", steps)
	}

	if !catalog.IsCached(got.ID, got.Version) {
		t.Fatal("the bundle is not in the cache")
	}
	in, ok := pipeline.InstalledInfo(got.ID)
	if !ok || in.Name != "Normal (Proxyshop)" || !slices.Contains(in.Shapes, template.Shape{Role: template.RoleSingle, Kind: template.KindStandard}) {
		t.Errorf("installed %+v, %v", in, ok)
	}
	var row *TemplateView
	for _, tv := range svc.List(context.Background()) {
		if tv.Name == got.ID {
			row = &tv
		}
	}
	if row == nil || row.Title != got.Name || len(row.Versions) != 1 || row.Versions[0].Version != got.Version || row.Versions[0].Action != "select" {
		t.Fatalf("the library lists %+v", row)
	}
	// The import is not made the active template
	if name, _ := svc.ws.Active(); name == got.ID {
		t.Error("the import was made the active template")
	}

	// Importing again keeps the first and installs the next version beside it
	id, err = svc.ImportProxyshop(paths)
	if err != nil {
		t.Fatal(err)
	}
	events = finish(t, svc, id)
	if again := events[len(events)-1].Imported; again == nil || again.Version != "0.1.1" {
		t.Fatalf("the second import ended with %+v", events[len(events)-1])
	}
	if v, _ := catalog.CachedVersions(got.ID); !slices.Equal(v, []string{"0.1.1", "0.1.0"}) {
		t.Errorf("installed versions %v", v)
	}
	entries, _ := os.ReadDir(filepath.Dir(mustBundlePath(t, got.ID, got.Version)))
	if len(entries) != 2 {
		t.Errorf("the template's folder holds %d files, want the two bundles", len(entries))
	}
}

func mustBundlePath(t *testing.T, id, ver string) string {
	t.Helper()
	p, err := catalog.BundlePath(id, ver)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportProxyshopMakesOneTemplateOfAFrontAndABack(t *testing.T) {
	svc := New(workspacetest.New(t))
	id, err := svc.ImportProxyshop(writePSDs(t, "tf-back.psd", "tf-front.psd"))
	if err != nil {
		t.Fatal(err)
	}
	events := finish(t, svc, id)
	got := events[len(events)-1].Imported
	if got == nil || got.ID != "proxyshop.tf" {
		t.Fatalf("the import ended with %+v", events[len(events)-1])
	}
	in, ok := pipeline.InstalledInfo(got.ID)
	front := template.Shape{Role: template.RoleTransformFront, Kind: template.KindStandard}
	back := template.Shape{Role: template.RoleTransformBack, Kind: template.KindStandard}
	if !ok || !slices.Contains(in.Shapes, front) || !slices.Contains(in.Shapes, back) {
		t.Errorf("installed %+v, %v", in.Shapes, ok)
	}
}

func TestAStoppedImportInstallsNothing(t *testing.T) {
	svc := New(workspacetest.New(t))
	plan, err := planImport(writePSDs(t, "normal.psd"))
	if err != nil {
		t.Fatal(err)
	}
	// Stopped as the first part starts drawing, the conversion unwinds before the next
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	report := func(step string, frac float64) {
		if strings.HasPrefix(step, "Drawing") {
			cancel()
		}
	}
	if _, err := svc.importTemplate(ctx, plan, report); err != errImportStopped {
		t.Errorf("a stop while drawing gave %v", err)
	}
	// Stopped before it starts, no PSD is opened
	if _, err := svc.importTemplate(ctx, plan, func(string, float64) {}); err != errImportStopped {
		t.Errorf("a stop before reading gave %v", err)
	}
	if catalog.IsCached(plan.id, firstImportVersion) {
		t.Error("a stopped import installed a bundle")
	}
	if dir, err := catalog.Dir(); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(dir, plan.id)); len(entries) > 0 {
			t.Errorf("a stopped import left %v", entries)
		}
	}
}

func TestCancelImportStopsTheJob(t *testing.T) {
	svc := New(workspacetest.New(t))
	id, err := svc.ImportProxyshop(writePSDs(t, "normal.psd"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelImport(id); err != nil {
		t.Fatal(err)
	}
	events := finish(t, svc, id)
	// The import may have finished before the stop reached it, and either way it ends
	last := events[len(events)-1]
	if last.Imported == nil && last.Err != errImportStopped.Error() {
		t.Errorf("a stopped import ended with %+v", last)
	}
	if err := svc.CancelImport("no-such-job"); apierr.KindOf(err) != apierr.NotFound {
		t.Errorf("stopping an unknown job: %v", err)
	}
}

func TestImportProxyshopRefusals(t *testing.T) {
	svc := New(workspacetest.New(t))
	if _, err := svc.ImportProxyshop(nil); apierr.KindOf(err) != apierr.BadRequest {
		t.Errorf("no files: %v", err)
	}
	svc.importing.Store(true)
	if _, err := svc.ImportProxyshop(writePSDs(t, "normal.psd")); apierr.KindOf(err) != apierr.Conflict {
		t.Errorf("a second import at once: %v", err)
	}
	svc.importing.Store(false)
	svc.ws.Run.Set(batch.New(nil, batch.Options{ID: "run-1"}), nil)
	if _, err := svc.ImportProxyshop(writePSDs(t, "normal.psd")); apierr.KindOf(err) != apierr.Conflict {
		t.Errorf("an import mid-run: %v", err)
	}
}

func TestAConversionThatPanicsFailsTheImport(t *testing.T) {
	open := func(string) (*psd.File, error) { panic("bad layer") }
	_, _, err := convert([]proxyshop.SourceFile{{Name: "normal.psd"}}, open, proxyshop.Options{ID: "proxyshop.normal"})
	if err == nil || err.Error() != "The conversion failed: bad layer" {
		t.Errorf("a panic gave %v", err)
	}
}
