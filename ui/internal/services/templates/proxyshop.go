package templates

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/odevine/mimic/engine/proxyshop"
	"github.com/odevine/mimic/engine/psd"
	"github.com/odevine/mimic/engine/version"
	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/catalog"
	"github.com/odevine/mimic/ui/internal/jobs"
)

// ProxyshopOwner is the owner part of the id an imported Proxyshop template takes
const ProxyshopOwner = "proxyshop"

// firstImportVersion is the version a template takes the first time it is
// imported, and each import after that takes the next patch
const firstImportVersion = "0.1.0"

// errImportStopped is how a stopped import unwinds out of the conversion
var errImportStopped = errors.New("The import was stopped")

// importPlan is what an import converts: the PSDs under the keys the recipe
// gives them, where each one is on disk, and the template they become
type importPlan struct {
	files       []proxyshop.SourceFile
	paths       map[string]string
	id          string
	name        string
	description string
}

// abbreviations spell out the short words Proxyshop's file names use
var abbreviations = map[string]string{
	"tf":   "Transform",
	"mdfc": "MDFC",
	"dfc":  "DFC",
	"pw":   "Planeswalker",
	"psd":  "PSD",
}

// words splits a file name without its extension into lower case runs of
// letters and digits
func words(file string) []string {
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	return strings.FieldsFunc(strings.ToLower(stem), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// title spells words the way a template's name shows them
func title(ws []string) string {
	out := make([]string, len(ws))
	for i, w := range ws {
		if a, ok := abbreviations[w]; ok {
			out[i] = a
		} else {
			out[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(out, " ")
}

// faces finds the word that tells two file names apart when it is front in one
// and back in the other, and returns the words they share. It reports false for
// names that differ in any other way
func faces(a, b []string) (shared []string, aFront, ok bool) {
	if len(a) != len(b) {
		return nil, false, false
	}
	at := -1
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if at >= 0 {
			return nil, false, false
		}
		at = i
	}
	if at < 0 {
		return nil, false, false
	}
	pair := []string{a[at], b[at]}
	switch {
	case pair[0] == "front" && pair[1] == "back":
		aFront = true
	case pair[0] == "back" && pair[1] == "front":
	default:
		return nil, false, false
	}
	return slices.Delete(slices.Clone(a), at, at+1), aFront, true
}

// folderPSDs lists the PSDs directly inside a folder, which an import takes in
// place of the folder when there are one or two of them
func folderPSDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read", filepath.Base(dir))
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.EqualFold(filepath.Ext(e.Name()), ".psd") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	switch {
	case len(out) == 0:
		return nil, fmt.Errorf("%s holds no PSD", filepath.Base(dir))
	case len(out) > 2:
		return nil, fmt.Errorf("%s holds %d PSDs, so choose the one to import, or its front and back", filepath.Base(dir), len(out))
	}
	return out, nil
}

// planImport decides what an import of the chosen PSDs makes. One PSD becomes a
// template of its own. Two become one template with a front and a back layout,
// which needs their names to be alike apart from front and back, as in
// tf-front.psd and tf-back.psd. A folder stands for the PSDs in it. The id and
// name come from the file names
func planImport(paths []string) (importPlan, error) {
	if len(paths) == 1 {
		if st, err := os.Stat(paths[0]); err == nil && st.IsDir() {
			found, err := folderPSDs(paths[0])
			if err != nil {
				return importPlan{}, err
			}
			paths = found
		}
	}
	if len(paths) == 0 || len(paths) > 2 {
		return importPlan{}, fmt.Errorf("Choose one Proxyshop PSD, or a front and a back PSD that make one template")
	}
	for _, p := range paths {
		if !strings.EqualFold(filepath.Ext(p), ".psd") {
			return importPlan{}, fmt.Errorf("%s is not a Photoshop document", filepath.Base(p))
		}
		st, err := os.Stat(p)
		if err != nil {
			return importPlan{}, fmt.Errorf("%s cannot be read", filepath.Base(p))
		}
		if !st.Mode().IsRegular() {
			return importPlan{}, fmt.Errorf("%s is not a file", filepath.Base(p))
		}
	}
	plan := importPlan{paths: map[string]string{}}
	var shared []string
	if len(paths) == 1 {
		name := filepath.Base(paths[0])
		shared = words(name)
		plan.files = []proxyshop.SourceFile{{Key: sourceKey(name), Name: name}}
		plan.paths[name] = paths[0]
		plan.description = fmt.Sprintf("Converted from the Proxyshop template %s", name)
	} else {
		a, b := filepath.Base(paths[0]), filepath.Base(paths[1])
		var aFront, ok bool
		shared, aFront, ok = faces(words(a), words(b))
		if !ok || a == b {
			return importPlan{}, fmt.Errorf("Two PSDs make one template when their names differ only in front and back, as tf-front.psd and tf-back.psd do")
		}
		front, back := paths[0], paths[1]
		if !aFront {
			front, back = back, front
		}
		plan.files = []proxyshop.SourceFile{{Key: "front", Name: filepath.Base(front)}, {Key: "back", Name: filepath.Base(back)}}
		plan.paths[filepath.Base(front)] = front
		plan.paths[filepath.Base(back)] = back
		plan.description = fmt.Sprintf("Converted from the Proxyshop templates %s and %s", filepath.Base(front), filepath.Base(back))
	}
	if len(shared) == 0 {
		shared = []string{"template"}
	}
	plan.id = ProxyshopOwner + "." + strings.Join(shared, "-")
	plan.name = title(shared) + " (Proxyshop)"
	return plan, nil
}

// sourceKey is the recipe key of a lone PSD, which names its layout. A file
// name with nothing the engine can make a key from takes a plain one
func sourceKey(name string) string {
	if k := proxyshop.SourceKey(name); k != "" && (k[0] < '0' || k[0] > '9') {
		return k
	}
	return "card"
}

// nextImportVersion is the version an import of a template takes: the first
// one, or a patch after the newest already installed, so an import never
// overwrites a bundle the app may be rendering from
func nextImportVersion(id string) string {
	major, minor, patch := 0, 1, 0
	if newest, ok := catalog.NewestCachedVersion(id); ok {
		core, _, _ := strings.Cut(strings.TrimPrefix(newest, "v"), "-")
		if _, err := fmt.Sscanf(core, "%d.%d.%d", &major, &minor, &patch); err == nil {
			patch++
		} else {
			major, minor, patch = 0, 1, 0
		}
	}
	v := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	for catalog.IsCached(id, v) {
		patch++
		v = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	}
	return v
}

// ImportProxyshop starts converting one or two Proxyshop PSDs, or a folder of
// them, into a template and installing it, and returns the job. The job reports
// each part as it is drawn and ends with the template it installed and the
// conversion's notes. One import runs at a time, since a real PSD takes minutes
// and gigabytes of memory
func (s *Service) ImportProxyshop(paths []string) (string, error) {
	if s.ws.Run.Active() {
		return "", apierr.New(apierr.Conflict, "Templates cannot change while a run is in progress")
	}
	plan, err := planImport(paths)
	if err != nil {
		return "", apierr.Wrap(apierr.BadRequest, "", err)
	}
	if !s.importing.CompareAndSwap(false, true) {
		return "", apierr.New(apierr.Conflict, "Another template import is still running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := s.ws.Jobs.New()
	j.SetCancel(cancel)
	go func() {
		defer s.importing.Store(false)
		defer cancel()
		report := func(step string, frac float64) { j.Emit(jobs.Event{Step: step, Frac: frac}) }
		got, err := s.importTemplate(ctx, plan, report)
		if err != nil {
			j.Emit(jobs.Event{Done: true, Err: err.Error()})
			return
		}
		j.Emit(jobs.Event{Done: true, Frac: 1, Imported: got})
	}()
	return j.ID(), nil
}

// CancelImport stops an import. The conversion stops before the next part it
// draws, and the job then ends with an error saying it was stopped
func (s *Service) CancelImport(jobID string) error {
	j, ok := s.ws.Jobs.Lookup(jobID)
	if !ok {
		return apierr.New(apierr.NotFound, "no such import")
	}
	j.Cancel()
	return nil
}

// Where on the progress bar reading the PSDs and drawing their parts end. The
// rest is packing and installing the bundle, and reading is quick beside drawing
const (
	readShare = 0.05
	drawShare = 0.95
)

// importTemplate converts the plan's PSDs and installs the template they make,
// reporting each stage
func (s *Service) importTemplate(ctx context.Context, plan importPlan, report func(step string, frac float64)) (*jobs.Imported, error) {
	names := make([]string, len(plan.files))
	for i, f := range plan.files {
		names[i] = f.Name
	}
	report("Reading "+strings.Join(names, " and ")+"…", 0)

	// Every file opened is closed here as well, since a stopped conversion
	// unwinds past the engine's own Close
	var mu sync.Mutex
	var opened []*psd.File
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range opened {
			f.Close()
		}
	}()
	open := func(name string) (*psd.File, error) {
		if ctx.Err() != nil {
			return nil, errImportStopped
		}
		path, ok := plan.paths[name]
		if !ok {
			return nil, fmt.Errorf("%s is not one of the chosen files", name)
		}
		f, err := psd.Open(path)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		opened = append(opened, f)
		mu.Unlock()
		return f, nil
	}

	ver := nextImportVersion(plan.id)
	opt := proxyshop.Options{ID: plan.id, Name: plan.name, Description: plan.description, Version: ver}
	if version.Version != "dev" && version.Version != "" {
		opt.MinEngine = strings.TrimPrefix(version.Version, "v")
	}
	opt.Progress = func(part string, done, total int) {
		if ctx.Err() != nil {
			panic(errImportStopped)
		}
		if part == "" || total == 0 {
			return
		}
		report(fmt.Sprintf("Drawing %s, part %d of %d…", part, done+1, total), readShare+(drawShare-readShare)*float64(done)/float64(total))
	}
	out, an, err := convert(plan.files, open, opt)
	if err != nil {
		if errors.Is(err, errImportStopped) || ctx.Err() != nil {
			return nil, errImportStopped
		}
		return nil, err
	}

	report("Installing the template…", drawShare)
	res, err := s.installConverted(ctx, out)
	if err != nil {
		return nil, err
	}
	notes := append(append([]string{}, an.Notes...), out.Notes...)
	return &jobs.Imported{ID: res.Template, Name: res.Title, Version: res.Version, Notes: notes}, nil
}

// convert runs the engine's conversion. A stop unwinds out of it as a panic
// from the progress hook, the one place the engine calls back while it draws.
// Any other panic is reported as a failed import, since a PSD the engine reads
// wrongly should not close the app
func convert(files []proxyshop.SourceFile, open proxyshop.Opener, opt proxyshop.Options) (out *proxyshop.Output, an *proxyshop.Analysis, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if r == errImportStopped {
			err = errImportStopped
			return
		}
		log.Printf("mimic-ui: the Proxyshop conversion panicked: %v\n%s", r, debug.Stack())
		err = fmt.Errorf("The conversion failed: %v", r)
	}()
	return proxyshop.Convert(files, open, opt)
}

// installConverted packs a converted template into a bundle and installs it the
// way an imported file is, so it is checked and listed like any other. The
// bundle is packed in a temporary file, so a failed or stopped import leaves
// nothing in the cache
func (s *Service) installConverted(ctx context.Context, out *proxyshop.Output) (ImportResult, error) {
	tmp, err := os.CreateTemp("", "mimic-proxyshop-*.mimic")
	if err != nil {
		return ImportResult{}, err
	}
	defer os.Remove(tmp.Name())
	if err := out.Bundle(tmp); err != nil {
		tmp.Close()
		return ImportResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return ImportResult{}, err
	}
	if ctx.Err() != nil {
		return ImportResult{}, errImportStopped
	}
	res, err := s.installBundle(tmp.Name(), false)
	if err != nil {
		return ImportResult{}, err
	}
	if res.Exists {
		return ImportResult{}, fmt.Errorf("%s %s was installed by something else while the PSD was converting", res.Template, res.Version)
	}
	return res, nil
}
