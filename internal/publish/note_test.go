package publish_test

import (
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

func TestNote(t *testing.T) {
	t.Parallel()

	files := []publish.File{{Path: nightPath, Src: []byte(night)}, {Path: peakPath, Src: []byte(peak)}}
	older := publish.Digest([]byte("an older version\n"))

	r, err := publish.Prepare(input(t, map[string]string{nightPath: older}), files, options(t, "{slug}", ""))
	if err != nil {
		t.Fatal(err)
	}

	n, err := publish.NoteFor(r.Files[publish.Manifest])
	if err != nil {
		t.Fatal(err)
	}

	if n.MergeRequest != 42 || n.Unreviewed != 2 || !strings.HasPrefix(n.Body, publish.NoteMarker+"\n") {
		t.Fatalf("note %+v", n)
	}

	for _, want := range []string{
		"merged as `abc123`, published 2 schedules",
		"| `" + nightPath + "` | changed after the last review | `" + publish.Digest([]byte(night)) + "` | `" + older +
			"` |",
		"| `" + peakPath + "` | not in the review | `" + publish.Digest([]byte(peak)) + "` | none |",
	} {
		if !strings.Contains(n.Body, want) {
			t.Errorf("missing %q in\n%s", want, n.Body)
		}
	}

	// Every schedule as reviewed: no note.
	r, err = publish.Prepare(input(t, map[string]string{nightPath: publish.Digest([]byte(night))}), files[:1],
		options(t, "{slug}", ""))
	if err != nil {
		t.Fatal(err)
	}

	if n, err := publish.NoteFor(r.Files[publish.Manifest]); err != nil || n.Body != "" || n.Unreviewed != 0 {
		t.Errorf("reviewed: %+v, %v", n, err)
	}

	for _, raw := range []string{`[]`, `{"version": 1, "schedules": []}`, `{"version": 2, "merge-request": {"number": 1}}`} {
		if _, err := publish.NoteFor([]byte(raw)); err == nil {
			t.Errorf("%s: want an error", raw)
		}
	}
}
