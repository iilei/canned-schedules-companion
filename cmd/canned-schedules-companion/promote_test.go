package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const promoteNight = "# Night Shift\r\n\r\nThe desk needs cover.\r\n\r\n> [!SCHEDULE-STATE] Medium\r\n" +
	"> - `22:00` to `06:00` MON-FRI\r\n"

// fakeRepo is a repository with branches of files, open requests, an issue's labels and a write log, served as
// GitHub's or GitLab's API.
type fakeRepo struct {
	branches map[string]map[string]string
	requests map[string]string // source branch → URL
	labels   []string
	writes   []string
	mu       sync.Mutex
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		branches: map[string]map[string]string{"main": {}},
		requests: map[string]string{},
		labels:   []string{"Schedule", "Schedule approved"},
	}
}

func (f *fakeRepo) file(branch, path string) (string, bool) {
	b, ok := f.branches[branch]
	if !ok {
		return "", false
	}

	c, ok := b[path]

	return c, ok
}

func (f *fakeRepo) write(branch, from, path, content string) {
	if from != "" {
		f.branches[branch] = maps.Clone(f.branches[from])

		f.writes = append(f.writes, "branch "+branch)
	}

	f.branches[branch][path] = content
	f.writes = append(f.writes, "commit "+path)
}

// gitHub serves the GitHub calls promote open makes for o/catalog, issue 12.
func (f *fakeRepo) gitHub(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const repo = "/repos/o/catalog"

	path, _ := url.PathUnescape(r.URL.EscapedPath())

	switch {
	case r.Header.Get("Authorization") != "Bearer secret":
		w.WriteHeader(http.StatusUnauthorized)
	case strings.HasPrefix(path, repo+"/branches/"):
		if _, ok := f.branches[strings.TrimPrefix(path, repo+"/branches/")]; !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write([]byte("{}"))
	case strings.HasPrefix(path, repo+"/git/ref/heads/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "base-sha"}})
	case path == repo+"/git/refs" && r.Method == http.MethodPost:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.branches[strings.TrimPrefix(in["ref"], "refs/heads/")] = map[string]string{}
		f.writes = append(f.writes, "branch "+strings.TrimPrefix(in["ref"], "refs/heads/"))
		_, _ = w.Write([]byte("{}"))
	case strings.HasPrefix(path, repo+"/contents/") && r.Method == http.MethodGet:
		c, ok := f.file(r.URL.Query().Get("ref"), strings.TrimPrefix(path, repo+"/contents/"))
		if !ok {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"content": base64.StdEncoding.EncodeToString([]byte(c)), "encoding": "base64", "sha": "blob-sha",
		})
	case strings.HasPrefix(path, repo+"/contents/") && r.Method == http.MethodPut:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		raw, _ := base64.StdEncoding.DecodeString(in["content"])
		p := strings.TrimPrefix(path, repo+"/contents/")

		if _, exists := f.file(in["branch"], p); exists != (in["sha"] == "blob-sha") {
			http.Error(w, "sha mismatch", http.StatusConflict)

			return
		}

		f.branches[in["branch"]][p] = string(raw)
		f.writes = append(f.writes, "commit "+p)
		_, _ = w.Write([]byte("{}"))
	case path == repo+"/pulls" && r.Method == http.MethodGet:
		branch := strings.TrimPrefix(r.URL.Query().Get("head"), "o:")

		list := []map[string]string{}
		if u, ok := f.requests[branch]; ok {
			list = append(list, map[string]string{"html_url": u})
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == repo+"/pulls" && r.Method == http.MethodPost:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.requests[in["head"]] = "https://github.example/o/catalog/pull/13"
		f.writes = append(f.writes, "request "+in["title"]+" | "+strings.SplitN(in["body"], "\n", 2)[0])
		_ = json.NewEncoder(w).Encode(map[string]string{"html_url": f.requests[in["head"]]})
	case strings.HasPrefix(path, repo+"/issues/12/labels/") && r.Method == http.MethodDelete:
		l := strings.TrimPrefix(path, repo+"/issues/12/labels/")
		f.labels = slices.DeleteFunc(f.labels, func(x string) bool { return x == l })
		f.writes = append(f.writes, "label -"+l)
		_, _ = w.Write([]byte("[]"))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

// gitLab serves the GitLab calls promote open makes for group/catalog, issue 12.
func (f *fakeRepo) gitLab(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const project = "/projects/group/catalog"

	path, _ := url.PathUnescape(r.URL.EscapedPath())

	switch {
	case r.Header.Get("PRIVATE-TOKEN") != "secret":
		w.WriteHeader(http.StatusUnauthorized)
	case strings.HasPrefix(path, project+"/repository/branches/"):
		if _, ok := f.branches[strings.TrimPrefix(path, project+"/repository/branches/")]; !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write([]byte("{}"))
	case strings.HasPrefix(path, project+"/repository/files/"):
		c, ok := f.file(r.URL.Query().Get("ref"), strings.TrimPrefix(path, project+"/repository/files/"))
		if !ok {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]string{
			"content": base64.StdEncoding.EncodeToString([]byte(c)), "encoding": "base64",
		})
	case path == project+"/repository/commits":
		var in struct {
			Branch  string `json:"branch"`
			Start   string `json:"start_branch"`
			Actions []struct {
				Action  string `json:"action"`
				Path    string `json:"file_path"`
				Content string `json:"content"`
			} `json:"actions"`
		}

		_ = json.NewDecoder(r.Body).Decode(&in)
		a := in.Actions[0]
		raw, _ := base64.StdEncoding.DecodeString(a.Content)

		base := in.Branch
		if in.Start != "" {
			base = in.Start
		}

		if _, exists := f.file(base, a.Path); exists != (a.Action == "update") {
			http.Error(w, "wrong action "+a.Action, http.StatusBadRequest)

			return
		}

		f.write(in.Branch, in.Start, a.Path, string(raw))
		_, _ = w.Write([]byte("{}"))
	case path == project+"/merge_requests" && r.Method == http.MethodGet:
		list := []map[string]string{}
		if u, ok := f.requests[r.URL.Query().Get("source_branch")]; ok {
			list = append(list, map[string]string{"web_url": u})
		}

		_ = json.NewEncoder(w).Encode(list)
	case path == project+"/merge_requests" && r.Method == http.MethodPost:
		var in map[string]any

		_ = json.NewDecoder(r.Body).Decode(&in)
		branch, _ := in["source_branch"].(string)
		title, _ := in["title"].(string)
		body, _ := in["description"].(string)
		f.requests[branch] = "https://gitlab.example/group/catalog/-/merge_requests/3"
		f.writes = append(f.writes, "request "+title+" | "+strings.SplitN(body, "\n", 2)[0])
		_ = json.NewEncoder(w).Encode(map[string]string{"web_url": f.requests[branch]})
	case path == project+"/issues/12" && r.Method == http.MethodPut:
		var in map[string]string

		_ = json.NewDecoder(r.Body).Decode(&in)
		f.labels = slices.DeleteFunc(f.labels, func(x string) bool { return x == in["remove_labels"] })
		f.writes = append(f.writes, "label -"+in["remove_labels"])
		_, _ = w.Write([]byte("{}"))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusNotFound)
	}
}

func preparePromotion(t *testing.T, dir, body string) int {
	t.Helper()

	in := filepath.Join(dir, "issue.md")
	if err := os.WriteFile(in, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer

	return run([]string{"promote", "prepare", "--out-dir", filepath.Join(dir, "out"), in}, &stdout, &stderr)
}

func runOpen(t *testing.T, args []string) (string, int) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)

	return stdout.String() + stderr.String(), code
}

