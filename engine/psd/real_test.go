package psd

import (
	"crypto/md5"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealDocuments opens every document under MIMIC_PSD_DIR, such as a checkout
// of Proxyshop's templates, and skips when it is unset
func TestRealDocuments(t *testing.T) {
	dir := os.Getenv("MIMIC_PSD_DIR")
	if dir == "" {
		t.Skip("MIMIC_PSD_DIR is not set")
	}
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".psd") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no documents under %s", dir)
	}
	for _, p := range paths {
		t.Run(strings.TrimPrefix(p, dir), func(t *testing.T) {
			f, err := Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Width <= 0 || f.Height <= 0 || len(f.Layers) == 0 {
				t.Fatalf("%dx%d with %d layers", f.Width, f.Height, len(f.Layers))
			}
			for _, l := range f.Layers {
				for _, e := range l.Problems {
					t.Errorf("%s: %v", strings.Join(l.Path(), "/"), e)
				}
				if l.Kind == KindText && (l.Text == nil || l.Text.Engine == nil) {
					t.Errorf("%s: text without engine data", strings.Join(l.Path(), "/"))
				}
			}
		})
	}
}

func digest(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

// TestRealNormal checks pixels of Proxyshop's normal template against digests
// computed with psd-tools
func TestRealNormal(t *testing.T) {
	dir := os.Getenv("MIMIC_PSD_DIR")
	if dir == "" {
		t.Skip("MIMIC_PSD_DIR is not set")
	}
	path := filepath.Join(dir, "normal.psd")
	if _, err := os.Stat(path); err != nil {
		t.Skip("normal.psd is not in MIMIC_PSD_DIR")
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Width != 3264 || f.Height != 4440 || f.Resolution != 1200 {
		t.Errorf("%dx%d at %v ppi", f.Width, f.Height, f.Resolution)
	}
	name := f.Find("Text and Icons", "Card Name")
	if name == nil || name.Kind != KindText || len(name.Text.Runs) == 0 || name.Text.Runs[0].Font == "" {
		t.Fatalf("card name %+v", name)
	}

	land := f.Find("Nyx", "Land")
	if land == nil || land.ID != 19776 {
		t.Fatalf("Nyx/Land %+v", land)
	}
	img, err := land.Image()
	if err != nil {
		t.Fatal(err)
	}
	if got := digest(img.Pix); got != "f373f80c7a4758e12de0841a305970a7" {
		t.Errorf("Nyx/Land pixels digest %s", got)
	}

	shadows := f.Find("Shadows")
	if shadows == nil || shadows.Mask == nil {
		t.Fatalf("Shadows %+v", shadows)
	}
	img, err = shadows.Image()
	if err != nil {
		t.Fatal(err)
	}
	if got := digest(img.Pix); got != "a3a0187b3de80e820e2d87853f0f9f8a" {
		t.Errorf("Shadows pixels digest %s", got)
	}
	mask, err := shadows.MaskImage()
	if err != nil {
		t.Fatal(err)
	}
	if mask.Rect.Min.X != 623 || mask.Rect.Min.Y != 243 || mask.Rect.Max.X != 2642 || mask.Rect.Max.Y != 346 || shadows.Mask.DefaultColor != 255 {
		t.Errorf("Shadows mask %v default %d", mask.Rect, shadows.Mask.DefaultColor)
	}
	if got := digest(mask.Pix); got != "c9a2a89e0ba70c173e9bbfcb578783c5" {
		t.Errorf("Shadows mask digest %s", got)
	}

	comp, err := f.Composite()
	if err != nil {
		t.Fatal(err)
	}
	if got := digest(comp.Pix); got != "e3c7d62ff47eb9485769c47efc0feaee" {
		t.Errorf("composite digest %s", got)
	}
}
