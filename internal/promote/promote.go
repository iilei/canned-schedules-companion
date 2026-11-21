// Package promote is the local part of step 2: it turns an approved issue's text into the catalog file a pull or
// merge request adds. It reads and returns bytes only; the forge part lives in the command.
package promote

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/review"
)

// Files that Prepare's result is written to, for promote open.
const (
	// JSON holds valid, schedule, name, path and title.
	JSON = "promote.json"
	// Schedule is the catalog file's text.
	Schedule = "schedule.md"
	// Report is the core's Markdown report.
	Report = "report.md"
)

// catalogDir is the rule site.json uses for catalog.dir: relative segments, none starting with a dot.
var catalogDir = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(?:/[A-Za-z0-9_][A-Za-z0-9._-]*)*$`)

var errCatalogDir = errors.New("want a relative folder such as schedules")

// Result is what promote open needs.
type Result struct {
	// Name is the schedule's name (its H1), empty unless Valid.
	Name string `json:"name"`
	// Path is the catalog file, catalogDir/<file name of the H1>, empty unless Valid.
	Path string `json:"path"`
	// Title names the pull or merge request after the schedule, as the review does.
	Title string `json:"title"`
	// Content is the file's text: the issue's text as written, with LF line endings and a final newline.
	Content []byte `json:"-"`
	// Report is the core's Markdown report.
	Report []byte `json:"-"`
	// Valid means the text holds a schedule without errors.
	Valid bool `json:"valid"`
	// Schedule is false when the text holds no schedule directive at all.
	Schedule bool `json:"schedule"`
}

// Prepare checks the issue's text with the catalog's options and names the file it becomes. The text stays as the
// author wrote it (decided 2026-11-21: verbatim, as the playground proposes), apart from line endings: issue bodies
// arrive with CRLF, and a catalog file has LF and ends with a newline.
func Prepare(body []byte, file string, opts *cs.Options, dir string) (*Result, error) {
	if dir == "" {
		dir = "schedules"
	}

	if !catalogDir.MatchString(dir) {
		return nil, fmt.Errorf("catalog dir %q: %w", dir, errCatalogDir)
	}

	content := Normalize(body)

	res, err := cs.Evaluate(content, opts)
	if err != nil {
		return nil, err
	}

	report, err := res.Report(file).Markdown()
	if err != nil {
		return nil, err
	}

	r := &Result{Content: content, Report: report, Schedule: res.HasDirectives()}
	if r.Schedule && res.Valid() {
		r.Valid = true
		r.Name = res.Output.Name
		r.Path = path.Join(dir, cs.FileName(r.Name))
		r.Title = review.Title([]string{r.Name})
	}

	return r, nil
}

// Normalize gives text LF line endings and exactly one final newline.
func Normalize(body []byte) []byte {
	out := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\r"), []byte("\n"))
	out = bytes.TrimRight(out, "\n")

	return append(out, '\n')
}