func TestPromote(t *testing.T) {
	for _, forgeName := range []string{"github", "gitlab"} {
		t.Run(forgeName, func(t *testing.T) {
			f := newFakeRepo()
			handler, repo, token := f.gitHub, "o/catalog", "GITHUB_TOKEN"

			if forgeName == "gitlab" {
				handler, repo, token = f.gitLab, "group/catalog", "GITLAB_TOKEN"
			}

			t.Setenv(token, "secret")

			srv := httptest.NewServer(http.HandlerFunc(handler))
			defer srv.Close()

			dir := t.TempDir()
			if code := preparePromotion(t, dir, promoteNight); code != exitOK {
				t.Fatalf("prepare: exit %d", code)
			}

			open := []string{
				"promote", "open", "--forge", forgeName, "--repo", repo, "--issue", "12", "--base-branch", "main",
				"--api-url", srv.URL, "--approved-label", "Schedule approved", filepath.Join(dir, "out"),
			}

			if out, code := runOpen(t, append([]string{open[0], open[1], "--dry-run"}, open[2:]...)); code != exitOK ||
				len(f.writes) != 0 || !strings.Contains(out, "would add") || !strings.Contains(out, "would open") {
				t.Fatalf("dry run: exit %d, %q, writes %q", code, out, f.writes)
			}

			if out, code := runOpen(t, open); code != exitOK {
				t.Fatalf("open: exit %d: %s", code, out)
			}

			const path = "schedules/night-shift.canned-schedule.md"

			want := []string{
				"branch canned-schedules/issue-12", "commit " + path,
				"request Schedule: Night Shift | Closes #12.",
			}
			if !slices.Equal(f.writes, want) {
				t.Fatalf("writes %q, want %q", f.writes, want)
			}

			if c, _ := f.file("canned-schedules/issue-12", path); !strings.HasPrefix(c, "# Night Shift\n\nThe desk") {
				t.Errorf("committed %q: want the issue's text with LF", c)
			}

			// Again: nothing changes, the request is named.
			if out, code := runOpen(
				t,
				open,
			); code != exitOK || len(f.writes) != 3 ||
				!strings.Contains(out, "is open") {
				t.Errorf("again: exit %d, %q, writes %q", code, out, f.writes)
			}

			// An edited issue updates the branch, the request stays.
			if code := preparePromotion(t, dir, promoteNight+"\r\nOne more line.\r\n"); code != exitOK {
				t.Fatalf("prepare edited: exit %d", code)
			}

			if out, code := runOpen(
				t,
				open,
			); code != exitOK ||
				!slices.Equal(f.writes[3:], []string{"commit " + path}) {
				t.Errorf("edited: exit %d, %q, writes %q", code, out, f.writes)
			}

			// A broken edit takes the approval back and fails.
			if code := preparePromotion(t, dir, "# Night Shift!\n\n> [!SCHEDULE-STATE] Medium\n"); code != exitInvalid {
				t.Fatalf("prepare broken: exit %d", code)
			}

			if out, code := runOpen(t, open); code != exitInvalid ||
				!slices.Equal(f.writes[4:], []string{"label -Schedule approved"}) {
				t.Errorf("broken: exit %d, %q, writes %q", code, out, f.writes)
			}
		})
	}
}

