// Command canned-schedules-companion runs the lifecycle steps around canned schedules: issue feedback, the review of
// merge requests, the self-hosted playground, promotion of an approved issue to the catalog and publishing; later
// housekeeping.
//
// Usage:
//
//	canned-schedules-companion preview render [flags] request.md
//	canned-schedules-companion preview post --forge github|gitlab --repo OWNER/REPO --issue N [flags] dir
//	canned-schedules-companion review render [flags] file...
//	canned-schedules-companion review post --forge github|gitlab --repo P --merge-request N [flags] dir
//	canned-schedules-companion playground build --site-zip zip --forge github|gitlab --project P --branch B [flags] dir
//	canned-schedules-companion promote prepare [flags] issue.md
//	canned-schedules-companion promote open --forge github|gitlab --repo P --issue N --base-branch B [flags] dir
//	canned-schedules-companion publish fetch --forge github|gitlab --repo P --commit SHA [flags]
//	canned-schedules-companion publish prepare --merge-request-file merge-request.json [flags] file...
//
// Every step splits into local verbs (render, prepare, check), which read and write files only, and forge verbs
// (post, open, publish, retire), which write to GitHub or GitLab, support --dry-run and take the forge explicitly.
//
// Exit codes: 0 when the schedule is valid (render) or the comment is posted (post), 1 when the schedule has errors,
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

// stepAndVerb is how many arguments name a command ("preview render").
const stepAndVerb = 2

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
       canned-schedules-companion preview post --forge github|gitlab --repo OWNER/REPO --issue N [flags] dir
       canned-schedules-companion playground build --site-zip zip --forge github|gitlab --project P --branch B [flags] dir
       canned-schedules-companion review render [flags] file...
       canned-schedules-companion review post --forge github|gitlab --repo P --merge-request N [flags] dir
       canned-schedules-companion promote prepare [flags] issue.md
       canned-schedules-companion promote open --forge github|gitlab --repo P --issue N --base-branch B [flags] dir
       canned-schedules-companion publish fetch --forge github|gitlab --repo P --commit SHA [flags]
       canned-schedules-companion publish prepare --merge-request-file merge-request.json [flags] file...
       canned-schedules-companion --version
`

var errUsage = errors.New("usage")

// commands are the steps' verbs, by "<step> <verb>".
var commands = map[string]func(args []string, stdout, stderr io.Writer) int{
	"preview render":   previewRender,
	"preview post":     previewPost,
	"playground build": playgroundBuild,
	"review render":    reviewRender,
	"review post":      reviewPost,
	"promote prepare":  promotePrepare,
	"promote open":     promoteOpen,
	"publish fetch":    publishFetch,
	"publish prepare":  publishPrepare,
}

// version is set by goreleaser (-ldflags -X main.version=…); without it the module version is used.
var version = ""

// overrides collects repeatable --set-config-at / --set-config-value values.
type overrides []string

// evalFlags are the evaluation flags, with the core CLI's names and meaning, shared by the commands that check.
type evalFlags struct {
	at, values overrides
	opts       cs.Options
	downsample int
}

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
	case len(args) >= stepAndVerb:
		if cmd, ok := commands[args[0]+" "+args[1]]; ok {
			return cmd(args[stepAndVerb:], stdout, stderr)
		}
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

	res, err := preview.Render(src, file, opts, fiddle, "canned-schedules-companion@"+ownVersion())
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	if err := writeAll(outDir, res.Files); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	fmt.Fprintf(stdout, "wrote %s/%s\n", outDir, preview.Comment)

	if !res.Schedule {
		fmt.Fprintf(stdout, "%s holds no schedule directive, so there is nothing to check\n", file)

		return exitOK
	}

	if !res.Valid {
		return exitInvalid
	}

	return exitOK
}

// parseRender reads the flags of preview render. The evaluation flags have the core CLI's names and meaning.
func parseRender(args []string, stderr io.Writer) (*cs.Options, string, string, *url.URL, error) {
	fs := flag.NewFlagSet("preview render", flag.ContinueOnError)
	fs.SetOutput(stderr)

	ev := addEvalFlags(fs)
	outDir := fs.String("out-dir", "canned-schedule-preview", "where the files go")
	fiddleRaw := fs.String("fiddle-link-base-uri", "", "opt-in: a preflight page you host; the comment links to it "+
		"with the schedule in the URL fragment")
	fiddleLock := fs.Bool("fiddle-link-lock", true, "lock the playground's settings (?lock=1), so the reader edits "+
		"only the schedule, checked with this run's flags; =false lets the reader change them")

	if err := fs.Parse(args); err != nil {
		return nil, "", "", nil, errUsage
	}

	opts, err := ev.options()
	if err != nil {
		return nil, "", "", nil, err
	}

	var fiddle *url.URL

	if *fiddleRaw != "" {
		if fiddle, err = preview.ParseFiddleBase(*fiddleRaw); err != nil {
			return nil, "", "", nil, err
		}

		preview.LockFiddle(fiddle, *fiddleLock)
	}

	if fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)

		return nil, "", "", nil, errUsage
	}

	return opts, *outDir, fs.Arg(0), fiddle, nil
}

func addEvalFlags(fs *flag.FlagSet) *evalFlags {
	var e evalFlags

	fs.IntVar(&e.opts.SampleInterval, "sample-interval", 1, "every time must be a multiple of it; must divide 60")
	// A value, not a boolean flag, as in the core CLI: `--require-base true` must not read "true" as the file.
	fs.Func("require-base", "true: the document must contain a BASE block (true or false)", func(s string) error {
		switch s {
		case "true", "false":
			e.opts.RequireBase = s == "true"

			return nil
		}

		return errors.New(`want "true" or "false"`)
	})
	fs.StringVar(&e.opts.Base, "base", "", "base state when the document has no BASE block")
	fs.StringVar(&e.opts.AlertPattern, "error-unless-alert-pattern", "",
		"every alert other than a schedule directive must match (Go syntax, anchored)")
	fs.Var(&e.at, "set-config-at", "config path to replace (repeatable, paired with --set-config-value)")
	fs.Var(&e.values, "set-config-value", "JSON value for the matching --set-config-at (repeatable)")
	fs.IntVar(&e.downsample, "preview-downsample", cs.DefaultDownsample, "preview buckets in minutes; must divide 60")

	return &e
}

// options pairs the overrides and plans the preview.
func (e *evalFlags) options() (*cs.Options, error) {
	if len(e.at) != len(e.values) {
		return nil, fmt.Errorf("%d --set-config-at but %d --set-config-value: they must come in pairs",
			len(e.at), len(e.values))
	}

	opts := e.opts
	for i := range e.at {
		opts.Config = append(opts.Config, cs.ConfigOverride{Path: e.at[i], Value: e.values[i]})
	}

	opts.Preview = &cs.PreviewOptions{Downsample: e.downsample}

	return &opts, nil
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

// writeAll removes every file the step can write, then writes files atomically, so a schedule that turned invalid
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
