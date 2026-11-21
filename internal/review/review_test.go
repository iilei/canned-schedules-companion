package review_test

import (
	"strings"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
	"github.com/iilei/canned-schedules-companion/internal/review"
)

const night = "# Night Shift\n\n> [!SCHEDULE-STATE] Medium\n> - `22:00` to `06:00` MON-FRI\n"

func opts() *cs.Options { return &cs.Options{Preview: &cs.PreviewOptions{}} }

func TestRenderValid(t *testing.T) {
	t.Parallel()

	res, err := review.Render([]review.File{{Path: "schedules/night-shift.canned-schedule.md", Src: []byte(night)}},
		opts(), nil, "test")
	if err != nil {
		t.Fatal(err)
	}

	c := string(res.Comment)
	if !res.Valid || len(res.Names) != 1 || res.Names[0] != "Night Shift" {
		t.Errorf("result %+v", res)
	}

	if _, ok := preview.ParseStampFor(review.Marker, c); !ok || !strings.HasPrefix(c, review.Marker) {
		t.Errorf("no stamped marker: %.120q", c)
	}

	for _, want := range []string{"1 schedule checked, no problems found.", "<summary>Valid: <code>schedules/night-shift.canned-schedule.md</code></summary>", "~~~~~~~~text\n"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in\n%s", want, c)
		}
	}
}

func TestRenderProblems(t *testing.T) {
	t.Parallel()

	res, err := review.Render([]review.File{
		{Path: "schedules/nightly.canned-schedule.md", Src: []byte(night)},
		{Path: "schedules/broken.canned-schedule.md", Src: []byte("# Broken!\n")},
		{Path: "schedules/<b>x</b>.canned-schedule.md", Src: []byte(night)},
	}, opts(), nil, "test")
	if err != nil {
		t.Fatal(err)
	}

	c := string(res.Comment)
	if res.Valid || len(res.Names) != 0 {
		t.Errorf("result %+v", res)
	}

	for _, want := range []string{
		"3 schedules checked, 3 with problems.",
		"The file should be named `night-shift.canned-schedule.md`",
		"invalid-name",
		"a file with an unusual path",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in\n%s", want, c)
		}
	}

	if strings.Contains(c, "<b>x</b>") {
		t.Error("an unsafe path must not be echoed")
	}
}

// TestRenderStamp: the same files give the same stamp; another file or content another one.
func TestRenderStamp(t *testing.T) {
	t.Parallel()

	stamp := func(files ...review.File) preview.Stamp {
		res, err := review.Render(files, opts(), nil, "test")
		if err != nil {
			t.Fatal(err)
		}

		s, _ := preview.ParseStampFor(review.Marker, string(res.Comment))

		return s
	}

	a := review.File{Path: "schedules/night-shift.canned-schedule.md", Src: []byte(night)}
	b := review.File{
		Path: "schedules/night-shift.canned-schedule.md",
		Src:  []byte(strings.Replace(night, "22:00", "21:00", 1)),
	}

	first, second := stamp(a), stamp(a)
	if first != second || first == stamp(b) {
		t.Error("the stamp must follow the files")
	}

	crlf := review.File{Path: a.Path, Src: []byte(strings.ReplaceAll(night, "\n", "\r\n"))}
	if stamp(a) != stamp(crlf) {
		t.Error("line endings must not change the stamp")
	}
}

func TestTitles(t *testing.T) {
	t.Parallel()

	if review.Title([]string{"Night Shift"}) != "Schedule: Night Shift" ||
		review.Title([]string{"A", "B"}) != "Schedules: A, B" || review.Title(nil) != "" {
		t.Error("Title")
	}

	for title, want := range map[string]bool{
		"Add new file": true,
		"Update schedules/night-shift.canned-schedule.md": true,
		"Edit night-shift.canned-schedule.md":             true,
		"Night shift for the support desk":                false,
		"Update the night shift":                          false,
		"Schedule: Night Shift":                           false,
	} {
		if review.DefaultTitle(title) != want {
			t.Errorf("DefaultTitle(%q) != %v", title, want)
		}
	}
}
