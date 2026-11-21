package preview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	cs "github.com/iilei/canned-schedules"
)

// Marker starts the step's comment. The comment's first line is the marker with a Stamp. Forge steps find their
// earlier comment by the marker and the bot author; the step-specific key keeps later steps' comments apart. The
// space at the end matters: it matches the stamped line as well as the plain marker earlier releases wrote
// ("<!-- canned-schedules:issue-preview -->"), but not another step whose key merely starts the same way.
const Marker = "<!-- canned-schedules:issue-preview "

const markerEnd = "-->"

// Stamp records what a comment was rendered from, so a later run with the same inputs can leave it alone. It sits in
// the marker line, invisible on the forge: <!-- canned-schedules:issue-preview sha256=… tool=… args=… -->.
type Stamp struct {
	// Body is the SHA-256 of the request (BodyDigest).
	Body string
	// Tool names the program that rendered the comment, with its version.
	Tool string
	// Args is the SHA-256 of the effective options (ArgsDigest).
	Args string
}

// Line is the stamped marker line, without the newline.
func (s Stamp) Line() string {
	return Marker + "sha256=" + s.Body + " tool=" + s.Tool + " args=" + s.Args + " " + markerEnd
}

// ParseStamp reads the stamp from a comment's first line. It reports false for a comment without one, such as the
// plain marker of earlier releases, so such a comment is always rendered again.
func ParseStamp(comment string) (Stamp, bool) {
	line, _, _ := strings.Cut(comment, "\n")

	rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), Marker)
	if !ok {
		return Stamp{}, false
	}

	rest, ok = strings.CutSuffix(rest, " "+markerEnd)
	if !ok {
		return Stamp{}, false
	}

	var s Stamp

	fields := map[string]*string{"sha256": &s.Body, "tool": &s.Tool, "args": &s.Args}

	for f := range strings.FieldsSeq(rest) {
		key, value, ok := strings.Cut(f, "=")
		if p := fields[key]; ok && p != nil && *p == "" && value != "" {
			*p = value
		} else {
			return Stamp{}, false
		}
	}

	return s, s.Body != "" && s.Tool != "" && s.Args != ""
}

// BodyDigest is the SHA-256 of a request with CRLF line endings read as LF: issue bodies reach a job with either,
// depending on whether they come from the event or the API, and the check reads both the same way.
func BodyDigest(src []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")))

	return hex.EncodeToString(sum[:])
}

// ArgsDigest is the SHA-256 of the effective options and the playground link's base: everything besides the request
// that changes the comment. Spelling a flag differently gives the same digest.
func ArgsDigest(opts *cs.Options, fiddle *url.URL) string {
	var base string
	if fiddle != nil {
		base = fiddle.String()
	}

	// Options holds only strings, numbers, booleans and slices of them, so marshalling can't fail.
	raw, _ := json.Marshal(struct {
		Options *cs.Options `json:"options"`
		Fiddle  string      `json:"fiddle"`
	}{opts, base})

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}

// toolName keeps the tool field one word, so the marker line stays parseable whatever a version string holds.
func toolName(s string) string {
	s = strings.Join(strings.Fields(s), "_")
	s = strings.ReplaceAll(s, markerEnd, "")

	if s == "" {
		return "unknown"
	}

	return s
}
