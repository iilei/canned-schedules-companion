package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

// prepared runs publish prepare for merge request !1 in group/catalog without a review, and returns its directory.
func prepared(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	mr := filepath.Join(repo, "merge-request.json")
	sched := filepath.Join(repo, "night-shift.canned-schedule.md")
	input := `{"merge-request": {"forge": "gitlab", "host": "gitlab.example", "project": "group/catalog",
	  "number": 1, "created-at": "2026-11-20T09:15:00Z", "merged-at": "2026-11-22T14:30:00Z", "merge-commit": "abc"},
	  "review": null}`

	for p, c := range map[string]string{mr: input, sched: publishNight} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "publish")

	var stdout, stderr bytes.Buffer
	if code := run(
		[]string{
			"publish",
			"prepare",
			"--merge-request-file",
			mr,
			"--path-template",
			"{slug}/{mr}",
			"--out-dir",
			out,
			sched,
		},
		&stdout,
		&stderr,
	); code != exitOK {
		t.Fatalf("prepare: exit %d: %s", code, stderr.String())
	}

	return out
}

func TestPublishPost(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	f := &fakeMR{}
	srv := httptest.NewServer(f)

	defer srv.Close()

	post := func(dir string, extra ...string) string {
		t.Helper()

		var stdout, stderr bytes.Buffer

		args := append(
			[]string{"publish", "post", "--forge", "gitlab", "--repo", "group/catalog", "--api-url", srv.URL},
			extra...)
		if code := run(append(args, dir), &stdout, &stderr); code != exitOK {
			t.Fatalf("post: exit %d: %s", code, stderr.String())
		}

		return stdout.String()
	}

	// Without a review: one note, and the same run again leaves it alone.
	unreviewed := prepared(t)

	if o := post(unreviewed, "--dry-run"); !strings.Contains(o, "would create the note on group/catalog!1") ||
		len(f.writes) != 0 {
		t.Errorf("dry run: %q, writes %q", o, f.writes)
	}

	post(unreviewed)

	if len(f.notes) != 1 || !strings.HasPrefix(f.notes[0], publish.NoteMarker) ||
		!strings.Contains(f.notes[0], "no review comment") {
		t.Fatalf("notes %q", f.notes)
	}

	if o := post(unreviewed); !strings.Contains(o, "up to date") || !slices.Equal(f.writes, []string{"create"}) {
		t.Errorf("again: %q, writes %q", o, f.writes)
	}

	// Every schedule as reviewed: nothing written.
	digest := publish.Digest([]byte(publishNight))

	ok := t.TempDir()
	if err := os.WriteFile(filepath.Join(ok, publish.Manifest), []byte(`{"version": 1, "merge-request": {"number": 1},
	  "schedules": [{"path": "a", "digest": "`+digest+`", "reviewed": true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if o := post(ok); !strings.Contains(o, "published as reviewed: no note") || len(f.writes) != 1 {
		t.Errorf("reviewed: %q, writes %q", o, f.writes)
	}
}

func TestPublishPostUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--repo", "o/r", "dir"},
		{"--forge", "gitlab", "dir"},
		{"--forge", "gitlab", "--repo", "o/r"},
		{"--forge", "gitlab", "--repo", "o/r", t.TempDir()}, // no manifest
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"publish", "post"}, args...), &stdout, &stderr); code != exitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}
