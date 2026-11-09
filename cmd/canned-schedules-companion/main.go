// Command canned-schedules-companion runs the lifecycle steps around canned schedules: issue feedback first, later
// promotion to the shelf, publishing and housekeeping.
//
// Usage:
//
//	canned-schedules-companion preview render [flags] request.md
//	canned-schedules-companion preview post --forge github --repo OWNER/REPO --issue N [flags] dir
//
// Every step splits into local verbs (render, prepare, check), which read and write files only, and forge verbs
// (post, open, publish, retire), which write to GitHub or GitLab, support --dry-run and take the forge explicitly.
//
// Exit codes: 0 when the request is valid (render) or the comment is posted (post), 1 when the request has errors,
// 2 when the command line, the environment or the forge is unusable.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

const (
	exitOK      = 0
	exitInvalid = 1
	exitUsage   = 2
)

const kib = 1 << 10

// maxInputBytes caps the input, like the core CLI. GitHub issue bodies are limited to 65,536 characters.
const maxInputBytes = 256 * kib

// File modes: the output directory and the files are meant to be read by others.
const (
	dirMode  = 0o750
	fileMode = 0o644
)

const usage = `usage: canned-schedules-companion preview render [flags] request.md
       canned-schedules-companion preview post --forge github --repo OWNER/REPO --issue N [flags] dir
       canned-schedules-companion --version
`

var errUsage = errors.New("usage")

// version is set by goreleaser (-ldflags -X main.version=…); without it the module version is used.
var version = ""

// overrides collects repeatable --set-config-at / --set-config-value values.
type overrides []string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Fprintf(stdout, "canned-schedules-companion %s (canned-schedules %s)\n", ownVersion(), coreVersion())

		return exitOK
	case len(args) >= 1 && (args[0] == "-h" || args[0] == "--help"):
		fmt.Fprint(stdout, usage)

		return exitOK
	case len(args) >= 2 && args[0] == "preview" && args[1] == "render":
		return previewRender(args[2:], stdout, stderr)
	case len(args) >= 2 && args[0] == "preview" && args[1] == "post":
		return previewPost(args[2:], stdout, stderr)
	}

	fmt.Fprint(stderr, usage)

	return exitUsage
}

func (o *overrides) String() string { return strings.Join(*o, ",") }

func (o *overrides) Set(s string) error {
	*o = append(*o, s)

	return nil
}

func previewRender(args []string, stdout, stderr io.Writer) int {
	opts, outDir, file, fiddle, err := parseRender(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	src, err := readInput(file)
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	res, err := preview.Render(src, file, opts, fiddle)
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	if err := writeAll(outDir, res.Files); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	fmt.Fprintf(stdout, "wrote %s/%s\n", outDir, preview.Comment)

	if !res.Valid {
		return exitInvalid
	}

	return exitOK
}

// parseRender reads the flags of preview render. The evaluation flags have the core CLI's names and meaning.
func parseRender(args []string, stderr io.Writer) (*cs.Options, string, string, *url.URL, error) {
	fs := flag.NewFlagSet("preview render", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		opts       cs.Options
		at, values overrides
		downsample int
	)

	outDir := fs.String("out-dir", "canned-schedule-preview", "where the files go")
	fs.IntVar(&opts.SampleInterval, "sample-interval", 1, "every time must be a multiple of it; must divide 60")
	// A value, not a boolean flag, as in the core CLI: `--require-base true` must not read "true" as the file.
	fs.Func("require-base", "true: the document must contain a BASE block (true or false)", func(s string) error {
		switch s {
		case "true", "false":
			opts.RequireBase = s == "true"

			return nil
		}

		return errors.New(`want "true" or "false"`)
	})
	fs.StringVar(&opts.Base, "base", "", "base state when the document has no BASE block")
	fs.StringVar(&opts.AlertPattern, "error-unless-alert-pattern", "",
		"every alert other than a schedule directive must match (Go syntax, anchored)")
	fs.Var(&at, "set-config-at", "config path to replace (repeatable, paired with --set-config-value)")
	fs.Var(&values, "set-config-value", "JSON value for the matching --set-config-at (repeatable)")
	fs.IntVar(&downsample, "preview-downsample", cs.DefaultDownsample, "preview buckets in minutes; must divide 60")
	fiddleRaw := fs.String("fiddle-link-base-uri", "", "opt-in: a preflight page you host; the comment links to it "+
		"with the schedule in the URL fragment")
	fiddleLock := fs.Bool("fiddle-link-lock", true, "lock the playground's settings (?lock=1), so the reader edits "+
		"only the schedule, checked with this run's flags; =false lets the reader change them")

	if err := fs.Parse(args); err != nil {
		return nil, "", "", nil, errUsage
	}

	if len(at) != len(values) {
		return nil, "", "", nil, fmt.Errorf("%d --set-config-at but %d --set-config-value: they must come in pairs",
			len(at), len(values))
	}

	var fiddle *url.URL

	if *fiddleRaw != "" {
		var err error
		if fiddle, err = preview.ParseFiddleBase(*fiddleRaw); err != nil {
			return nil, "", "", nil, err
		}

		preview.LockFiddle(fiddle, *fiddleLock)
	}

	for i := range at {
		opts.Config = append(opts.Config, cs.ConfigOverride{Path: at[i], Value: values[i]})
	}

	opts.Preview = &cs.PreviewOptions{Downsample: downsample}

	if fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)

		return nil, "", "", nil, errUsage
	}

	return &opts, *outDir, fs.Arg(0), fiddle, nil
}

// readInput opens file once, checks the opened file and reads at most maxInputBytes.
func readInput(file string) ([]byte, error) {
	f, err := os.Open(file) //nolint:gosec // Reading the file named on the command line is the point.
	if err != nil {
		return nil, fmt.Errorf("cannot open the input file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot read the input file: %w", err)
	}

	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("the input must be a regular file: %s", file)
	}

	src, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the input file: %w", err)
	}

	if len(src) > maxInputBytes {
		return nil, fmt.Errorf("the input file is larger than %d KiB", maxInputBytes/kib)
	}

	return src, nil
}

// writeAll removes every file the step can write, then writes files atomically, so a request that turned invalid
// leaves no stale preview behind.
func writeAll(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}

	for _, name := range preview.Names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	for name, data := range files {
		if err := writeFile(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}

	return nil
}

// writeFile writes through a temporary file in the same directory and a rename.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return err
	}

	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())

		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		return err
	}

	return os.Rename(tmp.Name(), path)
}

func ownVersion() string {
	if version != "" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		return trim(info.Main.Version)
	}

	return "dev"
}

func coreVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "github.com/iilei/canned-schedules" {
				return trim(d.Version)
			}
		}
	}

	return "unknown"
}

func trim(v string) string {
	if v = strings.TrimPrefix(v, "v"); v == "" || v == "(devel)" {
		return "dev"
	}

	return v
}
