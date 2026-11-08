// Package preview is the local part of step 1, issue feedback: it evaluates a schedule request (an issue body) and
// produces the files a forge step posts or uploads, without touching the network or the file system.
package preview

import (
	"bytes"
	"strings"

	cs "github.com/iilei/canned-schedules"
)

// Marker is the comment's first line. Forge steps find their earlier comment by it (and by the bot author); the
// step-specific key keeps later steps' comments apart.
const Marker = "<!-- canned-schedules:issue-preview -->"

// File names in the output directory, the same as the consumer example's schedule:check task.
const (
	ReportMarkdown = "report.md"
	ReportJSON     = "report.json"
	Comment        = "comment.md"
	ScheduleJSON   = "schedule.json"
	ScheduleText   = "schedule.txt"
	ScheduleSVG    = "schedule.svg"
)

// minFence is the shortest fence around the text form; it grows past any tilde run inside.
const minFence = 8

// Names lists every file Render can produce, so a caller can remove stale ones first.
var Names = []string{ReportMarkdown, ReportJSON, Comment, ScheduleJSON, ScheduleText, ScheduleSVG}

// Result is what Render produced: file name → content. A valid request has every file, an invalid one only the
// reports and the comment.
type Result struct {
	Files map[string][]byte
	Valid bool
}

// Render evaluates src (named file in the reports) with opts, which must plan a preview (opts.Preview). The error
// is only for unusable options or a rendering bug; document problems are in the reports and the comment.
func Render(src []byte, file string, opts *cs.Options) (*Result, error) {
	r, err := cs.Evaluate(src, opts)
	if err != nil {
		return nil, err
	}

	rep := r.Report(file)
	res := &Result{Valid: r.Valid(), Files: map[string][]byte{}}

	if res.Files[ReportMarkdown], err = rep.Markdown(); err != nil {
		return nil, err
	}

	if res.Files[ReportJSON], err = rep.JSON(); err != nil {
		return nil, err
	}

	if r.Valid() {
		if res.Files[ScheduleJSON], err = r.JSON(); err != nil {
			return nil, err
		}

		if res.Files[ScheduleText], err = r.Text(false); err != nil {
			return nil, err
		}

		if res.Files[ScheduleSVG], err = r.SVG(cs.SVGOptions{Theme: cs.ThemeAuto}); err != nil {
			return nil, err
		}
	}

	res.Files[Comment] = comment(res.Files[ReportMarkdown], res.Files[ScheduleText])

	return res, nil
}

// comment is the marker, the report and, for a valid request, the text form inside a fence, so nothing in it
// renders as Markdown. A forge step may append links to the files it uploaded.
func comment(report, text []byte) []byte {
	var b bytes.Buffer

	b.WriteString(Marker + "\n")
	b.Write(report)

	if text != nil {
		f := Fence(string(text))
		b.WriteString("\n<details open><summary>Preview</summary>\n\n" + f + "text\n")
		b.Write(text)
		b.WriteString(f + "\n\n</details>\n")
	}

	return b.Bytes()
}

// Fence returns a tilde fence longer than any tilde run in text, at least minFence long.
func Fence(text string) string {
	longest, run := 0, 0

	for _, c := range text {
		if c == '~' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}

	return strings.Repeat("~", max(minFence, longest+1))
}
