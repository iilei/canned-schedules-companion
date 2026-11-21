package promote_test

import (
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/promote"
)

const night = "# Night Shift\r\n\r\nWhy: the desk needs cover.\r\n\r\n> [!SCHEDULE-STATE] Medium\r\n" +
	"> - `22:00` to `06:00` MON-FRI\r\n\r\n"

func TestPrepare(t *testing.T) {
	t.Parallel()

	r, err := promote.Prepare([]byte(night), "issue.md", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if !r.Valid || !r.Schedule || r.Name != "Night Shift" || r.Path != "schedules/night-shift.canned-schedule.md" ||
		r.Title != "Schedule: Night Shift" {
		t.Errorf("result %+v", r)
	}

	// Verbatim apart from line endings: the explanation stays, CRLF goes, one final newline.
	want := strings.TrimRight(strings.ReplaceAll(night, "\r\n", "\n"), "\n") + "\n"
	if string(r.Content) != want {
		t.Errorf("content %q, want %q", r.Content, want)
	}
}

func TestPrepareInvalid(t *testing.T) {
	t.Parallel()

	r, err := promote.Prepare([]byte("# Night Shift!\n\n> [!SCHEDULE-STATE] Medium\n"), "issue.md", nil, "ops/cat")
	if err != nil {
		t.Fatal(err)
	}

	if r.Valid || !r.Schedule || r.Path != "" || !strings.Contains(string(r.Report), "invalid-name") {
		t.Errorf("result %+v, report %s", r, r.Report)
	}

	if r, _ := promote.Prepare([]byte("# Just prose\n"), "issue.md", nil, ""); r.Valid || r.Schedule {
		t.Errorf("prose without directives: %+v", r)
	}
}

func TestPrepareCatalogDir(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"../x", "/abs", "a//b", ".hidden", "a/../b"} {
		if _, err := promote.Prepare([]byte(night), "issue.md", nil, dir); err == nil {
			t.Errorf("%q: want an error", dir)
		}
	}

	if r, err := promote.Prepare([]byte(night), "issue.md", nil, "ops/catalog"); err != nil ||
		r.Path != "ops/catalog/night-shift.canned-schedule.md" {
		t.Errorf("nested dir: %+v, %v", r, err)
	}
}
