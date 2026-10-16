package proxyshop

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"github.com/odevine/mimic/engine/author"
)

// SourceFile names a PSD for Convert, by its path inside the folder an Opener
// reads. An empty Key is made from the file's name
type SourceFile struct {
	Key  string
	Name string
}

// SourceKey makes a recipe source key from a PSD's file name
func SourceKey(name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	return slug(strings.TrimSuffix(base, path.Ext(base)))
}

// AnalyzeFiles opens PSDs and analyzes them
func AnalyzeFiles(files []SourceFile, open Opener, opt Options) (*Analysis, error) {
	var srcs []Source
	defer func() {
		for _, s := range srcs {
			s.File.Close()
		}
	}()
	for _, sf := range files {
		key := sf.Key
		if key == "" {
			key = SourceKey(sf.Name)
		}
		f, err := open(sf.Name)
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", sf.Name, err)
		}
		srcs = append(srcs, Source{Key: key, Name: sf.Name, File: f})
	}
	return Analyze(srcs, opt)
}

// Convert analyzes PSDs and builds the template the analysis describes: the
// whole conversion from Proxyshop files to a template's source folder. The
// recipe it followed comes back with the output so it can be kept and edited
func Convert(files []SourceFile, open Opener, opt Options) (*Output, *Analysis, error) {
	an, err := AnalyzeFiles(files, open, opt)
	if err != nil {
		return nil, nil, err
	}
	out, err := BuildProgress(an.Recipe, open, opt.Progress)
	if err != nil {
		return nil, an, err
	}
	return out, an, nil
}

// FS serves the output as a folder, the form author.Build and author.NewDraft
// read a template's source from
func (o *Output) FS() fs.FS {
	m := fstest.MapFS{}
	for p, data := range o.Files {
		m[p] = &fstest.MapFile{Data: data, Mode: 0o644}
	}
	return m
}

// Bundle packs the output into a .mimic bundle, as the pack command does with
// a source folder
func (o *Output) Bundle(w io.Writer) error {
	built, ds := author.Build(o.FS(), author.Options{})
	if err := ds.Err(); err != nil {
		return fmt.Errorf("the converted template does not build: %w", err)
	}
	return built.WriteZip(w)
}
