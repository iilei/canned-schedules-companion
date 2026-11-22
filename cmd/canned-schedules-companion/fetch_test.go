package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/publish"
	"github.com/iilei/canned-schedules-companion/internal/review"
)

const (
	fetchSHA   = "c0ffee"
	fetchNight = "# Night Shift\n\n> [!SCHEDULE-STATE] Medium\n> - `22:00` to `06:00` MON-FRI\n"
	fetchPath  = "schedules/night-shift.canned-schedule.md"
)

// staticForge answers fixed paths with fixed JSON; anything else is a 404, and every request must carry the token.
type staticForge struct {
	answers map[string]string
	header  string
}

func (f *staticForge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.Header.Get(f.header), "secret") || r.Method != http.MethodGet {
		http.Error(w, `{"message":"no"}`, http.StatusUnauthorized)

		return
	}

	// Lists have one page: fewer entries than a page holds.
	a, ok := f.answers[r.URL.EscapedPath()]
	if !ok {
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)

		return
	}

	_, _ = w.Write([]byte(a))
}

func reviewComment(t *testing.T) string {
	t.Helper()

	res, err := review.Render([]review.File{{Path: fetchPath, Src: []byte(fetchNight)}},
		&cs.Options{Preview: &cs.PreviewOptions{}}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}

	return string(res.Comment)
}

