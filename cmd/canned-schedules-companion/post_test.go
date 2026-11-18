package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPreviewPost renders a request, then posts it to a fake API that has no comments yet: first as a dry run, then
// for real. The token comes from the variable --token-env names.
func TestPreviewPost(t *testing.T) {
	t.Setenv("TEST_COMPANION_TOKEN", "secret")

	var posted []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)

			var in struct {
				Body string `json:"body"`
			}

			_ = json.Unmarshal(raw, &in)
			posted = append(posted, in.Body)
			_, _ = w.Write([]byte(`{"id": 1, "html_url": "https://github.com/o/r/issues/7#issuecomment-1"}`))

			return
		}

		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	out := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"preview", "render", "--out-dir", out, "../../testdata/office-hours.md"}, &stdout,
		&stderr); code != exitOK {
		t.Fatalf("render: exit %d: %s", code, stderr.String())
	}

	post := []string{
		"preview", "post", "--forge", "github", "--repo", "o/r", "--issue", "7", "--api-url", srv.URL,
		"--token-env", "TEST_COMPANION_TOKEN", "--link", "Week as SVG=https://example.com/svg", "--note", "30 days",
	}

	stdout.Reset()

	if code := run(append(post, "--dry-run", out), &stdout, &stderr); code != exitOK {
		t.Fatalf("dry run: exit %d: %s", code, stderr.String())
	}

	if !strings.HasPrefix(stdout.String(), "would create a comment on o/r#7") || len(posted) != 0 {
		t.Errorf("dry run: %q, posted %d", stdout.String(), len(posted))
	}

	stdout.Reset()

	if code := run(append(post, out), &stdout, &stderr); code != exitOK {
		t.Fatalf("post: exit %d: %s", code, stderr.String())
	}

	if len(posted) != 1 || !strings.HasSuffix(posted[0], "[Week as SVG](https://example.com/svg) (30 days)\n") {
		t.Fatalf("posted %q", posted)
	}

	if !strings.Contains(stdout.String(), "created a comment on o/r#7 https://github.com/o/r/issues/7#issuecomment-1") {
		t.Errorf("stdout %q", stdout.String())
	}
}

// TestPreviewPostGitLab posts a valid request to a fake GitLab: the week is uploaded and shown in the note, and the
// note's author comes from the token. A dry run uploads nothing, and an invalid request has no week to upload.
func TestPreviewPostGitLab(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	var (
		uploads  int
		uploaded string
	)

	var notes []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()

		switch {
		case r.Header.Get("PRIVATE-TOKEN") != "secret":
			w.WriteHeader(http.StatusUnauthorized)
		case path == "/user":
			_, _ = w.Write([]byte(`{"username": "project_1_bot"}`))
		case path == "/projects/group%2Fcatalog/uploads":
			uploads++

			if _, head, err := r.FormFile("file"); err == nil {
				uploaded = head.Filename
			}

			_, _ = w.Write([]byte(`{"url": "/uploads/abc/schedule.svg"}`))
		case path == "/projects/group%2Fcatalog/issues/7":
			_, _ = w.Write([]byte(`{"web_url": "https://gitlab.example/group/catalog/-/issues/7"}`))
		case r.Method == http.MethodPost:
			var in struct {
				Body string `json:"body"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)
			notes = append(notes, in.Body)
			_, _ = w.Write([]byte(`{"id": 5}`))
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	render := func(file string, want int) string {
		t.Helper()

		out := t.TempDir()

		var stdout, stderr bytes.Buffer
		if code := run([]string{"preview", "render", "--out-dir", out, file}, &stdout, &stderr); code != want {
			t.Fatalf("render %s: exit %d: %s", file, code, stderr.String())
		}

		return out
	}

	post := []string{
		"preview",
		"post",
		"--forge",
		"gitlab",
		"--repo",
		"group/catalog",
		"--issue",
		"7",
		"--api-url",
		srv.URL,
	}
	valid := render("../../testdata/office-hours.md", exitOK)

	var stdout, stderr bytes.Buffer
	if code := run(append(post, "--dry-run", valid), &stdout, &stderr); code != exitOK {
		t.Fatalf("dry run: exit %d: %s", code, stderr.String())
	}

	if !strings.HasPrefix(stdout.String(), "would upload schedule-7-") || uploads != 0 ||
		len(notes) != 0 {
		t.Errorf("dry run: %q, %d uploads, %d notes", stdout.String(), uploads, len(notes))
	}

	stdout.Reset()

	if code := run(append(post, valid), &stdout, &stderr); code != exitOK {
		t.Fatalf("post: exit %d: %s", code, stderr.String())
	}

	if !strings.HasPrefix(uploaded, "schedule-7-") || !strings.HasSuffix(uploaded, ".svg") {
		t.Errorf("uploaded as %q", uploaded)
	}

	if uploads != 1 || len(notes) != 1 ||
		!strings.HasSuffix(notes[0], "\n![Week as SVG](/uploads/abc/schedule.svg)\n") {
		t.Fatalf("%d uploads, notes %q", uploads, notes)
	}

	if !strings.Contains(
		stdout.String(),
		"created a comment on group/catalog#7 https://gitlab.example/group/catalog/-/issues/7#note_5",
	) {
		t.Errorf("stdout %q", stdout.String())
	}

	invalid := render("../../testdata/invalid-name.md", exitInvalid)

	if code := run(append(post, invalid), &stdout, &stderr); code != exitOK {
		t.Fatalf("post invalid: exit %d: %s", code, stderr.String())
	}

	if uploads != 1 || len(notes) != 2 || strings.Contains(notes[1], "![") {
		t.Errorf("invalid: %d uploads, notes %q", uploads, notes)
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
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}
