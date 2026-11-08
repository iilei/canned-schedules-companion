package preview

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// GitHubMaxBody is the longest comment GitHub accepts, in characters.
const GitHubMaxBody = 65536

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

// Body is the comment to post: comment (comment.md as Render wrote it) with a line of links and an optional note
// under it. When that exceeds limit characters, the report and preview give way to a short notice, so the issue
// still gets the marker, the notice and the links.
func Body(comment []byte, links []Link, note string, limit int) (string, error) {
	c := string(comment)
	if !strings.HasPrefix(c, Marker+"\n") {
		return "", fmt.Errorf("%s doesn't start with the marker %s: not written by preview render", Comment, Marker)
	}

	tail := linkLine(links, note)

	body := c + tail
	if utf8.RuneCountInString(body) <= limit {
		return body, nil
	}

	body = Marker + "\n" + tooLong + tail
	if utf8.RuneCountInString(body) > limit {
		return "", fmt.Errorf("even the short comment has %d characters, more than %d", utf8.RuneCountInString(body),
			limit)
	}

	return body, nil
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
