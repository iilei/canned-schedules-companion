package preview_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

func TestBodyWithLinks(t *testing.T) {
	t.Parallel()

	svg, err := preview.ParseLink("Week as SVG=https://github.com/o/r/actions/runs/1/artifacts/2")
	if err != nil {
		t.Fatal(err)
	}

	all, err := preview.ParseLink("All files = https://example.com/a(1)")
	if err != nil {
		t.Fatal(err)
	}

	got, err := preview.Body([]byte(preview.Marker+"\nreport\n"), []preview.Link{svg, all}, "kept for 30 days", 1000)
	if err != nil {
		t.Fatal(err)
	}

	want := preview.Marker + "\nreport\n\n[Week as SVG](https://github.com/o/r/actions/runs/1/artifacts/2) · " +
		"[All files](https://example.com/a%281%29) (kept for 30 days)\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBodyTooLong(t *testing.T) {
	t.Parallel()

	link, _ := preview.ParseLink("All files=https://example.com/f")
	long := []byte(preview.Marker + "\n" + strings.Repeat("ä", 70000))

	got, err := preview.Body(long, []preview.Link{link}, "", preview.GitHubMaxBody)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(got, preview.Marker+"\n") || !strings.Contains(got, "too long") ||
		!strings.Contains(got, "[All files](https://example.com/f)") || utf8.RuneCountInString(got) > 200 {
		t.Errorf("fallback: %q", got)
	}

	if _, err := preview.Body(long, nil, "", 10); err == nil {
		t.Error("a limit below the short comment must fail")
	}
}

func TestBodyNeedsTheMarker(t *testing.T) {
	t.Parallel()

	if _, err := preview.Body([]byte("hand-written\n"), nil, "", 1000); err == nil {
		t.Error("a comment.md without the marker must be refused")
	}
}

func TestParseLink(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"no equals sign",
		"=https://example.com",
		"label=http://example.com",
		"label=javascript:alert(1)",
		"label=https://",
		"[evil](x)=https://example.com",
		"two\nlines=https://example.com",
	} {
		if _, err := preview.ParseLink(s); err == nil {
			t.Errorf("ParseLink(%q) must fail", s)
		}
	}
}
