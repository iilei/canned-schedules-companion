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

func TestPreviewPostUsage(t *testing.T) {
	t.Setenv("TEST_COMPANION_EMPTY", "")

	dir := t.TempDir()
	base := []string{"preview", "post", "--repo", "o/r", "--issue", "7"}

	for name, args := range map[string][]string{
		"no forge":         append(base, dir),
		"gitlab":           append(base, "--forge", "gitlab", dir),
		"no repo":          {"preview", "post", "--forge", "github", "--issue", "7", dir},
		"no dir":           append(base, "--forge", "github"),
		"bad link":         append(base, "--forge", "github", "--link", "x=http://insecure", dir),
		"no comment.md":    append(base, "--forge", "github", dir),
		"empty token":      append(base, "--forge", "github", "--token-env", "TEST_COMPANION_EMPTY", dir),
		"empty author":     append(base, "--forge", "github", "--author", " ", dir),
		"negative issue":   {"preview", "post", "--forge", "github", "--repo", "o/r", "--issue", "-1", dir},
		"unknown argument": append(base, "--forge", "github", "--colour", dir),
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}
