package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
)

const noteURL = "https://gitlab.example/group/catalog/-/issues/7#note_5"

// fakeForge is a minimal GitHub or GitLab for issue #7: its text, its labels, the comments in creation order and a
// log of writes. GitHub and GitLab differ only in paths, field names and how labels change.
type fakeForge struct {
	body     string
	labels   []string
	comments []string
	writes   []string
	mu       sync.Mutex
}

func (f *fakeForge) log(s string) { f.writes = append(f.writes, s) }

func (f *fakeForge) comment(w http.ResponseWriter, r *http.Request, id int) {
	var in struct {
		Body string `json:"body"`
	}

	_ = json.NewDecoder(r.Body).Decode(&in)

	if r.Method == http.MethodPost {
		f.comments = append(f.comments, in.Body)
		f.log("create")
	} else {
		f.comments[0] = in.Body
		f.log("update")
	}

	_, _ = fmt.Fprintf(w, `{"id": %d, "html_url": "https://github.com/o/r/issues/7#issuecomment-%d"}`, id, id)
}

func (f *fakeForge) github(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer secret" {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	switch path := r.URL.EscapedPath(); {
	case path == "/repos/o/r/issues/7" && r.Method == http.MethodGet:
		labels := make([]map[string]string, 0, len(f.labels))
		for _, l := range f.labels {
			labels = append(labels, map[string]string{"name": l})
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"body": f.body, "labels": labels})
	case path == "/repos/o/r/issues/7/comments" && r.Method == http.MethodGet:
		list := make([]map[string]any, 0, len(f.comments))
		for _, c := range f.comments {
			list = append(list, map[string]any{
				"id": 1, "body": c, "user": map[string]string{"login": "github-actions[bot]"},
				"html_url": "https://github.com/o/r/issues/7#issuecomment-1",
			})
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == "/repos/o/r/issues/7/comments", path == "/repos/o/r/issues/comments/1":
		f.comment(w, r, 1)
	case path == "/repos/o/r/issues/7/labels" && r.Method == http.MethodPost:
		var in struct {
			Labels []string `json:"labels"`
		}

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.labels = append(f.labels, in.Labels...)
		f.log("label +" + strings.Join(in.Labels, ","))
		_, _ = w.Write([]byte("[]"))
	case strings.HasPrefix(path, "/repos/o/r/issues/7/labels/") && r.Method == http.MethodDelete:
		name, _ := url.PathUnescape(strings.TrimPrefix(path, "/repos/o/r/issues/7/labels/"))
		f.labels = slices.DeleteFunc(f.labels, func(l string) bool { return l == name })
		f.log("label -" + name)
		_, _ = w.Write([]byte("[]"))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

func (f *fakeForge) gitlab(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const issue = "/projects/group%2Fcatalog/issues/7"

	switch path := r.URL.EscapedPath(); {
	case r.Header.Get("PRIVATE-TOKEN") != "secret":
		w.WriteHeader(http.StatusUnauthorized)
	case path == "/user":
		_, _ = w.Write([]byte(`{"username": "project_1_bot"}`))
	case path == "/projects/group%2Fcatalog/uploads":
		name := ""
		if _, head, err := r.FormFile("file"); err == nil {
			name = head.Filename
		}

		f.log("upload " + name)
		_, _ = w.Write([]byte(`{"url": "/uploads/abc/schedule.svg"}`))
	case path == issue && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"description": f.body, "labels": f.labels, "web_url": "https://gitlab.example/group/catalog/-/issues/7",
		})
	case path == issue && r.Method == http.MethodPut:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)

		if l := in["add_labels"]; l != "" {
			f.labels = append(f.labels, l)
			f.log("label +" + l)
		}

		if l := in["remove_labels"]; l != "" {
			f.labels = slices.DeleteFunc(f.labels, func(x string) bool { return x == l })
			f.log("label -" + l)
		}

		_, _ = w.Write([]byte("{}"))
	case path == issue+"/notes" && r.Method == http.MethodGet:
		list := make([]map[string]any, 0, len(f.comments))
		for _, c := range f.comments {
			list = append(
				list,
				map[string]any{"id": 5, "body": c, "author": map[string]string{"username": "project_1_bot"}},
			)
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == issue+"/notes", path == issue+"/notes/5":
		f.comment(w, r, 5)
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

// renderTo renders a request into a new directory and returns it with the request's text.
func renderTo(t *testing.T, file string, want int) (string, string) {
	t.Helper()

	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"preview", "render", "--out-dir", out, file}, &stdout, &stderr); code != want {
		t.Fatalf("render %s: exit %d: %s", file, code, stderr.String())
	}

	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	return out, string(src)
}

// postTo runs preview post and returns its stdout.
func postTo(t *testing.T, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if code := run(append([]string{"preview", "post"}, args...), &stdout, &stderr); code != exitOK {
		t.Fatalf("post %q: exit %d: %s", args, code, stderr.String())
	}

	return stdout.String()
}

