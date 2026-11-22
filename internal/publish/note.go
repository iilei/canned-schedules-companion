package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// NoteMarker starts the note publishing keeps on a merged merge request.
const NoteMarker = "<!-- canned-schedules:publish-note -->"

// plain is what a path or commit may contain to be echoed in the note; anything else could carry Markdown.
var plain = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

var errManifest = errors.New("not a manifest publish prepare wrote")

// Note is the note for the merge request in a manifest: what was published without the review vouching for it.
type Note struct {
	// Body is the whole note, starting with NoteMarker; empty when every schedule was published as reviewed.
	Body string
	// MergeRequest is the merge request's number.
	MergeRequest int
	// Unreviewed counts the schedules the note lists.
	Unreviewed int
}

// NoteFor reads a manifest and writes the note for its merge request.
func NoteFor(manifestJSON []byte) (*Note, error) {
	var m manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", errManifest, err)
	}

	if m.Version != Version || m.MergeRequest.Number < 1 {
		return nil, fmt.Errorf("%w (version %d, merge request %d)", errManifest, m.Version, m.MergeRequest.Number)
	}

	n := &Note{MergeRequest: m.MergeRequest.Number}

	var rows strings.Builder

	for i := range m.Schedules {
		e := &m.Schedules[i]
		if e.Reviewed {
			continue
		}

		n.Unreviewed++

		reviewed := "none"
		if e.ReviewedDigest != nil {
			reviewed = code(*e.ReviewedDigest, IsDigest(*e.ReviewedDigest))
		}

		fmt.Fprintf(&rows, "| %s | %s | %s | %s |\n", code(e.Path, plain.MatchString(e.Path)), reasonText(e.Reason),
			code(e.Digest, IsDigest(e.Digest)), reviewed)
	}

	if n.Unreviewed == 0 {
		return n, nil
	}

	var b strings.Builder

	b.WriteString(NoteMarker + "\n### Published without review\n\n")

	merged := "This merge request"
	if c := m.MergeRequest.MergeCommit; c != "" && plain.MatchString(c) {
		merged = "This merge request, merged as `" + c + "`,"
	}

	fmt.Fprintf(&b, "%s published %s that its review didn't vouch for. Check that the merged files are what was "+
		"meant: the published provenance records the same.\n\n", merged, schedules(n.Unreviewed))
	b.WriteString("| File | Why | Published digest | Reviewed digest |\n| --- | --- | --- | --- |\n")
	b.WriteString(rows.String())

	n.Body = b.String()

	return n, nil
}

func reasonText(reason string) string {
	switch reason {
	case ReasonChanged:
		return "changed after the last review"
	case ReasonNotReviewed:
		return "not in the review"
	case ReasonNoReview:
		return "no review comment"
	default:
		return "unknown"
	}
}

// code shows s as code when ok, otherwise says that it can't be shown.
func code(s string, ok bool) string {
	if !ok {
		return "(not shown: unusual characters)"
	}

	return "`" + s + "`"
}

func schedules(n int) string {
	if n == 1 {
		return "1 schedule"
	}

	return fmt.Sprintf("%d schedules", n)
}
