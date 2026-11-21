package preview_test

import (
	"net/url"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

func TestStampRoundTrip(t *testing.T) {
	t.Parallel()

	s := preview.Stamp{Body: "ab12", Tool: "canned-schedules-companion@0.2.0", Args: "cd34"}

	got, ok := preview.ParseStamp(s.Line() + "\nreport\n")
	if !ok || got != s {
		t.Errorf("ParseStamp(%q) = %+v, %v", s.Line(), got, ok)
	}

	if got, ok := preview.ParseStamp(s.Line() + "\r\nreport\r\n"); !ok || got != s {
		t.Errorf("a stamped line with CRLF must parse: %+v, %v", got, ok)
	}
}

// TestParseStampRefuses: anything but a complete stamp reads as "no stamp", so the comment is rendered again.
func TestParseStampRefuses(t *testing.T) {
	t.Parallel()

	for _, c := range []string{
		"<!-- canned-schedules:issue-preview -->\nreport\n",
		"<!-- canned-schedules:issue-preview sha256=ab tool=t -->",
		"<!-- canned-schedules:issue-preview sha256=ab tool=t args=cd extra=1 -->",
		"<!-- canned-schedules:issue-preview sha256=ab sha256=ab tool=t args=cd -->",
		"<!-- canned-schedules:issue-preview sha256= tool=t args=cd -->",
		"<!-- canned-schedules:issue-preview sha256=ab tool=t args=cd",
		"<!-- canned-schedules:issue-preview-other sha256=ab tool=t args=cd -->",
		"report\n<!-- canned-schedules:issue-preview sha256=ab tool=t args=cd -->",
		"",
	} {
		if s, ok := preview.ParseStamp(c); ok {
			t.Errorf("ParseStamp(%q) = %+v, want no stamp", c, s)
		}
	}
}

func TestBodyDigestReadsCRLFAsLF(t *testing.T) {
	t.Parallel()

	if preview.BodyDigest([]byte("# A\r\n\r\ntext\r\n")) != preview.BodyDigest([]byte("# A\n\ntext\n")) {
		t.Error("CRLF and LF must give the same digest")
	}

	if preview.BodyDigest([]byte("# A\n")) == preview.BodyDigest([]byte("# B\n")) {
		t.Error("different texts must give different digests")
	}
}

func TestArgsDigest(t *testing.T) {
	t.Parallel()

	base := &cs.Options{Preview: &cs.PreviewOptions{}}
	other := &cs.Options{Preview: &cs.PreviewOptions{}, SampleInterval: 15}
	u, _ := url.Parse("https://example.com/playground/")

	if preview.ArgsDigest(base, nil) != preview.ArgsDigest(&cs.Options{Preview: &cs.PreviewOptions{}}, nil) {
		t.Error("equal options must give the same digest")
	}

	if preview.ArgsDigest(base, nil) == preview.ArgsDigest(other, nil) {
		t.Error("another sample interval must change the digest")
	}

	if preview.ArgsDigest(base, nil) == preview.ArgsDigest(base, u) {
		t.Error("a playground link must change the digest")
	}
}