func jsonString(t *testing.T, s string) string {
	t.Helper()

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// runFetch runs publish fetch and reads back what it wrote.
func runFetch(t *testing.T, args ...string) (*publish.Input, string, int) {
	t.Helper()

	out := filepath.Join(t.TempDir(), "merge-request.json")

	var stdout, stderr bytes.Buffer

	code := run(append([]string{"publish", "fetch", "--out-file", out}, args...), &stdout, &stderr)
	if code != exitOK {
		return nil, stdout.String() + stderr.String(), code
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	in, err := publish.ParseInput(raw)
	if err != nil {
		t.Fatalf("%v in\n%s", err, raw)
	}

	return in, stdout.String(), code
}

func gitlabForge(t *testing.T, notes string) *staticForge {
	t.Helper()

	project := "/api/v4/projects/ops%2Fcatalog"

	return &staticForge{header: "PRIVATE-TOKEN", answers: map[string]string{
		"/api/v4/user": `{"username":"project_1_bot"}`,
		project + "/repository/commits/" + fetchSHA + "/merge_requests": `[
		  {"iid": 3, "state": "opened", "sha": "` + fetchSHA + `"},
		  {"iid": 42, "state": "merged", "merged_at": "2026-11-22T15:30:00+01:00", "created_at": "2026-11-20T09:15:00Z",
		   "merge_user": {"username": "jürgen"}, "merged_by": {"username": "old"}, "author": {"username": "ana"},
		   "references": {"full": "ops/catalog!42"}, "web_url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42",
		   "title": "Schedule: Night Shift", "target_branch": "main", "sha": "head1", "squash_commit_sha": "` + fetchSHA + `",
		   "merge_commit_sha": null}
		]`,
		project + "/repository/commits/direct/merge_requests": `[]`,
		project + "/merge_requests/42":                        `{"web_url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42"}`,
		project + "/merge_requests/42/notes":                  notes,
	}}
}

func TestPublishFetchGitLab(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	notes := `[
	  {"id": 6, "author": {"username": "ana"}, "body": ` + jsonString(t, reviewComment(t)) + `},
	  {"id": 7, "author": {"username": "project_1_bot"}, "body": ` + jsonString(t, reviewComment(t)) + `}
	]`

	srv := httptest.NewServer(gitlabForge(t, notes))
	defer srv.Close()

	args := []string{
		"--forge",
		"gitlab",
		"--repo",
		"ops/catalog",
		"--commit",
		fetchSHA,
		"--api-url",
		srv.URL + "/api/v4",
	}

	in, out, code := runFetch(t, args...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}

	mr := in.MergeRequest
	if mr.Number != 42 || mr.MergedBy != "jürgen" || mr.Project != "ops/catalog" || mr.Host != "gitlab.example.com" ||
		mr.Forge != "gitlab" || mr.MergeCommit != fetchSHA || mr.MergedAt.Format("15:04") != "14:30" {
		t.Errorf("merge request %+v", mr)
	}

	// Only the bot's comment counts; a pasted copy by someone else never does.
	if in.Review == nil || in.Review.Digests[fetchPath] != publish.Digest([]byte(fetchNight)) ||
		!strings.HasSuffix(in.Review.URL, "#note_7") {
		t.Errorf("review %+v", in.Review)
	}

	if !strings.Contains(out, "vouches for 1 file(s)") {
		t.Errorf("stdout %q", out)
	}
}

func TestPublishFetchNoReview(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	old := "<!-- canned-schedules:merge-request-review sha256=a tool=t args=b -->\n### Schedule review\n"

	for name, notes := range map[string]string{
		"none":  `[]`,
		"older": `[{"id": 7, "author": {"username": "project_1_bot"}, "body": ` + jsonString(t, old) + `}]`,
	} {
		srv := httptest.NewServer(gitlabForge(t, notes))

		in, out, code := runFetch(t, "--forge", "gitlab", "--repo", "ops/catalog", "--commit", fetchSHA,
			"--api-url", srv.URL+"/api/v4")

		srv.Close()

		if code != exitOK || in.Review != nil || !strings.Contains(out, "published as not reviewed") {
			t.Errorf("%s: exit %d, review %+v, %q", name, code, in, out)
		}
	}

	// A commit no merged merge request produced: exit 1, nothing written.
	srv := httptest.NewServer(gitlabForge(t, `[]`))
	defer srv.Close()

	if _, out, code := runFetch(t, "--forge", "gitlab", "--repo", "ops/catalog", "--commit", "direct",
		"--api-url", srv.URL+"/api/v4"); code != exitInvalid || !strings.Contains(out, "no merged merge request") {
		t.Errorf("unknown commit: exit %d, %q", code, out)
	}
}

func TestPublishFetchGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")

	pr := `{"number": 5, "merged_at": "2026-11-22T14:30:00Z", "created_at": "2026-11-20T09:15:00Z",
	  "merged_by": {"login": "jo"}, "user": {"login": "ana"}, "html_url": "https://github.com/o/catalog/pull/5",
	  "title": "Schedule: Night Shift", "merge_commit_sha": "` + fetchSHA + `",
	  "base": {"ref": "main", "repo": {"full_name": "o/catalog"}}}`
	srv := httptest.NewServer(&staticForge{header: "Authorization", answers: map[string]string{
		"/repos/o/catalog/commits/" + fetchSHA + "/pulls": `[{"number": 4, "merged_at": null}, ` + pr + `]`,
		"/repos/o/catalog/pulls/5":                        pr,
		"/repos/o/catalog/issues/5/comments": `[{"id": 9, "user": {"login": "github-actions[bot]"},
		  "html_url": "https://github.com/o/catalog/pull/5#issuecomment-9", "body": ` + jsonString(t, reviewComment(t)) + `}]`,
	}})

	defer srv.Close()

	in, out, code := runFetch(t, "--forge", "github", "--repo", "o/catalog", "--commit", fetchSHA, "--api-url", srv.URL)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}

	if in.MergeRequest.Number != 5 || in.MergeRequest.MergedBy != "jo" || in.MergeRequest.Host != "github.com" ||
		in.Review == nil || len(in.Review.Digests) != 1 {
		t.Errorf("input %+v, review %+v", in.MergeRequest, in.Review)
	}
}

func TestPublishFetchUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--repo", "o/r", "--commit", "a"},
		{"--forge", "gitlab", "--commit", "a"},
		{"--forge", "gitlab", "--repo", "o/r", "--commit", "../x"},
		{"--forge", "gitlab", "--repo", "o/r", "--commit", "a", "extra"},
	} {
		if _, _, code := runFetch(t, args...); code != exitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}
