package preview_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

func render(t *testing.T, path string) *preview.Result {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := preview.Render(src, "request.md", &cs.Options{Preview: &cs.PreviewOptions{}})
	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestValidRequest(t *testing.T) {
	t.Parallel()

	res := render(t, "../../testdata/office-hours.md")
	if !res.Valid {
		t.Fatal("the issue template must be valid")
	}

	for _, name := range preview.Names {
		if len(res.Files[name]) == 0 {
			t.Errorf("%s missing", name)
		}
	}

	c := string(res.Files[preview.Comment])
	if !strings.HasPrefix(c, preview.Marker+"\n") {
		t.Errorf("the comment must start with the marker: %.80q", c)
	}

	// The text form sits inside an 8-tilde fence, exactly as the consumer example's schedule:check writes it.
	want := "\n<details open><summary>Preview</summary>\n\n~~~~~~~~text\n" + string(res.Files[preview.ScheduleText]) +
		"~~~~~~~~\n\n</details>\n"
	if !strings.HasSuffix(c, want) {
		t.Errorf("the comment must end with the fenced text form:\n%s", c)
	}

	if !bytes.Contains(res.Files[preview.ScheduleSVG], []byte("prefers-color-scheme")) {
		t.Error("the SVG must follow the viewer's theme (ThemeAuto)")
	}
}

func TestInvalidRequest(t *testing.T) {
	t.Parallel()

	res := render(t, "../../testdata/invalid-name.md")
	if res.Valid {
		t.Fatal("a name ending in ! is invalid")
	}

	for _, name := range []string{preview.ScheduleJSON, preview.ScheduleText, preview.ScheduleSVG} {
		if _, ok := res.Files[name]; ok {
			t.Errorf("an invalid request must have no %s", name)
		}
	}

	c := string(res.Files[preview.Comment])
	if !strings.HasPrefix(c, preview.Marker+"\n") || !strings.Contains(c, "invalid-name") {
		t.Errorf("the comment must carry the marker and the error:\n%s", c)
	}

	if strings.Contains(c, "<details") {
		t.Error("an invalid request has no preview in the comment")
	}

	if !bytes.Contains(res.Files[preview.ReportJSON], []byte(`"code": "invalid-name"`)) {
		t.Errorf("report.json must name the error: %s", res.Files[preview.ReportJSON])
	}
}

func TestFence(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]int{
		"no tildes":                   8,
		"~~~text\n.\n~~~\n":           8,
		"~~~~~~~~ eight":              9,
		"a ~~~~~~~~~~~~ twelve ~ one": 13,
	} {
		if got := len(preview.Fence(text)); got != want {
			t.Errorf("Fence(%q) is %d tildes, want %d", text, got, want)
		}
	}
}
