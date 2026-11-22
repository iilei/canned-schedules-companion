package publish_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

const (
	night = "# Night Shift\n\nWhy: the desk needs cover.\n\n> [!SCHEDULE-STATE] Medium\n" +
		"> - `22:00` to `06:00` MON-FRI\n"
	peak = "# Friday Peak\n\n> [!SCHEDULE-STATE] Maximum\n> - `16:00` to `20:00` FRI\n"

	nightPath = "schedules/night-shift.canned-schedule.md"
	peakPath  = "schedules/friday-peak.canned-schedule.md"
)

const inputJSON = `{
  "merge-request": {
    "forge": "gitlab", "host": "gitlab.example.com", "project": "ops/catalog", "number": 42,
    "url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42", "title": "Schedule: Night Shift",
    "author": "ana", "merged-by": "Jürgen", "created-at": "2026-11-20T09:15:00+01:00",
    "merged-at": "2026-11-22T14:30:00Z", "target-branch": "main", "merge-commit": "abc123"
  },
  "review": {"url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42#note_7", "digests": {}}
}`

type manifest struct {
	Schedules []publish.Entry `json:"schedules"`
	Version   int             `json:"version"`
}

func input(t *testing.T, reviewed map[string]string) *publish.Input {
	t.Helper()

	in, err := publish.ParseInput([]byte(inputJSON))
	if err != nil {
		t.Fatal(err)
	}

	in.Review.Digests = reviewed

	return in
}

func options(t *testing.T, path, meta string) *publish.Options {
	t.Helper()

	p, err := publish.Parse(path)
	if err != nil {
		t.Fatal(err)
	}

	m, err := publish.ParseMetadata(meta)
	if err != nil {
		t.Fatal(err)
	}

	return &publish.Options{
		Path:     p,
		Metadata: m,
		Tool:     "canned-schedules-companion test",
		Core:     "canned-schedules test",
	}
}

func readManifest(t *testing.T, r *publish.Result) manifest {
	t.Helper()

	var m manifest
	if err := json.Unmarshal(r.Files[publish.Manifest], &m); err != nil {
		t.Fatal(err)
	}

	return m
}

func TestPrepareReviewed(t *testing.T) {
	t.Parallel()

	digest := publish.Digest([]byte(night))
	in := input(t, map[string]string{nightPath: digest})

	r, err := publish.Prepare(in, []publish.File{{Path: nightPath, Src: []byte(night)}},
		options(t, publish.DefaultPath, publish.DefaultMetadata))
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Problems) > 0 || len(r.Mismatches) > 0 {
		t.Fatalf("problems %v, mismatches %v", r.Problems, r.Mismatches)
	}

	dir := "night-shift/42-" + digest[:19]
	for _, name := range []string{".md", ".json", ".svg", ".txt", ".provenance.json"} {
		if _, ok := r.Files[dir+"/night-shift.canned-schedule"+name]; !ok {
			t.Errorf("missing %s%s; have %v", dir, name, keys(r.Files))
		}
	}

	if string(r.Files[dir+"/night-shift.canned-schedule.md"]) != night {
		t.Error("the raw input isn't the file as merged")
	}

	m := readManifest(t, r)
	if m.Version != publish.Version || len(m.Schedules) != 1 {
		t.Fatalf("manifest %+v", m)
	}

	e := m.Schedules[0]
	if !e.Reviewed || e.Reason != "" || e.Dir != dir || e.Slug != "night-shift" || len(e.Files) != 5 {
		t.Errorf("entry %+v", e)
	}

	if e.Metadata["merged-by"] != "Jürgen" || e.Metadata["merge-request"] != "42" || e.Metadata["reviewed"] != "true" {
		t.Errorf("metadata %v", e.Metadata)
	}

	if !strings.Contains(e.Artifactory, ";merged-by=J%C3%BCrgen;") ||
		!strings.Contains(e.Artifactory, ";merged-at=2026-11-22T14%3A30%3A00Z;") {
		t.Errorf("artifactory %q", e.Artifactory)
	}

	if e.S3["merged-by"] != "=?UTF-8?b?SsO8cmdlbg==?=" || e.S3["schedule"] != "Night Shift" {
		t.Errorf("s3 %v", e.S3)
	}
}

func TestPrepareProvenance(t *testing.T) {
	t.Parallel()

	in := input(t, map[string]string{nightPath: publish.Digest([]byte("an older version\n"))})

	r, err := publish.Prepare(in, []publish.File{{Path: nightPath, Src: []byte(night)}},
		options(t, "{slug}", "schedule={name}"))
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Mismatches) != 1 || r.Mismatches[0].Reason != publish.ReasonChanged {
		t.Fatalf("mismatches %+v", r.Mismatches)
	}

	var prov struct {
		Review struct {
			Digest   *string `json:"digest"`
			Reason   string  `json:"reason"`
			URL      string  `json:"url"`
			Reviewed bool    `json:"reviewed"`
		} `json:"review"`
		MergeRequest struct {
			CreatedAt string `json:"created-at"`
		} `json:"merge-request"`
		Files []publish.Out `json:"files"`
	}

	if err := json.Unmarshal(r.Files["night-shift/night-shift.canned-schedule.provenance.json"], &prov); err != nil {
		t.Fatal(err)
	}

	if prov.Review.Reviewed || prov.Review.Reason != publish.ReasonChanged || prov.Review.Digest == nil ||
		!strings.HasSuffix(prov.Review.URL, "#note_7") {
		t.Errorf("review %+v", prov.Review)
	}

	// The forge's times, in UTC.
	if prov.MergeRequest.CreatedAt != "2026-11-20T08:15:00Z" {
		t.Errorf("created-at %s", prov.MergeRequest.CreatedAt)
	}

	// The provenance lists the other four files by name, with the digests of their bytes.
	if len(prov.Files) != 4 || prov.Files[1].Path != "night-shift.canned-schedule.json" ||
		prov.Files[1].Digest != publish.Digest(r.Files["night-shift/night-shift.canned-schedule.json"]) {
		t.Errorf("files %+v", prov.Files)
	}
}

