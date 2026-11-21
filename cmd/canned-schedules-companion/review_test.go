package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeMR is a minimal GitLab merge request !1 in group/catalog: its title, head, labels, notes and a write log.
type fakeMR struct {
	title, sha string
	labels     []string
	notes      []string
	writes     []string
	mu         sync.Mutex
}

// fakePR is a minimal GitHub pull request #1 in o/catalog: title, head, labels, comments and a write log.
type fakePR struct {
	title, sha string
	labels     []string
	comments   []string
	writes     []string
	mu         sync.Mutex
}

func (f *fakeMR) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const mr = "/projects/group%2Fcatalog/merge_requests/1"

	switch path := r.URL.EscapedPath(); {
	case r.Header.Get("PRIVATE-TOKEN") != "secret":
		w.WriteHeader(http.StatusUnauthorized)
	case path == "/user":
		_, _ = w.Write([]byte(`{"username": "project_1_bot"}`))
	case path == mr && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": f.title, "sha": f.sha, "labels": f.labels,
			"web_url": "https://gitlab.example/group/catalog/-/merge_requests/1",
		})
	case path == mr && r.Method == http.MethodPut:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)

		switch {
		case in["add_labels"] != "":
			f.labels = append(f.labels, in["add_labels"])
			f.writes = append(f.writes, "label +"+in["add_labels"])
		case in["remove_labels"] != "":
			f.labels = slices.DeleteFunc(f.labels, func(l string) bool { return l == in["remove_labels"] })
			f.writes = append(f.writes, "label -"+in["remove_labels"])
		case in["title"] != "":
			f.title = in["title"]
			f.writes = append(f.writes, "title "+in["title"])
		}

		_, _ = w.Write([]byte("{}"))
	case path == mr+"/notes" && r.Method == http.MethodGet:
		list := make([]map[string]any, 0, len(f.notes))
		for _, n := range f.notes {
			list = append(
				list,
				map[string]any{"id": 9, "body": n, "author": map[string]string{"username": "project_1_bot"}},
			)
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == mr+"/notes" || path == mr+"/notes/9":
		var in struct {
			Body string `json:"body"`
		}

		_ = json.NewDecoder(r.Body).Decode(&in)

		if r.Method == http.MethodPost {
			f.notes = append(f.notes, in.Body)
			f.writes = append(f.writes, "create")
		} else {
			f.notes[0] = in.Body
			f.writes = append(f.writes, "update")
		}

		_, _ = w.Write([]byte(`{"id": 9}`))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

func writeSchedule(t *testing.T, dir, name, src string) string {
	t.Helper()

	p := filepath.Join(dir, "schedules", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestReview(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	f := &fakeMR{title: "Add new file", sha: "abc"}
	srv := httptest.NewServer(f)

	defer srv.Close()

	// Relative paths, as git diff names the files in the job (an absolute path on Windows has a colon, which a
	// catalog path can't have).
	dir := t.TempDir()
	t.Chdir(dir)
	writeSchedule(t, dir, "night-shift.canned-schedule.md",
		"# Night Shift\n\n> [!SCHEDULE-STATE] Medium\n> - `22:00` to `06:00` MON-FRI\n")

	file := "schedules/night-shift.canned-schedule.md"
	out := "out"

	var stdout, stderr bytes.Buffer
	if code := run([]string{"review", "render", "--out-dir", out, file}, &stdout, &stderr); code != exitOK {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}

	post := []string{
		"review", "post", "--forge", "gitlab", "--repo", "group/catalog", "--merge-request", "1",
		"--head-sha", "abc", "--api-url", srv.URL, "--label", "Schedule", "--retitle",
	}

	if o := postReviewTo(
		t,
		append(post, "--dry-run", out)...); len(f.writes) != 0 ||
		!strings.Contains(o, "would create") {
		t.Errorf("dry run: %q, writes %q", o, f.writes)
	}

	postReviewTo(t, append(post, out)...)

	if !slices.Equal(f.writes, []string{"create", "label +Schedule", "title Schedule: Night Shift"}) {
		t.Fatalf("writes %q", f.writes)
	}

	if o := postReviewTo(t, append(post, out)...); !strings.Contains(o, "up to date") || len(f.writes) != 3 {
		t.Errorf("same files: %q, writes %q", o, f.writes)
	}

	stale := slices.Clone(post)
	stale[slices.Index(stale, "abc")] = "def"

	if o := postReviewTo(t, append(stale, out)...); !strings.Contains(o, "moved on") || len(f.writes) != 3 {
		t.Errorf("stale: %q, writes %q", o, f.writes)
	}

	// A broken file: the review says so, the label goes, a title someone wrote stays.
	f.title = "Night shift for the support desk"
	writeSchedule(t, dir, "night-shift.canned-schedule.md", "# Night Shift!\n")

	if code := run([]string{"review", "render", "--out-dir", out, file}, &stdout, &stderr); code != exitInvalid {
		t.Fatalf("render broken: exit %d", code)
	}

	postReviewTo(t, append(post, out)...)

	if !slices.Equal(f.writes[3:], []string{"update", "label -Schedule"}) ||
		f.title != "Night shift for the support desk" {
		t.Errorf("broken: writes %q, title %q", f.writes, f.title)
	}
}

func postReviewTo(t *testing.T, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("%q: exit %d: %s", args, code, stderr.String())
	}

	return stdout.String()
}

func TestReviewUsage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for name, args := range map[string][]string{
		"forge":    {"review", "post", "--forge", "bitbucket", "--repo", "o/r", "--merge-request", "1", dir},
		"no forge": {"review", "post", "--repo", "g/p", "--merge-request", "1", dir},
		"no mr":    {"review", "post", "--forge", "gitlab", "--repo", "g/p", dir},
		"no file":  {"review", "render", "--out-dir", dir, filepath.Join(dir, "missing.canned-schedule.md")},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func (f *fakePR) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const repo = "/repos/o/catalog"

	switch path := r.URL.EscapedPath(); {
	case r.Header.Get("Authorization") != "Bearer secret":
		w.WriteHeader(http.StatusUnauthorized)
	case path == repo+"/pulls/1" && r.Method == http.MethodGet:
		labels := make([]map[string]string, 0, len(f.labels))
		for _, l := range f.labels {
			labels = append(labels, map[string]string{"name": l})
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": f.title, "head": map[string]string{"sha": f.sha}, "labels": labels,
		})
	case path == repo+"/pulls/1" && r.Method == http.MethodPatch:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.title = in["title"]
		f.writes = append(f.writes, "title "+in["title"])
		_, _ = w.Write([]byte("{}"))
	case path == repo+"/issues/1/labels" && r.Method == http.MethodPost:
		var in map[string][]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.labels = append(f.labels, in["labels"]...)
		f.writes = append(f.writes, "label +"+strings.Join(in["labels"], ","))
		_, _ = w.Write([]byte("[]"))
	case strings.HasPrefix(path, repo+"/issues/1/labels/") && r.Method == http.MethodDelete:
		l := strings.TrimPrefix(path, repo+"/issues/1/labels/")
		f.labels = slices.DeleteFunc(f.labels, func(x string) bool { return x == l })
		f.writes = append(f.writes, "label -"+l)
		_, _ = w.Write([]byte("[]"))
	case path == repo+"/issues/1/comments" && r.Method == http.MethodGet:
		list := make([]map[string]any, 0, len(f.comments))
		for _, c := range f.comments {
			list = append(list, map[string]any{
				"id": 7, "body": c, "user": map[string]string{"login": "github-actions[bot]"},
				"html_url": "https://github.example/o/catalog/pull/1#issuecomment-7",
			})
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == repo+"/issues/1/comments" && r.Method == http.MethodPost,
		path == repo+"/issues/comments/7" && r.Method == http.MethodPatch:
		var in struct {
			Body string `json:"body"`
		}

		_ = json.NewDecoder(r.Body).Decode(&in)

		if r.Method == http.MethodPost {
			f.comments = append(f.comments, in.Body)
			f.writes = append(f.writes, "create")
		} else {
			f.comments[0] = in.Body
			f.writes = append(f.writes, "update")
		}

		_, _ = w.Write([]byte(`{"id": 7, "html_url": "https://github.example/o/catalog/pull/1#issuecomment-7"}`))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

// TestReviewGitHub: the same review on a pull request, through GitHub's API with the workflow token's defaults.
func TestReviewGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")

	f := &fakePR{title: "Create night-shift.canned-schedule.md", sha: "abc"}
	srv := httptest.NewServer(f)

	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	writeSchedule(t, dir, "night-shift.canned-schedule.md",
		"# Night Shift\n\n> [!SCHEDULE-STATE] Medium\n> - `22:00` to `06:00` MON-FRI\n")

	var stdout, stderr bytes.Buffer
	if code := run(
		[]string{"review", "render", "--out-dir", "out", "schedules/night-shift.canned-schedule.md"}, &stdout, &stderr,
	); code != exitOK {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}

	post := []string{
		"review", "post", "--forge", "github", "--repo", "o/catalog", "--merge-request", "1",
		"--head-sha", "abc", "--api-url", srv.URL, "--label", "Schedule", "--retitle", "out",
	}

	if o := postReviewTo(t, post...); !strings.Contains(o, "o/catalog#1") {
		t.Errorf("output %q names the pull request as owner/repo#N", o)
	}

	if !slices.Equal(f.writes, []string{"create", "label +Schedule", "title Schedule: Night Shift"}) {
		t.Fatalf("writes %q", f.writes)
	}

	if o := postReviewTo(t, post...); !strings.Contains(o, "up to date") || len(f.writes) != 3 {
		t.Errorf("same files: %q, writes %q", o, f.writes)
	}
}
