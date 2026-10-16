// Command tplmerge combines built template source folders that cover different
// card kinds into one family, or pulls one apart again, so the result can be
// fixed by hand afterward
//
//	tplmerge merge -out <dir> -id <id> -name <name> [-version v] [-force] [-strict] <src1> <src2> ...
//	tplmerge split -out <dir> -id <id> -name <name> [-version v] [-force] <src> <layout1> [layout2 ...]
//
// merge combines two or more template source folders (a template.json and a
// layouts folder, the form engine/author.NewDraft reads and engine/cmd/pack
// builds) into one at -out: every source's layouts, and only the parts they
// draw with. A part name two sources define differently is renamed for each
// source that uses it, with its folder and layout references moved along with
// it, so both keep their own content rather than one silently winning; -strict
// refuses the merge instead, naming every part and source that disagree. split
// pulls the named layouts, and only the parts they use, out of one source
// folder into a new one. Both refuse to write into a non-empty -out unless
// -force is given, and leave every source folder untouched
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

const usage = `usage:
  tplmerge merge -out <dir> -id <id> -name <name> [-version v] [-force] [-strict] <src1> <src2> ...
  tplmerge split -out <dir> -id <id> -name <name> [-version v] [-force] <src> <layout1> [layout2 ...]
`

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "merge":
		return runMerge(rest)
	case "split":
		return runSplit(rest)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "tplmerge: %q is not merge or split\n%s", cmd, usage)
		return 2
	}
}

func runMerge(args []string) int {
	fs := flag.NewFlagSet("tplmerge merge", flag.ContinueOnError)
	out := fs.String("out", "", "the folder to write the merged template into")
	id := fs.String("id", "", "the merged template's id")
	name := fs.String("name", "", "the merged template's name")
	version := fs.String("version", "", "the merged template's version, 0.1.0 if left out")
	force := fs.Bool("force", false, "write into -out even if it already has files in it")
	strict := fs.Bool("strict", false, "refuse a part name two sources define differently, instead of renaming it for each")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	srcs := fs.Args()
	if len(srcs) < 2 {
		fmt.Fprintf(os.Stderr, "tplmerge merge: needs at least two source folders\n%s", usage)
		return 2
	}
	summary, err := mergeSources(srcs, mergeOptions{out: *out, id: *id, name: *name, version: *version, force: *force, strict: *strict})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tplmerge merge:", err)
		return 1
	}
	fmt.Println(summary)
	return 0
}

func runSplit(args []string) int {
	fs := flag.NewFlagSet("tplmerge split", flag.ContinueOnError)
	out := fs.String("out", "", "the folder to write the split template into")
	id := fs.String("id", "", "the split template's id")
	name := fs.String("name", "", "the split template's name")
	version := fs.String("version", "", "the split template's version, 0.1.0 if left out")
	force := fs.Bool("force", false, "write into -out even if it already has files in it")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintf(os.Stderr, "tplmerge split: needs a source folder and at least one layout name\n%s", usage)
		return 2
	}
	src, layouts := rest[0], rest[1:]
	summary, err := splitSource(src, layouts, splitOptions{out: *out, id: *id, name: *name, version: *version, force: *force})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tplmerge split:", err)
		return 1
	}
	fmt.Println(summary)
	return 0
}