// TestPreviewPost walks one issue through its life on a fake GitHub: a dry run writes nothing; the first post creates
// the comment and adds the label; the same inputs again change nothing; a run that checked an older text stays out
// of the way; an invalid edit updates the comment and removes the label.
func TestPreviewPost(t *testing.T) {
	t.Setenv("TEST_COMPANION_TOKEN", "secret")

	f := &fakeForge{labels: []string{"triage"}}
	srv := httptest.NewServer(http.HandlerFunc(f.github))

	defer srv.Close()

	valid, text := renderTo(t, "../../testdata/office-hours.md", exitOK)
	f.body = strings.ReplaceAll(text, "\n", "\r\n") // Issue bodies come back with CRLF; the stamp reads them as LF.

	args := []string{
		"--forge",
		"github",
		"--repo",
		"o/r",
		"--issue",
		"7",
		"--api-url",
		srv.URL,
		"--token-env",
		"TEST_COMPANION_TOKEN",
		"--label",
		"Schedule",
		"--link",
		"Week as SVG=https://example.com/svg",
		"--note",
		"30 days",
	}

	out := postTo(t, append(args, "--dry-run", valid)...)
	if !strings.Contains(out, "would create a comment on o/r#7") ||
		!strings.Contains(out, "would add the label Schedule to o/r#7") || len(f.writes) != 0 {
		t.Errorf("dry run: %q, writes %q", out, f.writes)
	}

	out = postTo(t, append(args, valid)...)
	if !slices.Equal(f.writes, []string{"create", "label +Schedule"}) ||
		!strings.HasSuffix(f.comments[0], "[Week as SVG](https://example.com/svg) (30 days)\n") ||
		!strings.Contains(out, "created a comment on o/r#7 https://github.com/o/r/issues/7#issuecomment-1") {
		t.Fatalf("post: %q, writes %q, comments %q", out, f.writes, f.comments)
	}

	if out = postTo(t, append(args, valid)...); !strings.Contains(out, "is up to date") || len(f.writes) != 2 {
		t.Errorf("same inputs: %q, writes %q", out, f.writes)
	}

	invalid, text := renderTo(t, "../../testdata/invalid-name.md", exitInvalid)

	if out = postTo(t, append(args, invalid)...); !strings.Contains(out, "changed since the check") ||
		len(f.writes) != 2 {
		t.Errorf("older text: %q, writes %q", out, f.writes)
	}

	f.body = text

	out = postTo(t, append(args, invalid)...)
	if !slices.Equal(f.writes[2:], []string{"update", "label -Schedule"}) ||
		!strings.Contains(f.comments[0], "invalid-name") ||
		!slices.Equal(f.labels, []string{"triage"}) {
		t.Errorf("invalid: %q, writes %q, labels %q", out, f.writes, f.labels)
	}
}

// TestPreviewPostGitLab posts to a fake GitLab: the week is uploaded and shown in the note, the note's author comes
// from the token, a dry run uploads nothing, the same inputs upload nothing again, and an invalid request has no week.
func TestPreviewPostGitLab(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	f := &fakeForge{}
	srv := httptest.NewServer(http.HandlerFunc(f.gitlab))

	defer srv.Close()

	valid, text := renderTo(t, "../../testdata/office-hours.md", exitOK)
	f.body = text
	args := []string{
		"--forge",
		"gitlab",
		"--repo",
		"group/catalog",
		"--issue",
		"7",
		"--api-url",
		srv.URL,
		"--label",
		"Schedule",
	}

	if out := postTo(t, append(args, "--dry-run", valid)...); !strings.HasPrefix(out, "would upload schedule-7-") ||
		len(f.writes) != 0 {
		t.Errorf("dry run: %q, writes %q", out, f.writes)
	}

	out := postTo(t, append(args, valid)...)
	if len(f.writes) != 3 || !strings.HasPrefix(f.writes[0], "upload schedule-7-") ||
		!strings.HasSuffix(f.writes[0], ".svg") ||
		f.writes[1] != "create" ||
		f.writes[2] != "label +Schedule" ||
		!strings.HasSuffix(f.comments[0], "\n![Week as SVG](/uploads/abc/schedule.svg)\n") ||
		!strings.Contains(out, "created a comment on group/catalog#7 "+noteURL) {
		t.Fatalf("post: %q, writes %q", out, f.writes)
	}

	if out = postTo(t, append(args, valid)...); !strings.Contains(out, "is up to date") || len(f.writes) != 3 {
		t.Errorf("same inputs: %q, writes %q", out, f.writes)
	}

	invalid, text := renderTo(t, "../../testdata/invalid-name.md", exitInvalid)
	f.body = text

	postTo(t, append(args, invalid)...)

	if !slices.Equal(f.writes[3:], []string{"update", "label -Schedule"}) || strings.Contains(f.comments[0], "![") {
		t.Errorf("invalid: writes %q, comments %q", f.writes, f.comments)
	}
}

func TestPreviewPostUsage(t *testing.T) {
	t.Setenv("TEST_COMPANION_EMPTY", "")

	dir := t.TempDir()
	base := []string{"preview", "post", "--repo", "o/r", "--issue", "7"}

	for name, args := range map[string][]string{
		"no forge":          append(base, dir),
		"unknown forge":     append(base, "--forge", "forgejo", dir),
		"gitlab no comment": append(base, "--forge", "gitlab", dir),
		"no repo":           {"preview", "post", "--forge", "github", "--issue", "7", dir},
		"no dir":            append(base, "--forge", "github"),
		"bad link":          append(base, "--forge", "github", "--link", "x=http://insecure", dir),
		"no comment.md":     append(base, "--forge", "github", dir),
		"empty token":       append(base, "--forge", "github", "--token-env", "TEST_COMPANION_EMPTY", dir),
		"empty author":      append(base, "--forge", "github", "--author", " ", dir),
		"negative issue":    {"preview", "post", "--forge", "github", "--repo", "o/r", "--issue", "-1", dir},
		"unknown argument":  append(base, "--forge", "github", "--colour", dir),
		"label with spaces": append(base, "--forge", "github", "--label", " Schedule", dir),
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}
