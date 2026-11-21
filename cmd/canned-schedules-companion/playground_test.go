package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip writes a zip with the given entries (name → content; a name ending in / is a directory).
func writeZip(t *testing.T, entries map[string]string, symlink string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "site.zip")

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		_, _ = w.Write([]byte(content))
	}

	if symlink != "" {
		h := &zip.FileHeader{Name: symlink}
		h.SetMode(os.ModeSymlink | 0o777)

		w, _ := zw.CreateHeader(h)
		_, _ = w.Write([]byte("/etc/passwd"))
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return file
}

func catalogRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	dir := filepath.Join(repo, "schedules")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{"office-hours.canned-schedule.md", "on-call.canned-schedule.md", "on-call.canned-schedule.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(repo, "canned-schedules.args"), []byte("# CI's flags\n--require-base\ntrue\n"),
		0o600); err != nil {
		t.Fatal(err)
	}

	return repo
}

func TestPlaygroundBuild(t *testing.T) {
	t.Parallel()

	site := writeZip(
		t,
		map[string]string{"index.html": "<!doctype html>", "app.js": "//", "fonts/": "", "fonts/a.woff2": "f"},
		"",
	)
	repo := catalogRepo(t)
	out := filepath.Join(t.TempDir(), "public")

	var stdout, stderr bytes.Buffer

	args := []string{
		"playground", "build", "--site-zip", site, "--forge", "gitlab", "--server-url", "https://gitlab.example.com",
		"--project", "acme/catalog", "--branch", "main", "--repo-root", repo,
		"--args-file", filepath.Join(repo, "canned-schedules.args"), "--name", "Acme schedules", "--fork",
	}

	if code := run(append(args, out), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	for _, f := range []string{"index.html", "app.js", "fonts/a.woff2", "site.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}

	raw, _ := os.ReadFile(filepath.Join(out, "site.json"))

	var s struct {
		Name    string   `json:"name"`
		Args    []string `json:"args"`
		Catalog struct {
			Names []string `json:"names"`
		} `json:"catalog"`
	}

	if err := json.Unmarshal(
		raw,
		&s,
	); err != nil || s.Name != "Acme schedules" ||
		strings.Join(s.Args, " ") != "--require-base true" ||
		strings.Join(s.Catalog.Names, ",") != "office-hours,on-call" {
		t.Errorf("site.json %s (%v)", raw, err)
	}

	if !strings.Contains(stdout.String(), "2 schedules") {
		t.Errorf("stdout %q", stdout.String())
	}

	// The same directory again: stale files of an earlier page must not mix with a new one.
	if code := run(
		append(args, out),
		&stdout,
		&stderr,
	); code != exitUsage ||
		!strings.Contains(stderr.String(), "new or empty") {
		t.Errorf("non-empty out-dir: exit %d, %q", code, stderr.String())
	}
}

func TestPlaygroundBuildRefusesZips(t *testing.T) {
	t.Parallel()

	repo := catalogRepo(t)

	for name, c := range map[string]struct {
		entries map[string]string
		symlink string
	}{
		"dot dot":  {entries: map[string]string{"index.html": "x", "../evil": "x"}},
		"absolute": {entries: map[string]string{"index.html": "x", "/etc/evil": "x"}},
		"symlink":  {entries: map[string]string{"index.html": "x"}, symlink: "link"},
		"no index": {entries: map[string]string{"app.js": "x"}},
		"nested":   {entries: map[string]string{"site/index.html": "x"}},
	} {
		out := filepath.Join(t.TempDir(), "public")

		var stdout, stderr bytes.Buffer
		if code := run([]string{
			"playground",
			"build",
			"--site-zip",
			writeZip(t, c.entries, c.symlink),
			"--forge",
			"github",
			"--project",
			"o/r",
			"--branch",
			"main",
			"--repo-root",
			repo,
			out,
		}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "site zip") {
			t.Errorf("%s: exit %d, %q", name, code, stderr.String())
		}
	}
}

func TestPlaygroundBuildCatalog(t *testing.T) {
	t.Parallel()

	site := writeZip(t, map[string]string{"index.html": "x"}, "")
	base := []string{
		"playground",
		"build",
		"--site-zip",
		site,
		"--forge",
		"github",
		"--project",
		"o/r",
		"--branch",
		"main",
	}

	var stdout, stderr bytes.Buffer

	// A catalog folder that doesn't exist yet is an error, unless --no-catalog says so.
	if code := run(
		append(base, "--repo-root", t.TempDir(), filepath.Join(t.TempDir(), "a")),
		&stdout,
		&stderr,
	); code != exitUsage {
		t.Errorf("missing catalog: exit %d", code)
	}

	if code := run(append(base, "--no-catalog", filepath.Join(t.TempDir(), "b")), &stdout, &stderr); code != exitOK {
		t.Errorf("--no-catalog: exit %d: %s", code, stderr.String())
	}

	if code := run([]string{"playground", "build", "--forge", "github", "x"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("missing flags: exit %d", code)
	}
}
