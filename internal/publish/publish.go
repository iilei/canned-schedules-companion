// Package publish is the local part of step 3, publishing: it renders every schedule a merged merge request changed
// from the file as merged, compares its digest with the one the review recorded, and lays out the files, provenance,
// metadata and manifest under the paths a template names. It reads and returns bytes only; the command writes them,
// and the forge part finds the merge request and its review.
package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/preview"
)

// DefaultPath is the path template when the caller gives none: one folder per schedule and merge request, unique by
// content.
const DefaultPath = "{slug}/{mr}-{digest | truncate 19}"

// Manifest is the file at the output root that lists what was published.
const Manifest = "manifest.json"

// Version is the format version of the manifest and the provenance files.
const Version = 1

// Reasons why a published schedule doesn't carry the reviewed digest.
const (
	// ReasonNoReview: the merge request has no review comment.
	ReasonNoReview = "no-review"
	// ReasonNotReviewed: the review didn't cover this file.
	ReasonNotReviewed = "not-reviewed"
	// ReasonChanged: the file changed after the review (a rebase, an edit, a conflict resolved in the merge).
	ReasonChanged = "changed"
)

const (
	mdSuffix     = ".md"
	provSuffix   = ".provenance.json"
	mediaJSON    = "application/json"
	sha256Prefix = "sha256:"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var (
	errInput = errors.New("merge request file")
	errPath  = errors.New("path")
)

// MergeRequest is what the forge recorded about the merged merge request (a pull request on GitHub). Times are the
// forge's, never git's.
type MergeRequest struct {
	CreatedAt    time.Time `json:"created-at"`
	MergedAt     time.Time `json:"merged-at"`
	Forge        string    `json:"forge"`
	Host         string    `json:"host"`
	Project      string    `json:"project"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	MergedBy     string    `json:"merged-by"`
	TargetBranch string    `json:"target-branch"`
	// MergeCommit is for reference only; nothing is matched by it.
	MergeCommit string `json:"merge-commit"`
	Number      int    `json:"number"`
}

// Review is what the merge request's latest review comment recorded.
type Review struct {
	// Digests maps each reviewed schedule file's path to its digest.
	Digests map[string]string `json:"digests"`
	URL     string            `json:"url"`
}

// Input is the file publish fetch writes (merge-request.json); written by hand for a local run.
type Input struct {
	// Review is nil when the merge request has no review comment.
	Review       *Review      `json:"review"`
	MergeRequest MergeRequest `json:"merge-request"`
}

// File is a schedule file as merged: its path in the repository and its content.
type File struct {
	Path string
	Src  []byte
}

// Options are what Prepare renders and lays out with.
type Options struct {
	Eval     *cs.Options
	Path     *Template
	Metadata *Metadata
	// Tool and Core name the companion and the core with their versions, for the provenance.
	Tool, Core     string
	ForceOverwrite bool
}

// Result is what the command writes, or the problems that stop the publish.
type Result struct {
	// Files maps paths relative to the output directory to their content, the manifest included.
	Files map[string][]byte
	// Mismatches are the schedules whose digest isn't the reviewed one; the forge part posts a note for them.
	Mismatches []Entry
	// Problems are schedules that don't render without errors; then Files is nil.
	Problems []string
	// Warnings don't stop the publish (an S3 form left out).
	Warnings []string
}

// Entry is one published schedule in the manifest.
type Entry struct {
	// ReviewedDigest is the digest the review recorded for the path, nil when there is none.
	ReviewedDigest *string `json:"reviewed-digest"`
	// S3 is nil when the metadata exceeds S3's limit.
	S3       map[string]string `json:"s3"`
	Metadata map[string]string `json:"metadata"`
	Name     string            `json:"name"`
	Slug     string            `json:"slug"`
	Path     string            `json:"path"`
	Digest   string            `json:"digest"`
	// Reason is empty when Reviewed.
	Reason      string `json:"reason,omitempty"`
	Dir         string `json:"dir"`
	Artifactory string `json:"artifactory"`
	Files       []Out  `json:"files"`
	Reviewed    bool   `json:"reviewed"`
}

// Out is one published file.
type Out struct {
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	MediaType string `json:"media-type"`
}

type manifest struct {
	Schedules    []Entry      `json:"schedules"`
	MergeRequest MergeRequest `json:"merge-request"`
	Version      int          `json:"version"`
}

type provenance struct {
	Schedule     provSchedule `json:"schedule"`
	Review       provReview   `json:"review"`
	Publish      provPublish  `json:"publish"`
	Files        []Out        `json:"files"`
	MergeRequest MergeRequest `json:"merge-request"`
	Version      int          `json:"version"`
}

type provSchedule struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type provReview struct {
	Digest   *string `json:"digest"`
	Reason   string  `json:"reason,omitempty"`
	URL      string  `json:"url,omitempty"`
	Reviewed bool    `json:"reviewed"`
}

type provPublish struct {
	Tool       string `json:"tool"`
	Core       string `json:"core"`
	ArgsDigest string `json:"args-digest"`
}

type rendered struct {
	files map[string][]byte
	entry Entry
}

type named struct {
	name, mediaType string
	data            []byte
}

// ParseInput reads merge-request.json. Unknown keys are errors, so a misspelled one in a hand-written file shows.
func ParseInput(raw []byte) (*Input, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()

	var in Input
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("%w: %w", errInput, err)
	}

	mr := &in.MergeRequest

	switch {
	case mr.Number < 1:
		return nil, fmt.Errorf("%w: merge-request.number must be a positive number", errInput)
	case mr.CreatedAt.IsZero() || mr.MergedAt.IsZero():
		return nil, fmt.Errorf("%w: merge-request.created-at and merged-at are required (RFC 3339)", errInput)
	}

	mr.CreatedAt, mr.MergedAt = mr.CreatedAt.UTC(), mr.MergedAt.UTC()

	if in.Review != nil {
		for p, d := range in.Review.Digests {
			if !IsDigest(d) {
				return nil, fmt.Errorf("%w: review.digests[%q] is not a sha256:<64 hex> digest", errInput, p)
			}
		}
	}

	return &in, nil
}

// IsDigest reports whether s is a digest as Digest writes them.
func IsDigest(s string) bool { return digestPattern.MatchString(s) }

// Digest is a content digest as Docker writes them: sha256:<64 hex>.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)

	return sha256Prefix + hex.EncodeToString(sum[:])
}

// Prepare renders each file and lays out the result. One schedule is the unit: the path template names each
// schedule's folder, and the manifest always lists the schedules, one or many. The error is for unusable options, a
// path that leaves the output directory or a collision; schedules that don't render are Result.Problems.
func Prepare(in *Input, files []File, opts *Options) (*Result, error) {
	o := *opts
	o.Eval = withPreview(opts.Eval)
	opts = &o

	res := &Result{}
	all := make([]rendered, 0, len(files))
	seen := map[string]bool{}

	for _, f := range files {
		if seen[f.Path] {
			return nil, fmt.Errorf("%s is given twice", f.Path)
		}

		seen[f.Path] = true

		r, problem, err := render(in, f, opts, res)
		if err != nil {
			return nil, err
		}

		if problem != "" {
			res.Problems = append(res.Problems, problem)

			continue
		}

		if !r.entry.Reviewed {
			res.Mismatches = append(res.Mismatches, r.entry)
		}

		all = append(all, r)
	}

	if len(res.Problems) > 0 {
		return res, nil
	}

	out, entries, err := merge(all, opts.ForceOverwrite)
	if err != nil {
		return nil, err
	}

	raw, err := json.MarshalIndent(manifest{Version: Version, MergeRequest: in.MergeRequest, Schedules: entries}, "",
		"  ")
	if err != nil {
		return nil, err
	}

	out[Manifest] = append(raw, '\n')
	res.Files = out

	return res, nil
}

// withPreview plans the default preview when eval doesn't: the text form and the SVG need one.
func withPreview(eval *cs.Options) *cs.Options {
	var e cs.Options
	if eval != nil {
		e = *eval
	}

	if e.Preview == nil {
		e.Preview = &cs.PreviewOptions{Downsample: cs.DefaultDownsample}
	}

	return &e
}

// merge collects every schedule's files, failing on two that write one path unless force; then the later wins and
// the earlier entry no longer lists the file.
func merge(all []rendered, force bool) (map[string][]byte, []Entry, error) {
	out := map[string][]byte{}
	owner := map[string]int{}
	entries := make([]Entry, len(all))

	for i := range all {
		r := &all[i]
		entries[i] = r.entry

		for _, o := range r.entry.Files {
			if o.Path == Manifest || strings.HasPrefix(o.Path, Manifest+"/") {
				return nil, nil, fmt.Errorf("%w %s: the output root's %s is the manifest", errPath, o.Path, Manifest)
			}

			if j, ok := owner[o.Path]; ok {
				if !force {
					return nil, nil, fmt.Errorf("%s and %s both write %s; change the path template or pass "+
						"--force-overwrite", all[j].entry.Path, r.entry.Path, o.Path)
				}

				entries[j].Files = dropFile(entries[j].Files, o.Path)
			}

			owner[o.Path] = i
			out[o.Path] = r.files[o.Path]
		}
	}

	return out, entries, nil
}

func dropFile(files []Out, p string) []Out {
	kept := make([]Out, 0, len(files))

	for _, f := range files {
		if f.Path != p {
			kept = append(kept, f)
		}
	}

	return kept
}

func render(in *Input, f File, opts *Options, res *Result) (rendered, string, error) {
	r, err := cs.Evaluate(f.Src, opts.Eval)
	if err != nil {
		return rendered{}, "", err
	}

	if !r.Valid() {
		text, err := r.Report(f.Path).Text()
		if err != nil {
			return rendered{}, "", err
		}

		return rendered{}, fmt.Sprintf("%s doesn't render without errors:\n%s", f.Path, text), nil
	}

	fileName := cs.FileName(r.Output.Name)
	if path.Base(f.Path) != fileName {
		return rendered{}, fmt.Sprintf("%s should be named %s, after the schedule's title", f.Path, fileName), nil
	}

	outputs, err := outputs(r, f.Src)
	if err != nil {
		return rendered{}, "", err
	}

	e := entry(in, f, r.Output.Name)
	vars := variables(in, &e, opts)

	if e.Dir, err = dir(opts.Path.Execute(vars)); err != nil {
		return rendered{}, "", err
	}

	pairs, err := opts.Metadata.Execute(vars)
	if err != nil {
		return rendered{}, "", err
	}

	e.Metadata = map[string]string{}
	for _, p := range pairs {
		e.Metadata[p.Key] = p.Value
	}

	e.Artifactory = Artifactory(pairs)

	s3, ok := S3(pairs)
	if ok {
		e.S3 = s3
	} else {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: the metadata exceeds S3's 2 KB, so the manifest has "+
			"no s3 form for it", f.Path))
	}

	files, err := lay(&e, in, opts, outputs)
	if err != nil {
		return rendered{}, "", err
	}

	return rendered{entry: e, files: files}, "", nil
}

// outputs are the files of one schedule, named as in the catalog, in a fixed order.
func outputs(r *cs.Result, src []byte) ([]named, error) {
	js, err := r.JSON()
	if err != nil {
		return nil, err
	}

	svg, err := r.SVG(cs.SVGOptions{Theme: cs.ThemeAuto})
	if err != nil {
		return nil, err
	}

	text, err := r.Text(false)
	if err != nil {
		return nil, err
	}

	stem := strings.TrimSuffix(cs.FileName(r.Output.Name), mdSuffix)

	return []named{
		{stem + mdSuffix, "text/markdown; charset=utf-8", src},
		{stem + ".json", mediaJSON, js},
		{stem + ".svg", "image/svg+xml", svg},
		{stem + ".txt", "text/plain; charset=utf-8", text},
	}, nil
}

func entry(in *Input, f File, name string) Entry {
	e := Entry{
		Name:   name,
		Slug:   strings.TrimSuffix(cs.FileName(name), cs.FileSuffix),
		Path:   f.Path,
		Digest: Digest(f.Src),
	}

	if in.Review == nil {
		e.Reason = ReasonNoReview

		return e
	}

	reviewed, ok := in.Review.Digests[f.Path]

	switch {
	case !ok:
		e.Reason = ReasonNotReviewed
	case reviewed != e.Digest:
		e.ReviewedDigest, e.Reason = &reviewed, ReasonChanged
	default:
		e.ReviewedDigest, e.Reviewed = &reviewed, true
	}

	return e
}

// lay writes the files and the provenance into e's folder.
func lay(e *Entry, in *Input, opts *Options, outputs []named) (map[string][]byte, error) {
	files := map[string][]byte{}
	prov := provenance{
		Version:      Version,
		Schedule:     provSchedule{Name: e.Name, Path: e.Path, Digest: e.Digest},
		Review:       provReview{Reviewed: e.Reviewed, Digest: e.ReviewedDigest, Reason: e.Reason},
		MergeRequest: in.MergeRequest,
		Publish: provPublish{
			Tool: opts.Tool, Core: opts.Core, ArgsDigest: sha256Prefix + preview.ArgsDigest(opts.Eval, nil),
		},
	}

	if in.Review != nil {
		prov.Review.URL = in.Review.URL
	}

	for _, o := range outputs {
		p := path.Join(e.Dir, o.name)
		out := Out{Path: p, Digest: Digest(o.data), MediaType: o.mediaType}

		files[p] = o.data
		e.Files = append(e.Files, out)
		prov.Files = append(prov.Files, Out{Path: o.name, Digest: out.Digest, MediaType: o.mediaType})
	}

	raw, err := json.MarshalIndent(prov, "", "  ")
	if err != nil {
		return nil, err
	}

	raw = append(raw, '\n')
	name := strings.TrimSuffix(cs.FileName(e.Name), mdSuffix) + provSuffix
	p := path.Join(e.Dir, name)

	files[p] = raw
	e.Files = append(e.Files, Out{Path: p, Digest: Digest(raw), MediaType: mediaJSON})

	return files, nil
}

func variables(in *Input, e *Entry, opts *Options) map[string]string {
	mr := in.MergeRequest

	return map[string]string{
		"name":         e.Name,
		"slug":         e.Slug,
		"path":         e.Path,
		"digest":       e.Digest,
		"reviewed":     strconv.FormatBool(e.Reviewed),
		"mr":           strconv.Itoa(mr.Number),
		"title":        mr.Title,
		"author":       mr.Author,
		"merged-by":    mr.MergedBy,
		"created-at":   mr.CreatedAt.Format(time.RFC3339),
		"merged-at":    mr.MergedAt.Format(time.RFC3339),
		"merged-date":  mr.MergedAt.Format(time.DateOnly),
		"forge":        mr.Forge,
		"host":         mr.Host,
		"project":      mr.Project,
		"branch":       mr.TargetBranch,
		"tool-version": opts.Tool,
		"core-version": opts.Core,
	}
}

// dir checks a filled path template: relative, inside the output directory, and nothing a file system or an object
// store would read differently. Designing the layout is otherwise the user's job.
func dir(p string) (string, error) {
	switch {
	case p == "":
		return "", fmt.Errorf("%w: the path template gives an empty path", errPath)
	case strings.HasPrefix(p, "/"), strings.Contains(p, `\`):
		return "", fmt.Errorf("%w %q: want a relative path with / between folders", errPath, p)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return "", fmt.Errorf("%w %q: holds a control character", errPath, p)
	}

	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w %q: empty, . and .. folders aren't allowed, so the path stays inside the "+
				"output directory", errPath, p)
		}
	}

	return p, nil
}
