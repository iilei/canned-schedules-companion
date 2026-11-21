package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// The longest comment each forge accepts: GitHub counts characters, GitLab bytes.
const (
	GitHubMaxBody = 65536
	GitLabMaxBody = 1 << 20
)

// imageLabel is the alternative text of the week shown inline.
const imageLabel = "Week as SVG"

// uploadHashLen is how much of the SVG's SHA-256 names the upload: enough to tell two weeks of one issue apart.
const uploadHashLen = 12

// tooLong replaces the report and preview when the comment wouldn't fit; the linked files hold everything.
const tooLong = "The check result is too long for a comment; the linked files hold the full report.\n"

// parens percent-encodes the parentheses in a link target.
var parens = strings.NewReplacer("(", "%28", ")", "%29")

var (
	errLink  = errors.New(`want label=https://…`)
	errLabel = errors.New("a label is plain text: no brackets, parentheses, backticks or line breaks")
)

// Link is a file the forge step uploaded, shown under the comment.
type Link struct {
	Label string
	URL   string
}

// UploadName is the name of the week uploaded to a forge. The hash lets another job tell which week a comment shows.
func UploadName(issue int, svg []byte) string {
	sum := sha256.Sum256(svg)

	return fmt.Sprintf("schedule-%d-%s.svg", issue, hex.EncodeToString(sum[:])[:uploadHashLen])
}

// ParseLink reads "label=https://…".
func ParseLink(s string) (Link, error) {
	label, raw, ok := strings.Cut(s, "=")
	label = strings.TrimSpace(label)

	if !ok || label == "" {
		return Link{}, fmt.Errorf("--link %q: %w", s, errLink)
	}

	if strings.ContainsAny(label, "[]()`\r\n") {
		return Link{}, fmt.Errorf("--link %q: %w", s, errLabel)
	}

	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return Link{}, fmt.Errorf("--link %q: %w", s, errLink)
	}

	return Link{Label: label, URL: u.String()}, nil
}

// Body is the comment to post on GitHub: comment (comment.md as Render wrote it) with a line of links and an optional
// note under it. When that exceeds limit characters, the report and preview give way to a short notice, so the issue
// still gets the marker, the notice and the links.
func Body(comment []byte, links []Link, note string, limit int) (string, error) {
	return body(comment, "", links, note, limit, utf8.RuneCountInString)
}

// GitLabBody is Body for GitLab: the week shows inline above the links (image is the path the upload returned, empty
// for none), and limit counts bytes.
func GitLabBody(comment []byte, image string, links []Link, note string, limit int) (string, error) {
	return body(comment, image, links, note, limit, func(s string) int { return len(s) })
}

func body(comment []byte, image string, links []Link, note string, limit int, size func(string) int) (string, error) {
	c := string(comment)

	head, _, ok := strings.Cut(c, "\n")
	if _, stamped := ParseStamp(c); !ok || !stamped {
		return "", fmt.Errorf("%s doesn't start with a stamped marker: not written by preview render", Comment)
	}

	tail := imageLine(image) + linkLine(links, note)

	full := c + tail
	if size(full) <= limit {
		return full, nil
	}

	short := head + "\n" + tooLong + tail
	if size(short) > limit {
		return "", fmt.Errorf("even the short comment has size %d, more than the limit of %d", size(short), limit)
	}

	return short, nil
}

func imageLine(image string) string {
	if image == "" {
		return ""
	}

	return "\n![" + imageLabel + "](" + parens.Replace(image) + ")\n"
}

func linkLine(links []Link, note string) string {
	if len(links) == 0 {
		return ""
	}

	parts := make([]string, 0, len(links))
	for _, l := range links {
		// CommonMark allows parentheses in a link target only in balanced pairs, so both are percent-encoded.
		parts = append(parts, "["+l.Label+"]("+parens.Replace(l.URL)+")")
	}

	line := "\n" + strings.Join(parts, " · ")
	if note != "" {
		line += " (" + note + ")"
	}

	return line + "\n"
}
