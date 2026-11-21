// Package review is the local part of reviewing a merge request (a pull request on GitHub) that changes a catalog:
// it checks every changed schedule file, applies the catalog's rules on top of the core's check, and writes one
// comment for the merge request. It touches neither the network nor the file system.
package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

// Marker starts the review comment, with a Stamp like the issue preview's. It ends in a space (see preview.Marker).
const Marker = "<!-- canned-schedules:merge-request-review "

// safePath is what a catalog file's path may contain to be shown as is; anything else could carry Markdown into the
// comment, so it is reported without being echoed.
var safePath = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// File is a changed schedule file: its path in the repository and its content at the merge request's head.
type File struct {
	Path string
	Src  []byte
}

// Result is the comment and what the forge step needs besides it.
type Result struct {
	// Comment is the whole comment, starting with the stamped marker.
	Comment []byte
	// Names are the schedules' names (H1s) of the valid files, in the order of the files.
	Names []string
	// Valid is true when every file passed.
	Valid bool
}

type checked struct {
	path, report, text, problem string
	ok                          bool
}

// Render checks files with opts (which must plan a preview, for the week in the comment). A file's name must be
// cs.FileName of its H1, so the catalog stays named after its schedules. tool goes into the stamp. The error is only
// for unusable options or a rendering bug; problems in the files are in the comment.
func Render(files []File, opts *cs.Options, fiddle *url.URL, tool string) (*Result, error) {
	res := &Result{Valid: true}
	results := make([]checked, 0, len(files))

	for _, f := range files {
		c, name, err := check(f, opts)
		if err != nil {
			return nil, err
		}

		if c.ok {
			res.Names = append(res.Names, name)
		} else {
			res.Valid = false
		}

		results = append(results, c)
	}

	stamp := preview.Stamp{Body: digest(files), Tool: preview.ToolName(tool), Args: preview.ArgsDigest(opts, fiddle)}
	res.Comment = comment(stamp, results)

	return res, nil
}

func check(f File, opts *cs.Options) (checked, string, error) {
	c := checked{path: f.Path}

	if !safePath.MatchString(f.Path) {
		c.problem = "A changed file has characters in its path that a catalog file can't have (letters, digits, " +
			"`.`, `_`, `-` and `/` only), so it isn't checked."

		return c, "", nil
	}

	r, err := cs.Evaluate(f.Src, opts)
	if err != nil {
		return c, "", err
	}

	md, err := r.Report(f.Path).Markdown()
	if err != nil {
		return c, "", err
	}

	c.report = string(md)

	if !r.Valid() {
		return c, "", nil
	}

	if want := cs.FileName(r.Output.Name); path.Base(f.Path) != want {
		c.problem = fmt.Sprintf("The file should be named `%s`: catalog files are named after the schedule's "+
			"title, so the playground and the catalog find it under that name.", want)

		return c, "", nil
	}

	text, err := r.Text(false)
	if err != nil {
		return c, "", err
	}

	c.text, c.ok = string(text), true

	return c, r.Output.Name, nil
}

// digest hashes the paths and contents, so a review of the same files at the same state can be left alone.
func digest(files []File) string {
	h := sha256.New()

	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(preview.BodyDigest(f.Src)))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

func comment(stamp preview.Stamp, results []checked) []byte {
	var b bytes.Buffer

	b.WriteString(stamp.LineFor(Marker) + "\n")
	b.WriteString("### Schedule review\n\n")

	failed := 0

	for _, c := range results {
		if !c.ok {
			failed++
		}
	}

	switch {
	case len(results) == 0:
		b.WriteString("This merge request changes no schedule files.\n")
	case failed == 0:
		fmt.Fprintf(&b, "%s checked, no problems found.\n", count(len(results)))
	default:
		fmt.Fprintf(&b, "%s checked, %d with problems.\n", count(len(results)), failed)
	}

	for _, c := range results {
		b.WriteString("\n" + section(c))
	}

	return b.Bytes()
}

func count(n int) string {
	if n == 1 {
		return "1 schedule"
	}

	return fmt.Sprintf("%d schedules", n)
}

func section(c checked) string {
	var b strings.Builder

	title := "a file with an unusual path"
	if safePath.MatchString(c.path) {
		title = "<code>" + c.path + "</code>"
	}

	mark := "Problems"
	if c.ok {
		mark = "Valid"
	}

	open := ""
	if !c.ok {
		open = " open"
	}

	fmt.Fprintf(&b, "<details%s><summary>%s: %s</summary>\n\n", open, mark, title)

	if c.report != "" {
		b.WriteString(c.report)

		if !strings.HasSuffix(c.report, "\n") {
			b.WriteString("\n")
		}
	}

	if c.problem != "" {
		b.WriteString("\n**Catalog:** " + c.problem + "\n")
	}

	if c.text != "" {
		f := preview.Fence(c.text)
		b.WriteString("\n" + f + "text\n" + c.text + f + "\n")
	}

	b.WriteString("\n</details>\n")

	return b.String()
}

// Title is the title a merge request gets from its schedules: the name of a single one, or the names joined.
func Title(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "Schedule: " + names[0]
	default:
		return "Schedules: " + strings.Join(names, ", ")
	}
}

// DefaultTitle reports whether title is one GitLab or GitHub wrote for a change made in the web editor ("Add new
// file", "Update schedules/x.canned-schedule.md", "Create …", "Edit …"), so renaming it overwrites nothing a person
// wrote.
func DefaultTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "Add new file" {
		return true
	}

	for _, verb := range []string{"Add ", "Update ", "Create ", "Edit ", "Delete "} {
		if rest, ok := strings.CutPrefix(title, verb); ok && strings.HasSuffix(rest, cs.FileSuffix) &&
			!strings.Contains(rest, " ") {
			return true
		}
	}

	return false
}
