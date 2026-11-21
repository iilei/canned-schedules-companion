package preview_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

// head is a stamped marker line, as preview render writes it.
var head = preview.Stamp{Body: "b0d7", Tool: "test", Args: "a1"}.Line()

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

	got, err := preview.Body([]byte(head+"\nreport\n"), []preview.Link{svg, all}, "kept for 30 days", 1000)
	if err != nil {
		t.Fatal(err)
	}

	want := head + "\nreport\n\n[Week as SVG](https://github.com/o/r/actions/runs/1/artifacts/2) · " +
		"[All files](https://example.com/a%281%29) (kept for 30 days)\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestBodyTooLong(t *testing.T) {
	t.Parallel()

	link, _ := preview.ParseLink("All files=https://example.com/f")
	long := []byte(head + "\n" + strings.Repeat("ä", 70000))

	got, err := preview.Body(long, []preview.Link{link}, "", preview.GitHubMaxBody)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(got, head+"\n") || !strings.Contains(got, "too long") ||
		!strings.Contains(got, "[All files](https://example.com/f)") || utf8.RuneCountInString(got) > 200 {
		t.Errorf("fallback: %q", got)
	}

	if _, err := preview.Body(long, nil, "", 10); err == nil {
		t.Error("a limit below the short comment must fail")
	}
}

func TestBodyNeedsTheMarker(t *testing.T) {
	t.Parallel()

	for _, c := range []string{"hand-written\n", "<!-- canned-schedules:issue-preview -->\nreport\n"} {
		if _, err := preview.Body([]byte(c), nil, "", 1000); err == nil {
			t.Errorf("a comment.md without a stamped marker must be refused: %q", c)
		}
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

func TestGitLabBodyShowsTheWeek(t *testing.T) {
	comment := []byte(head + "\nreport\n")
	links := []preview.Link{{Label: "All files", URL: "https://example.com/all"}}

	got, err := preview.GitLabBody(comment, "/uploads/a(1)/schedule.svg", links, "", preview.GitLabMaxBody)
	if err != nil {
		t.Fatal(err)
	}

	want := head + "\nreport\n\n![Week as SVG](/uploads/a%281%29/schedule.svg)\n\n[All files](https://example.com/all)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGitLabBodyCountsBytes: GitLab measures a note in bytes, so text with multi-byte characters reaches the limit
// with far fewer characters than GitHub's rule would allow.
func TestGitLabBodyCountsBytes(t *testing.T) {
	start := head + "\n"
	room := preview.GitLabMaxBody - len(start)

	fits := start + strings.Repeat("x", room)
	if got, err := preview.GitLabBody([]byte(fits), "", nil, "", preview.GitLabMaxBody); err != nil || got != fits {
		t.Errorf("a comment of exactly the limit must stay whole (err %v, %d bytes)", err, len(got))
	}

	over := start + strings.Repeat("é", room/2+1)

	got, err := preview.GitLabBody([]byte(over), "", nil, "", preview.GitLabMaxBody)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "too long for a comment") || len(got) > preview.GitLabMaxBody {
		t.Errorf("want the short notice, got %d bytes", len(got))
	}
}

func TestUploadName(t *testing.T) {
	// The SHA-256 of "abc" starts with ba7816bf8f01.
	if got := preview.UploadName(7, []byte("abc")); got != "schedule-7-ba7816bf8f01.svg" {
		t.Errorf("got %q", got)
	}
}
