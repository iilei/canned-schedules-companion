package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

// publishPrepare renders the schedules a merged merge request changed and lays out what gets published.
func publishPrepare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)

	ev := addEvalFlags(fs)
	input := fs.String("merge-request-file", "", "the merged merge request and its review, as publish fetch "+
		"writes it (merge-request.json); required")
	pathTmpl := fs.String("path-template", publish.DefaultPath, "each schedule's folder under the output directory")
	metaTmpl := fs.String("metadata", publish.DefaultMetadata, "metadata, one key=value template per line")
	force := fs.Bool("force-overwrite", false, "let a later schedule overwrite a file an earlier one wrote, instead "+
		"of failing")
	outDir := fs.String("out-dir", "canned-schedule-publish", "where the files go; must be empty or missing")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	if *input == "" {
		fmt.Fprintln(stderr, "canned-schedules-companion: --merge-request-file is required")

		return exitUsage
	}

	res, err := preparePublish(ev, *input, *pathTmpl, *metaTmpl, *force, fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "canned-schedules-companion: warning:", w)
	}

	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p)
		}

		return exitInvalid
	}

	if err := writeTree(*outDir, res.Files); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	for i := range res.Mismatches {
		m := &res.Mismatches[i]
		fmt.Fprintf(stdout, "%s: published, but not as reviewed (%s)\n", m.Path, m.Reason)
	}

	fmt.Fprintf(stdout, "wrote %s/%s for %d schedule(s)\n", *outDir, publish.Manifest, len(fs.Args()))

	return exitOK
}

func preparePublish(ev *evalFlags, input, pathTmpl, metaTmpl string, force bool, names []string) (
	*publish.Result, error,
) {
	eval, err := ev.options()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(input) //nolint:gosec // Reading the file named on the command line is the point.
	if err != nil {
		return nil, err
	}

	in, err := publish.ParseInput(raw)
	if err != nil {
		return nil, err
	}

	p, err := publish.Parse(pathTmpl)
	if err != nil {
		return nil, err
	}

	m, err := publish.ParseMetadata(metaTmpl)
	if err != nil {
		return nil, err
	}

	files := make([]publish.File, 0, len(names))

	for _, name := range names {
		src, err := readInput(name)
		if err != nil {
			return nil, err
		}

		files = append(files, publish.File{Path: filepath.ToSlash(name), Src: src})
	}

	return publish.Prepare(in, files, &publish.Options{
		Eval: eval, Path: p, Metadata: m, ForceOverwrite: force,
		Tool: "canned-schedules-companion " + ownVersion(), Core: "canned-schedules " + coreVersion(),
	})
}

// writeTree writes files (paths with /) under dir, which must be empty or missing, so nothing stale is published.
func writeTree(dir string, files map[string][]byte) error {
	entries, err := os.ReadDir(dir)

	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty: publish prepare writes into an empty or missing directory", dir)
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	for _, p := range paths {
		target := filepath.Join(dir, filepath.FromSlash(p))

		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return err
		}

		if err := writeFile(target, files[p]); err != nil {
			return err
		}
	}

	return nil
}
