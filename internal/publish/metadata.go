package publish

import (
	"errors"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"unicode"
)

// DefaultMetadata is the metadata template when the caller gives none: one key=value per line.
const DefaultMetadata = `schedule={name}
digest={digest}
reviewed={reviewed}
merge-request={mr}
merged-at={merged-at}
merged-by={merged-by}
project={project}
`

// utf8RuneSelf is the first byte value that isn't ASCII.
const utf8RuneSelf = 0x80

// s3MetadataLimit is S3's limit for user-defined metadata: keys and values together, in bytes.
const s3MetadataLimit = 2 << 10

// metadataKey is what both Artifactory properties and S3 metadata keys take without escaping: lower case, so S3's
// lower-casing changes nothing.
var metadataKey = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)

var errMetadata = errors.New("metadata")

// Metadata is a parsed metadata template: one template per line, filled and then split at the first "=".
type Metadata struct {
	lines []*Template
}

// Pair is one metadata entry, in the template's order.
type Pair struct {
	Key, Value string
}

// ParseMetadata reads a metadata template. Blank lines and lines starting with # are skipped.
func ParseMetadata(text string) (*Metadata, error) {
	m := &Metadata{}

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if !strings.Contains(line, "=") {
			return nil, fmt.Errorf("%w line %q: want key=value", errMetadata, line)
		}

		t, err := Parse(line)
		if err != nil {
			return nil, err
		}

		m.lines = append(m.lines, t)
	}

	return m, nil
}

// Execute fills every line and checks keys and values.
func (m *Metadata) Execute(vars map[string]string) ([]Pair, error) {
	pairs := make([]Pair, 0, len(m.lines))
	seen := map[string]bool{}

	for _, t := range m.lines {
		key, value, _ := strings.Cut(t.Execute(vars), "=")
		key = strings.TrimSpace(key)

		if !metadataKey.MatchString(key) {
			return nil, fmt.Errorf("%w key %q: use lower-case letters, digits, '.', '_' and '-'", errMetadata, key)
		}

		if seen[key] {
			return nil, fmt.Errorf("%w key %q appears twice", errMetadata, key)
		}

		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("%w %s: the value holds a control character", errMetadata, key)
		}

		seen[key] = true
		pairs = append(pairs, Pair{Key: key, Value: value})
	}

	return pairs, nil
}

// Artifactory is the matrix-parameter form of pairs, to append to an Artifactory deploy URL
// (PUT …/file;key=value;…): every value byte outside RFC 3986's unreserved set is percent-encoded, so ';', ',', '='
// and spaces in values can't split or end a property.
func Artifactory(pairs []Pair) string {
	var b strings.Builder

	for _, p := range pairs {
		b.WriteString(";" + p.Key + "=" + percentEncode(p.Value))
	}

	return b.String()
}

// S3 is the user-defined metadata form of pairs, for aws s3 cp --metadata (as JSON) or x-amz-meta-* headers. Values
// outside printable ASCII are RFC 2047 encoded, as S3 returns them. It reports false when the pairs exceed S3's 2 KB.
func S3(pairs []Pair) (map[string]string, bool) {
	out := make(map[string]string, len(pairs))
	size := 0

	for _, p := range pairs {
		v := p.Value
		if !printableASCII(v) {
			v = mime.BEncoding.Encode("UTF-8", v)
		}

		out[p.Key] = v
		size += len(p.Key) + len(v)
	}

	return out, size <= s3MetadataLimit
}

func percentEncode(s string) string {
	const hex = "0123456789ABCDEF"

	var b strings.Builder

	for i := range len(s) {
		c := s[i]
		if c < utf8RuneSelf && (isAlnum(c) || strings.IndexByte("-._~", c) >= 0) {
			b.WriteByte(c)

			continue
		}

		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}

	return b.String()
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func printableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}

	return true
}