// TestPromoteAlreadyInCatalog: the same text on the base branch needs no request.
func TestPromoteAlreadyInCatalog(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")

	f := newFakeRepo()
	f.branches["main"]["schedules/night-shift.canned-schedule.md"] = strings.ReplaceAll(promoteNight, "\r\n", "\n")

	srv := httptest.NewServer(http.HandlerFunc(f.gitHub))
	defer srv.Close()

	dir := t.TempDir()
	if code := preparePromotion(t, dir, promoteNight); code != exitOK {
		t.Fatalf("prepare: exit %d", code)
	}

	out, code := runOpen(t, []string{
		"promote", "open", "--forge", "github", "--repo", "o/catalog", "--issue", "12", "--base-branch", "main",
		"--api-url", srv.URL, filepath.Join(dir, "out"),
	})
	if code != exitOK || len(f.writes) != 0 || !strings.Contains(out, "nothing to promote") {
		t.Errorf("exit %d, %q, writes %q", code, out, f.writes)
	}
}

func TestPromoteUsage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for name, args := range map[string][]string{
		"no forge":  {"promote", "open", "--repo", "o/r", "--issue", "1", "--base-branch", "main", dir},
		"no base":   {"promote", "open", "--forge", "github", "--repo", "o/r", "--issue", "1", dir},
		"bad pref":  {"promote", "open", "--forge", "github", "--repo", "o/r", "--issue", "1", "--base-branch", "m", "--branch-prefix", "a..b", dir},
		"bad dir":   {"promote", "prepare", "--catalog-dir", "../x", filepath.Join(dir, "missing.md")},
		"two files": {"promote", "prepare", "a.md", "b.md"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}
