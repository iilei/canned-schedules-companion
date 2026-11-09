package preview

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	cs "github.com/iilei/canned-schedules"
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

// FiddleLink is base with the run's options in the query and the document in the fragment as base64url (no
// padding), the form the preflight page reads from #doc=. Browsers never send the fragment to the server, so the
// document stays in the reader's browser. The options make the playground check the document as the run did: the
// sample interval and the preview buckets as the page's own parameters (they replace the base's), everything else
// as ?arg=, one argument each, after the base's own (so the run's config overrides come later and win).
func FiddleLink(base *url.URL, doc []byte, opts *cs.Options) string {
	u := *base
	u.Fragment, u.RawFragment = "", ""

	if set, args := linkQuery(opts); len(set)+len(args) > 0 {
		q := u.Query()
		for k, v := range set {
			q.Set(k, v)
		}

		for _, a := range args {
			q.Add("arg", a)
		}

		u.RawQuery = q.Encode()
	}

	return u.String() + "#doc=" + base64.RawURLEncoding.EncodeToString(doc)
}

// linkQuery maps the options that differ from the defaults to the page's parameters and its ?arg= flags.
func linkQuery(opts *cs.Options) (map[string]string, []string) {
	set := map[string]string{}

	var args []string

	if opts == nil {
		return set, args
	}

	if opts.SampleInterval > 1 {
		set["sample-interval"] = strconv.Itoa(opts.SampleInterval)
	}

	if opts.Preview != nil && opts.Preview.Downsample != 0 && opts.Preview.Downsample != cs.DefaultDownsample {
		set["downsample"] = strconv.Itoa(opts.Preview.Downsample)
	}

	if opts.RequireBase {
		args = append(args, "--require-base", "true")
	}

	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}

	if opts.AlertPattern != "" {
		args = append(args, "--error-unless-alert-pattern", opts.AlertPattern)
	}

	for _, c := range opts.Config {
		args = append(args, "--set-config-at", c.Path, "--set-config-value", c.Value)
	}

	return set, args
}

// fiddleLine is the comment's link line for doc, or the notice when the link would be too long.
func fiddleLine(base *url.URL, doc []byte, opts *cs.Options) string {
	link := FiddleLink(base, doc, opts)
	if len(link) > MaxFiddleLink {
		return "\n" + fiddleTooLong
	}

	return "\n[" + FiddleLabel + "](" + parens.Replace(link) + ")\n"
}
