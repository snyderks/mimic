package templates

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odevine/mimic/engine/template"
	"github.com/odevine/mimic/ui/internal/apierr"
	"github.com/odevine/mimic/ui/internal/catalog"
	"github.com/odevine/mimic/ui/internal/pipeline"
)

// bundleVersion is what a version of an imported bundle may look like, since it becomes
// the name of a file in the cache
var bundleVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// ImportRequest is a .mimic file the person chose. Replace says to take the place of a
// bundle of the same template and version that is installed and is not the same file
type ImportRequest struct {
	Path    string `json:"path"`
	Replace bool   `json:"replace"`
}

// ImportResult says what was installed from the file. Same is set when the file is a
// bundle that was installed already, so nothing was written, and Replaced when it took
// the place of another. Exists is set when a different bundle of the same template and
// version is installed and nothing was written, which the page asks about before it sends
// the request again with Replace. Active says the template the app draws with was opened
// again
type ImportResult struct {
	Template string `json:"template"`
	Version  string `json:"version"`
	Title    string `json:"title"`
	Same     bool   `json:"same"`
	Replaced bool   `json:"replaced"`
	Exists   bool   `json:"exists"`
	Active   bool   `json:"active"`
}

// Import installs a .mimic file as a template the app can use, by checking that this
// version of the app can read it and putting it in the cache the installed templates are
// read from, where it is listed in the library beside the ones that were downloaded. It
// is not made the active template
func (s *Service) Import(req ImportRequest) (ImportResult, error) {
	if s.ws.Run.Active() {
		return ImportResult{}, apierr.New(apierr.Conflict, "Templates cannot change while a run is in progress")
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return ImportResult{}, apierr.New(apierr.BadRequest, "Choose a template file to import")
	}
	return s.installBundle(path, req.Replace)
}

// installBundle checks that a .mimic file is a template this version of the app
// can keep and read, and copies it into the cache under its id and version. A
// file imported by hand and a template converted from Proxyshop both land in the
// library this way
func (s *Service) installBundle(path string, replace bool) (ImportResult, error) {
	zp, err := template.NewZipAssetProvider(path)
	if err != nil {
		return ImportResult{}, apierr.Wrap(apierr.BadRequest, "That file is not a template this version of mimic can read", err)
	}
	header := zp.Header()
	defer zp.Close()
	if !pipeline.ValidID(header.ID) {
		return ImportResult{}, apierr.Newf(apierr.BadRequest, "The template's id %q is not one the app can keep, which looks like owner.name in lowercase letters, digits and hyphens", header.ID)
	}
	if !bundleVersion.MatchString(header.Version) {
		return ImportResult{}, apierr.Newf(apierr.BadRequest, "The template's version %q is not one the app can keep, which looks like 1.2.3", header.Version)
	}
	tm, ds, err := template.Load(zp)
	if err != nil {
		reason := err.Error()
		for _, d := range ds {
			reason = d.String()
			break
		}
		return ImportResult{}, apierr.Newf(apierr.BadRequest, "The template does not load: %s", reason)
	}
	dest, err := catalog.BundlePath(header.ID, header.Version)
	if err != nil {
		return ImportResult{}, apierr.Wrap(apierr.Internal, "Could not find the folder for templates", err)
	}
	res := ImportResult{Template: header.ID, Version: header.Version, Title: tm.Info().Name}
	if _, err := os.Stat(dest); err == nil {
		same, err := sameFile(path, dest)
		if err != nil {
			return ImportResult{}, apierr.Wrap(apierr.Internal, "Could not compare the file with the installed template", err)
		}
		switch {
		case same:
			res.Same = true
			return res, nil
		case !replace:
			res.Exists = true
			return res, nil
		}
		res.Replaced = true
	}
	if err := copyBundle(path, dest); err != nil {
		return ImportResult{}, apierr.Wrap(apierr.Internal, "Could not install the template", err)
	}
	s.ws.Pipe.Forget(header.ID)
	if name, ver := s.ws.Active(); name == header.ID && ver == header.Version {
		at, err := pipeline.FromVersion(context.Background(), header.ID, header.Version, nil)
		if err != nil {
			return ImportResult{}, apierr.Wrap(apierr.Internal, "The template was installed but could not be opened", err)
		}
		s.ws.SetActiveTemplate(at)
		res.Active = true
	}
	return res, nil
}

// sameFile reports whether two files hold the same bytes
func sameFile(a, b string) (bool, error) {
	ha, err := hashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := hashFile(b)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func hashFile(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// copyBundle copies a file to dest so that a copy that is half made is never left where a
// bundle is read from
func copyBundle(src, dest string) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".import-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
