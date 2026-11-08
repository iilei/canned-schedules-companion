package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewRender(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"preview", "render", "--out-dir", out, "../../testdata/office-hours.md"}, &stdout,
		&stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	for _, name := range []string{
		"report.md", "report.json", "comment.md", "schedule.json", "schedule.txt",
		"schedule.svg",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The same directory again with an invalid request: exit 1, and the previews of the earlier run are gone.
	if code := run([]string{"preview", "render", "--out-dir", out, "../../testdata/invalid-name.md"}, &stdout,
		&stderr); code != exitInvalid {
		t.Fatalf("exit %d for an invalid request", code)
	}

	for _, name := range []string{"schedule.json", "schedule.txt", "schedule.svg"} {
		if _, err := os.Stat(filepath.Join(out, name)); err == nil {
			t.Errorf("%s from the earlier run must be removed", name)
		}
	}

	if c, _ := os.ReadFile(filepath.Join(out, "comment.md")); !strings.Contains(string(c), "invalid-name") {
		t.Errorf("comment.md must report the error:\n%s", c)
	}
}

func TestEvaluationFlags(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	// The issue template's only alert is a NOTE; a pattern without NOTE makes it an error.
	if code := run([]string{
		"preview", "render", "--out-dir", out, "--error-unless-alert-pattern", "TIP",
		"--require-base", "false", "../../testdata/office-hours.md",
	}, &stdout, &stderr); code != exitInvalid {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}

	if c, _ := os.ReadFile(filepath.Join(out, "report.json")); !strings.Contains(string(c), "disallowed-alert") {
		t.Errorf("the alert pattern must reach the evaluation:\n%s", c)
	}
}

func TestFiddleLinkFlag(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"preview", "render", "--out-dir", out, "--fiddle-link-base-uri", "https://schedules.example.com/",
		"../../testdata/office-hours.md",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	if c, _ := os.ReadFile(filepath.Join(out, "comment.md")); !strings.Contains(string(c),
		"](https://schedules.example.com/#doc=") {
		t.Errorf("comment.md must link to the playground:\n%s", c)
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		nil,
		{"preview"},
		{"preview", "render"},
		{"preview", "render", "a.md", "b.md"},
		{"preview", "render", "--sample-interval", "7", "../../testdata/office-hours.md"},
		{"preview", "render", "--require-base", "yes", "../../testdata/office-hours.md"},
		{"preview", "render", "--set-config-at", "states", "../../testdata/office-hours.md"},
		{"preview", "render", "missing.md"},
		{"preview", "render", "--fiddle-link-base-uri", "http://insecure.example.com/", "../../testdata/office-hours.md"},
		{"preview", "render", "--fiddle-link-base-uri", "https://example.com/#x", "../../testdata/office-hours.md"},
		{"preview", "render", "../../testdata"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != exitOK ||
		!strings.HasPrefix(stdout.String(), "canned-schedules-companion ") {
		t.Errorf("exit %d: %q", code, stdout.String())
	}
}
