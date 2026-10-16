// Command psdconvert turns Proxyshop PSD templates into mimic template source
// folders
//
//	psdconvert build -recipe r.json -psd-dir <folder> -o <template folder>
//	psdconvert analyze -psd-dir <folder> -id <id> -name <name> -o r.json a.psd [key=b.psd ...]
//	psdconvert convert -psd-dir <folder> -id <id> -name <name> -o <template folder> a.psd [key=b.psd ...]
//
// build cuts the parts a recipe names from its PSDs and writes the template's
// source folder, which the pack command bundles. analyze reads PSDs and writes
// the recipe that converts them, and convert does both, keeping the recipe it
// followed in recipe.json beside the template folder
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odevine/mimic/engine/proxyshop"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "psdconvert:", err)
		os.Exit(1)
	}
}

const usage = "usage: psdconvert build|analyze|convert, see the package documentation"

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(usage)
	}
	switch args[0] {
	case "build":
		return build(args[1:])
	case "analyze":
		return analyze(args[1:], false)
	case "convert":
		return analyze(args[1:], true)
	default:
		return fmt.Errorf("unknown command %q, %s", args[0], usage)
	}
}

func build(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	recipePath := fs.String("recipe", "", "the recipe to build")
	psdDir := fs.String("psd-dir", ".", "the folder holding the recipe's PSDs")
	out := fs.String("o", "", "the template folder to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *recipePath == "" || *out == "" {
		return fmt.Errorf(usage)
	}
	r, err := proxyshop.Load(*recipePath)
	if err != nil {
		return err
	}
	start := time.Now()
	o, err := proxyshop.Build(r, proxyshop.DirOpener(*psdDir))
	if err != nil {
		return err
	}
	for _, n := range o.Notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}
	if err := o.WriteDir(*out); err != nil {
		return err
	}
	fmt.Printf("wrote %d files to %s in %s\n", len(o.Files), *out, time.Since(start).Round(time.Millisecond))
	return nil
}

func analyze(args []string, convert bool) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	psdDir := fs.String("psd-dir", ".", "the folder holding the PSDs")
	id := fs.String("id", "", "the template's id")
	name := fs.String("name", "", "the template's name")
	desc := fs.String("description", "", "the template's description")
	out := fs.String("o", "", "the recipe to write, or with convert the template folder")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" || *id == "" || fs.NArg() == 0 {
		return fmt.Errorf(usage)
	}
	var files []proxyshop.SourceFile
	for _, a := range fs.Args() {
		key, file, ok := strings.Cut(a, "=")
		if !ok {
			key, file = "", a
		}
		files = append(files, proxyshop.SourceFile{Key: key, Name: file})
	}
	if *name == "" {
		*name = *id
	}
	opt := proxyshop.Options{ID: *id, Name: *name, Description: *desc}
	start := time.Now()
	open := proxyshop.DirOpener(*psdDir)
	if !convert {
		an, err := proxyshop.AnalyzeFiles(files, open, opt)
		if err != nil {
			return err
		}
		for _, n := range an.Notes {
			fmt.Fprintln(os.Stderr, "note:", n)
		}
		raw, err := an.Recipe.Marshal()
		if err != nil {
			return err
		}
		return os.WriteFile(*out, raw, 0o644)
	}
	o, an, err := proxyshop.Convert(files, open, opt)
	if an != nil {
		for _, n := range an.Notes {
			fmt.Fprintln(os.Stderr, "note:", n)
		}
	}
	if err != nil {
		return err
	}
	for _, n := range o.Notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}
	if err := o.WriteDir(*out); err != nil {
		return err
	}
	raw, err := an.Recipe.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Clean(*out)+".recipe.json", raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %d files to %s in %s\n", len(o.Files), *out, time.Since(start).Round(time.Millisecond))
	return nil
}
