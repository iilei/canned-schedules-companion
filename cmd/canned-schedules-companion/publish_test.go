package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const publishInput = `{"merge-request": {"forge": "gitlab", "host": "gitlab.example.com", "project": "ops/catalog",
  "number": 7, "created-at": "2026-11-20T09:15:00Z", "merged-at": "2026-11-22T14:30:00Z"}, "review": null}`

const publishNight = "# Night Shift\n\n> [!SCHEDULE-STATE] Medium\n> - `22:00` to `06:00` MON-FRI\n"

func TestPublishPrepare(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	mr := filepath.Join(repo, "merge-request.json")
	sched := filepath.Join(repo, "night-shift.canned-schedule.md")

	for p, c := range map[string]string{mr: publishInput, sched: publishNight} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "publish")
	args := []string{
		"publish", "prepare", "--merge-request-file", mr, "--path-template", "{slug}/{mr}",
		"--out-dir", out, sched,
	}

	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	for _, name := range []string{
		"manifest.json", "night-shift/7/night-shift.canned-schedule.json",
		"night-shift/7/night-shift.canned-schedule.provenance.json",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// No review comment: published, and stdout says so (the forge part posts the note).
	if !strings.Contains(stdout.String(), "not as reviewed (no-review)") {
		t.Errorf("stdout %q", stdout.String())
	}

	// The same directory again: refused, so nothing stale is published.
	if code := run(args, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "not empty") {
		t.Errorf("second run: exit %d, %q", code, stderr.String())
	}

	// An invalid schedule: exit 1 and nothing written.
	bad := filepath.Join(repo, "bad.canned-schedule.md")
	if err := os.WriteFile(bad, []byte("# Bad!\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out2 := filepath.Join(t.TempDir(), "publish")
	if code := run([]string{"publish", "prepare", "--merge-request-file", mr, "--out-dir", out2, bad}, &stdout,
		&stderr); code != exitInvalid {
		t.Errorf("invalid: exit %d", code)
	}

	if _, err := os.Stat(out2); err == nil {
		t.Error("an invalid schedule must leave no output")
	}

	if code := run([]string{"publish", "prepare", sched}, &stdout, &stderr); code != exitUsage {
		t.Errorf("without --merge-request-file: exit %d", code)
	}
}