func TestPrepareReasons(t *testing.T) {
	t.Parallel()

	files := []publish.File{{Path: nightPath, Src: []byte(night)}, {Path: peakPath, Src: []byte(peak)}}

	in := input(t, map[string]string{nightPath: publish.Digest([]byte(night))})

	r, err := publish.Prepare(in, files, options(t, "{slug}", ""))
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Mismatches) != 1 || r.Mismatches[0].Path != peakPath ||
		r.Mismatches[0].Reason != publish.ReasonNotReviewed {
		t.Errorf("mismatches %+v", r.Mismatches)
	}

	if m := readManifest(t, r); len(m.Schedules) != 2 || m.Schedules[0].Path != nightPath {
		t.Errorf("manifest %+v", m)
	}

	in.Review = nil

	if r, err = publish.Prepare(in, files, options(t, "{slug}", "")); err != nil || len(r.Mismatches) != 2 ||
		r.Mismatches[0].Reason != publish.ReasonNoReview {
		t.Errorf("no review: %+v, %v", r, err)
	}

	// No schedule changed: an empty list, never a missing one.
	if r, err = publish.Prepare(in, nil, options(t, "{slug}", "")); err != nil ||
		!strings.Contains(string(r.Files[publish.Manifest]), `"schedules": []`) {
		t.Errorf("no files: %s, %v", r.Files[publish.Manifest], err)
	}
}

func TestPrepareCollision(t *testing.T) {
	t.Parallel()

	files := []publish.File{{Path: nightPath, Src: []byte(night)}, {
		Path: "other/night-shift.canned-schedule.md",
		Src:  []byte(night),
	}}

	in := input(t, nil)

	_, err := publish.Prepare(in, files, options(t, "{mr}", ""))
	if err == nil || !strings.Contains(err.Error(), "--force-overwrite") {
		t.Fatalf("want a collision error, got %v", err)
	}

	opts := options(t, "{mr}", "")
	opts.ForceOverwrite = true

	r, err := publish.Prepare(in, files, opts)
	if err != nil {
		t.Fatal(err)
	}

	// The later schedule owns the files; the earlier entry lists none of them.
	if m := readManifest(t, r); len(m.Schedules[0].Files) != 0 || len(m.Schedules[1].Files) != 5 {
		t.Errorf("manifest %+v", m)
	}
}

func TestPrepareProblems(t *testing.T) {
	t.Parallel()

	in := input(t, nil)

	for name, f := range map[string]publish.File{
		"invalid":   {Path: nightPath, Src: []byte("# Night Shift!\n\n> [!SCHEDULE-STATE] Medium\n")},
		"misnamed":  {Path: "schedules/late.canned-schedule.md", Src: []byte(night)},
		"duplicate": {Path: nightPath, Src: []byte(night)},
	} {
		files := []publish.File{f}
		if name == "duplicate" {
			files = append(files, f)
		}

		r, err := publish.Prepare(in, files, options(t, "{slug}", ""))

		switch {
		case name == "duplicate":
			if err == nil {
				t.Errorf("%s: want an error", name)
			}
		case err != nil || len(r.Problems) != 1 || r.Files != nil:
			t.Errorf("%s: %+v, %v", name, r, err)
		}
	}
}

func TestPreparePath(t *testing.T) {
	t.Parallel()

	in := input(t, nil)
	files := []publish.File{{Path: nightPath, Src: []byte(night)}}

	for _, tmpl := range []string{"/{slug}", "{slug}/../..", "{slug}//x", `a\{slug}`, "./{slug}", "{title}/x"} {
		if tmpl == "{title}/x" {
			in.MergeRequest.Title = "a/../../b"
		}

		if _, err := publish.Prepare(in, files, options(t, tmpl, "")); err == nil {
			t.Errorf("%q: want an error", tmpl)
		}
	}

	r, err := publish.Prepare(input(t, nil), files, options(t,
		"{host | reverseByDotDelim}/{project}/{name | toLower}/{merged-date}", ""))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Files["com.example.gitlab/ops/catalog/night shift/2026-11-22/night-shift.canned-schedule.json"]; !ok {
		t.Errorf("files %v", keys(r.Files))
	}
}

func TestParseInput(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"unknown key": `{"merge-request": {"number": 1, "created-at": "2026-11-20T09:15:00Z", ` +
			`"merged-at": "2026-11-20T09:15:00Z", "nmber": 2}, "review": null}`,
		"no number": `{"merge-request": {"created-at": "2026-11-20T09:15:00Z", "merged-at": "2026-11-20T09:15:00Z"}}`,
		"no times":  `{"merge-request": {"number": 1}}`,
		"bad digest": `{"merge-request": {"number": 1, "created-at": "2026-11-20T09:15:00Z", ` +
			`"merged-at": "2026-11-20T09:15:00Z"}, "review": {"digests": {"a": "abc"}}}`,
	} {
		if _, err := publish.ParseInput([]byte(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}
