// Package preview is the local part of step 1, issue feedback: it evaluates a schedule proposal (an issue body) and
// produces the files a forge step posts or uploads, without touching the network or the file system.
package preview

import (
	"bytes"
	"net/url"
	"strings"

	cs "github.com/iilei/canned-schedules"
)

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

// NoSchedule is the comment's text, after the marker line, for a proposal that holds no schedule directive: an issue
// that only asks about the syntax, or one whose schedule was edited away. Forge steps post it only over an earlier
// comment, so the issue keeps no stale preview, and never start a comment with it.
const NoSchedule = "This issue holds no schedule (any more), so there is nothing to check.\n"

// Names lists every file Render can produce, so a caller can remove stale ones first.
var Names = []string{ReportMarkdown, ReportJSON, Comment, ScheduleJSON, ScheduleText, ScheduleSVG}

// Result is what Render produced: file name → content. A valid schedule has every file, an invalid one only the
// reports and the comment.
type Result struct {
	Files map[string][]byte
	Valid bool
	// Schedule is false for text without a schedule directive (Result.HasDirectives in the core): it gets the
	// reports and the NoSchedule comment, and is neither valid nor an error.
	Schedule bool
}

// Render evaluates src (named file in the reports) with opts, which must plan a preview (opts.Preview). With a
// fiddle base (opt-in: a preflight page the team hosts), the comment links to it with the document: the canonical
// form of a valid schedule, the schedule as written otherwise, so the playground shows its errors at their lines.
// tool names the program and its version in the comment's stamp. The error is only for unusable options or a
// rendering bug; document problems are in the reports and the comment.
func Render(src []byte, file string, opts *cs.Options, fiddle *url.URL, tool string) (*Result, error) {
	r, err := cs.Evaluate(src, opts)
	if err != nil {
		return nil, err
	}

	rep := r.Report(file)
	res := &Result{Valid: r.Valid(), Schedule: r.HasDirectives(), Files: map[string][]byte{}}

	if res.Files[ReportMarkdown], err = rep.Markdown(); err != nil {
		return nil, err
	}

	if res.Files[ReportJSON], err = rep.JSON(); err != nil {
		return nil, err
	}

	stamp := Stamp{Body: BodyDigest(src), Tool: ToolName(tool), Args: ArgsDigest(opts, fiddle)}

	if !res.Schedule {
		res.Valid = false
		res.Files[Comment] = []byte(stamp.Line() + "\n" + NoSchedule)

		return res, nil
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

	res.Files[Comment] = comment(stamp, res.Files[ReportMarkdown], res.Files[ScheduleText])

	if fiddle != nil {
		doc := src

		if r.Valid() {
			if doc, err = r.Canonical(); err != nil {
				return nil, err
			}
		}

		res.Files[Comment] = append(res.Files[Comment], fiddleLine(fiddle, doc, opts)...)
	}

	return res, nil
}

// comment is the stamped marker, the report and, for a valid schedule, the text form inside a fence, so nothing in it
// renders as Markdown. A forge step may append links to the files it uploaded.
func comment(stamp Stamp, report, text []byte) []byte {
	var b bytes.Buffer

	b.WriteString(stamp.Line() + "\n")
	b.Write(report)

	if text != nil {
		f := Fence(string(text))
		b.WriteString("\n<details open><summary>Preview</summary>\n\n" + f + "text\n")
		b.Write(text)
		b.WriteString(f + "\n\n</details>\n")
	}

	return b.Bytes()
}

// IsNoSchedule reports whether comment is the NoSchedule comment Render wrote for text without a schedule.
func IsNoSchedule(comment []byte) bool {
	_, rest, ok := strings.Cut(string(comment), "\n")

	return ok && rest == NoSchedule
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
