package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/iilei/canned-schedules-companion/internal/playground"
)

const (
	mib = 1 << 20
	// maxSiteBytes caps what the site zip may unpack to; the page is about 14 MB.
	maxSiteBytes = 256 * mib
	siteJSON     = "site.json"
)

var (
	errSiteZip = errors.New("not the core's site zip")
	errOutDir  = errors.New("must be new or empty, so no stale files of an earlier page stay")
)

type playgroundFlags struct {
	siteZip   string
	repoRoot  string
	argsFile  string
	outDir    string
	opts      playground.Options
	noCatalog bool
}

// playgroundBuild unpacks the core's preflight page and writes its site.json: the local part of publishing a
// self-hosted playground. The Action and the GitLab component download and verify the zip before, and publish the
// directory after.
func playgroundBuild(args []string, stdout, stderr io.Writer) int {
	f, err := parsePlayground(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := buildPlayground(f, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	return exitOK
}

func buildPlayground(f *playgroundFlags, stdout io.Writer) error {
	if f.argsFile != "" {
		raw, err := os.ReadFile(f.argsFile)
		if err != nil {
			return fmt.Errorf("read the args file: %w", err)
		}

		f.opts.Args = playground.ReadArgs(string(raw))
	}

	if !f.noCatalog {
		entries, err := os.ReadDir(filepath.Join(f.repoRoot, filepath.FromSlash(f.opts.CatalogDir)))
		if err != nil {
			return fmt.Errorf("read the catalog (--no-catalog for none yet): %w", err)
		}

		var files []string
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, e.Name())
			}
		}

		f.opts.Names = playground.Names(files)
	}

	site, err := playground.SiteJSON(&f.opts)
	if err != nil {
		return err
	}

	if err := emptyDir(f.outDir); err != nil {
		return err
	}

	n, err := unzip(f.siteZip, f.outDir)
	if err != nil {
		return err
	}

	if err := writeFile(filepath.Join(f.outDir, siteJSON), site); err != nil {
		return err
	}

	schedules := "schedules"
	if len(f.opts.Names) == 1 {
		schedules = "schedule"
	}

	fmt.Fprintf(stdout, "wrote %s: the page (%d files) and %s with %d %s\n", f.outDir, n, siteJSON,
		len(f.opts.Names), schedules)

	return nil
}

// emptyDir creates dir, or accepts it when it exists and is empty.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return os.MkdirAll(dir, dirMode)
	case err != nil:
		return fmt.Errorf("--out-dir %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("--out-dir %s: %w", dir, errOutDir)
	}

	return nil
}

// unzip extracts the site zip into dir: regular files only, no absolute paths or "..", at most maxSiteBytes, and an
// index.html at the root, as the core's release attaches it.
func unzip(file, dir string) (int, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return 0, fmt.Errorf("--site-zip: %w", err)
	}
	defer zr.Close()

	var total uint64

	n, index := 0, false

	for _, zf := range zr.File {
		name := zf.Name
		if strings.HasSuffix(name, "/") && zf.FileInfo().IsDir() {
			continue
		}

		clean := path.Clean(name)
		if clean != name || path.IsAbs(name) || strings.HasPrefix(clean, "../") || clean == ".." ||
			strings.Contains(name, "\\") || !zf.Mode().IsRegular() {
			return n, fmt.Errorf("--site-zip: %q: %w", name, errSiteZip)
		}

		if total += zf.UncompressedSize64; total > maxSiteBytes {
			return n, fmt.Errorf("--site-zip: larger than %d MiB unpacked: %w", maxSiteBytes/mib, errSiteZip)
		}

		if err := extract(zf, filepath.Join(dir, filepath.FromSlash(clean))); err != nil {
			return n, err
		}

		index = index || clean == "index.html"
		n++
	}

	if !index {
		return n, fmt.Errorf("--site-zip: no index.html at the root: %w", errSiteZip)
	}

	return n, nil
}

func extract(zf *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return err
	}

	r, err := zf.Open()
	if err != nil {
		return err
	}
	defer r.Close()

	w, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode) //nolint:gosec // Checked above.
	if err != nil {
		return err
	}

	// The declared size was checked; LimitReader keeps a lying header from writing more.
	if _, err := io.Copy(w, io.LimitReader(r, int64(zf.UncompressedSize64))); err != nil { //nolint:gosec // Capped.
		_ = w.Close()

		return err
	}

	return w.Close()
}

func parsePlayground(args []string, stderr io.Writer) (*playgroundFlags, error) {
	fs := flag.NewFlagSet("playground build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f playgroundFlags

	fs.StringVar(&f.siteZip, "site-zip", "", "the core's canned-schedules-site_<version>.zip, downloaded and verified "+
		"(required)")
	fs.StringVar(&f.opts.Forge, "forge", "", "where proposals go: github or gitlab (required, never guessed)")
	fs.StringVar(&f.opts.Server, "server-url", "", "the forge's web address (default: "+playground.DefaultGitHubServer+
		" or "+playground.DefaultGitLabServer+")")
	fs.StringVar(&f.opts.Project, "project", "", "the catalog's project: OWNER/REPO, on GitLab GROUP/PROJECT "+
		"(required)")
	fs.StringVar(&f.opts.Branch, "branch", "", "the branch proposals target, usually the default branch (required)")
	fs.StringVar(&f.opts.CatalogDir, "catalog-dir", "schedules", "the catalog's folder in the repository")
	fs.StringVar(&f.repoRoot, "repo-root", ".", "the checked-out repository, where --catalog-dir is read")
	fs.BoolVar(&f.noCatalog, "no-catalog", false, "the catalog has no schedules yet: don't read --catalog-dir")
	fs.StringVar(&f.argsFile, "args-file", "", "the flags the catalog's CI checks with, as canned-schedules.args")
	fs.StringVar(&f.opts.Name, "name", "", "shown in the page's header")
	fs.BoolVar(&f.opts.Fork, "fork", false, "link readers who aren't members to the project's fork page")

	if err := fs.Parse(args); err != nil {
		return nil, errUsage
	}

	switch {
	case f.siteZip == "" || f.opts.Forge == "" || f.opts.Project == "" || f.opts.Branch == "":
		return nil, errors.New("--site-zip, --forge, --project and --branch are required")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, errUsage
	}

	f.outDir = fs.Arg(0)

	return &f, nil
}
