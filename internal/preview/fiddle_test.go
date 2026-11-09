package preview_test

import (
	"encoding/base64"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

// linkRe finds the playground link in a comment and captures the address and the document.
var linkRe = regexp.MustCompile(
	`\n\[` + regexp.QuoteMeta(preview.FiddleLabel) + `\]\(([^)#]*)#doc=([A-Za-z0-9_-]*)\)\n`,
)

func renderWithFiddle(t *testing.T, path, base string) (*preview.Result, []byte) {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	u, err := preview.ParseFiddleBase(base)
	if err != nil {
		t.Fatal(err)
	}

	res, err := preview.Render(src, "request.md", &cs.Options{Preview: &cs.PreviewOptions{}}, u)
	if err != nil {
		t.Fatal(err)
	}

	return res, src
}

// docOf returns the address and the decoded document of the comment's playground link.
func docOf(t *testing.T, comment []byte) (string, string) {
	t.Helper()

	m := linkRe.FindSubmatch(comment)
	if m == nil {
		t.Fatalf("no playground link in:\n%s", comment)
	}

	doc, err := base64.RawURLEncoding.DecodeString(string(m[2]))
	if err != nil {
		t.Fatal(err)
	}

	return string(m[1]), string(doc)
}

func TestFiddleLinkValid(t *testing.T) {
	t.Parallel()

	res, src := renderWithFiddle(
		t,
		"../../testdata/office-hours.md",
		"https://schedules.example.com/preflight/?theme=dark",
	)

	addr, doc := docOf(t, res.Files[preview.Comment])
	if addr != "https://schedules.example.com/preflight/?theme=dark" {
		t.Errorf("the base and its query stay as given: %q", addr)
	}

	// A valid request travels in canonical form: no prose, no HTML comments.
	if strings.Contains(doc, "<!--") || !strings.HasPrefix(doc, "# Office Hours\n") || len(doc) >= len(src) {
		t.Errorf("want the canonical form, got:\n%s", doc)
	}

	if r, err := cs.Evaluate([]byte(doc), nil); err != nil || !r.Valid() {
		t.Errorf("the linked document must evaluate cleanly: %v", err)
	}

	if !strings.HasSuffix(string(res.Files[preview.Comment]), "</details>\n\n["+preview.FiddleLabel+"]("+addr+"#doc="+
		base64.RawURLEncoding.EncodeToString([]byte(doc))+")\n") {
		t.Errorf("the link comes last, after the preview:\n%s", res.Files[preview.Comment])
	}
}

func TestFiddleLinkInvalid(t *testing.T) {
	t.Parallel()

	res, src := renderWithFiddle(t, "../../testdata/invalid-name.md", "https://schedules.example.com/")

	if _, doc := docOf(t, res.Files[preview.Comment]); doc != string(src) {
		t.Error("an invalid request travels as written, so the playground shows its errors at their lines")
	}
}

func TestFiddleLinkTooLong(t *testing.T) {
	t.Parallel()

	// Invalid (the name ends in !), so the request travels as written: 48 KB, too long for one link.
	long := "# Office Hours!\n\n" + strings.Repeat("Some prose.\n", 4000)

	u, _ := preview.ParseFiddleBase("https://schedules.example.com/")

	res, err := preview.Render([]byte(long), "request.md", &cs.Options{Preview: &cs.PreviewOptions{}}, u)
	if err != nil {
		t.Fatal(err)
	}

	c := string(res.Files[preview.Comment])
	if strings.Contains(c, preview.FiddleLabel) || !strings.Contains(c, "playground link is left out") {
		t.Errorf("a too long link gives way to a notice:\n%.400s", c[len(c)-min(len(c), 400):])
	}
}

func TestParseFiddleBase(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"http://schedules.example.com/",
		"https://",
		"/relative/path",
		"https://schedules.example.com/#doc=",
		"https://schedules.example.com/#",
		"https://user:pass@schedules.example.com/",
		"javascript:alert(1)",
	} {
		if _, err := preview.ParseFiddleBase(s); err == nil {
			t.Errorf("ParseFiddleBase(%q) must fail", s)
		}
	}

	if _, err := preview.ParseFiddleBase("https://iilei.github.io/canned-schedules/?show=week"); err != nil {
		t.Errorf("a query is fine: %v", err)
	}
}

func TestFiddleLinkOptions(t *testing.T) {
	t.Parallel()

	base, err := preview.ParseFiddleBase(
		"https://schedules.example.com/?theme=dark&sample-interval=5&arg=--set-config-at&arg=states&arg=--set-config-value&arg=[]",
	)
	if err != nil {
		t.Fatal(err)
	}

	link := preview.FiddleLink(base, []byte("# Plan\n"), &cs.Options{
		SampleInterval: 15,
		RequireBase:    true,
		AlertPattern:   "(?i)NOTE|TIP",
		Config:         []cs.ConfigOverride{{Path: "states", Value: `[{"id":"Off","display-name":"Off"}]`}},
		Preview:        &cs.PreviewOptions{Downsample: 30},
	})

	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	if q.Get("theme") != "dark" || q["sample-interval"][0] != "15" || len(q["sample-interval"]) != 1 ||
		q.Get("downsample") != "30" {
		t.Errorf("the page's own parameters: %v", q)
	}

	want := []string{
		"--set-config-at", "states", "--set-config-value", "[]", // the base's own come first
		"--require-base", "true",
		"--error-unless-alert-pattern", "(?i)NOTE|TIP",
		"--set-config-at", "states", "--set-config-value", `[{"id":"Off","display-name":"Off"}]`,
	}
	if strings.Join(q["arg"], "\x00") != strings.Join(want, "\x00") {
		t.Errorf("arg = %q, want %q", q["arg"], want)
	}

	if u.Fragment != "doc="+base64.RawURLEncoding.EncodeToString([]byte("# Plan\n")) {
		t.Errorf("fragment = %q", u.Fragment)
	}

	// Defaults add nothing: the base stays as given.
	plain, _ := preview.ParseFiddleBase("https://schedules.example.com/?theme=dark")
	if got := preview.FiddleLink(
		plain,
		nil,
		&cs.Options{Preview: &cs.PreviewOptions{Downsample: cs.DefaultDownsample}},
	); got != "https://schedules.example.com/?theme=dark#doc=" {
		t.Errorf("defaults: %q", got)
	}
}
