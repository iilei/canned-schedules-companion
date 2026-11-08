package preview

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// FiddleLabel is the text of the playground link.
const FiddleLabel = "Try this schedule in the playground and see its week"

// MaxFiddleLink caps the playground link in characters. Longer ones are left out: they would crowd the comment
// (GitHub allows 65,536 characters in all) and some browsers cut very long addresses.
const MaxFiddleLink = 32 << 10

// fiddleTooLong replaces the link when the schedule doesn't fit into one.
const fiddleTooLong = "The playground link is left out: this schedule is too long for one.\n"

var errFiddleBase = errors.New("want an https URI with a host and without a fragment (the link adds #doc=…)")

// ParseFiddleBase checks --fiddle-link-base-uri: the address of a preflight page the team hosts itself. A query is
// kept (e.g. ?theme=dark); a fragment isn't allowed, because the document goes there.
func ParseFiddleBase(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.Contains(s, "#") {
		return nil, fmt.Errorf("--fiddle-link-base-uri %q: %w", s, errFiddleBase)
	}

	return u, nil
}

// FiddleLink is base with the document in the fragment as base64url (no padding), the form the preflight page
// reads from #doc=. Browsers never send the fragment to the server, so the document stays in the reader's browser.
func FiddleLink(base *url.URL, doc []byte) string {
	u := *base
	u.Fragment, u.RawFragment = "", ""

	return u.String() + "#doc=" + base64.RawURLEncoding.EncodeToString(doc)
}

// fiddleLine is the comment's link line for doc, or the notice when the link would be too long.
func fiddleLine(base *url.URL, doc []byte) string {
	link := FiddleLink(base, doc)
	if len(link) > MaxFiddleLink {
		return "\n" + fiddleTooLong
	}

	return "\n[" + FiddleLabel + "](" + parens.Replace(link) + ")\n"
}
