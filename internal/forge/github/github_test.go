package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/forge/github"
)

const (
	marker = "<!-- canned-schedules:issue-preview -->"
	bot    = "github-actions[bot]"
)

type fakeComment struct {
	login, body string
	id          int64
}

// fake is a minimal GitHub: one issue (#7 in o/r), its comments in creation order, and a log of writes.
type fake struct {
	comments []fakeComment
	writes   []string
	mu       sync.Mutex
	next     int64
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-GitHub-Api-Version") == "" {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)

		return
	}

	const list = "/repos/o/r/issues/7/comments"

	switch {
	case r.Method == http.MethodGet && r.URL.Path == list:
		f.list(w, r)
	case r.Method == http.MethodPost && r.URL.Path == list:
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.add(bot, in.Body)
		f.writes = append(f.writes, "create")
		writeComment(w, f.comments[len(f.comments)-1])
	case strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), 10, 64)
		f.change(w, r, id)
	default:
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}
}

func (f *fake) add(login, body string) {
	f.next++
	f.comments = append(f.comments, fakeComment{id: f.next, login: login, body: body})
}

func (f *fake) list(w http.ResponseWriter, r *http.Request) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	from := min((page-1)*per, len(f.comments))
	to := min(from+per, len(f.comments))

	out := []map[string]any{}
	for _, c := range f.comments[from:to] {
		out = append(out, toJSON(c))
	}

	_ = json.NewEncoder(w).Encode(out)
}

func (f *fake) change(w http.ResponseWriter, r *http.Request, id int64) {
	i := slices.IndexFunc(f.comments, func(c fakeComment) bool { return c.id == id })
	if i < 0 {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

		return
	}

	switch r.Method {
	case http.MethodPatch:
		var in struct {
			Body string `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.comments[i].body = in.Body
		f.writes = append(f.writes, fmt.Sprintf("update %d", id))
		writeComment(w, f.comments[i])
	case http.MethodDelete:
		f.comments = slices.Delete(f.comments, i, i+1)
		f.writes = append(f.writes, fmt.Sprintf("delete %d", id))
		w.WriteHeader(http.StatusNoContent)
	}
}

func toJSON(c fakeComment) map[string]any {
	return map[string]any{
		"id": c.id, "body": c.body, "user": map[string]string{"login": c.login},
		"html_url": fmt.Sprintf("https://github.com/o/r/issues/7#issuecomment-%d", c.id),
	}
}

func writeComment(w http.ResponseWriter, c fakeComment) {
	_ = json.NewEncoder(w).Encode(toJSON(c))
}

func upsert(t *testing.T, f *fake, dry bool) (*github.Plan, error) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	c := &github.Client{HTTP: srv.Client(), BaseURL: srv.URL, Token: "secret", UserAgent: "test"}

	return c.UpsertComment(context.Background(), &github.Upsert{
		Repo: "o/r", Issue: 7, Marker: marker, Author: bot, Body: marker + "\nnew", DryRun: dry,
	})
}

func TestCreatesWhenThereIsNone(t *testing.T) {
	t.Parallel()

	f := &fake{}
	f.add("alice", "please check")
	f.add(bot, "an unrelated bot comment")

	p, err := upsert(t, f, false)
	if err != nil {
		t.Fatal(err)
	}

	if p.Action != github.ActionCreate || !slices.Equal(f.writes, []string{"create"}) || p.URL == "" {
		t.Errorf("plan %+v, writes %v", p, f.writes)
	}
}

func TestUpdatesTheOldestAndDeletesDuplicates(t *testing.T) {
	t.Parallel()

	f := &fake{}
	f.add("alice", marker+"\npasted by a person: never touched") // 1
	f.add(bot, marker+"\nfirst")                                 // 2
	f.add("bob", "a reply")                                      // 3
	f.add(bot, marker+"\nduplicate")                             // 4

	p, err := upsert(t, f, false)
	if err != nil {
		t.Fatal(err)
	}

	if p.Action != github.ActionUpdate || p.Update != 2 || !slices.Equal(f.writes, []string{"update 2", "delete 4"}) {
		t.Errorf("plan %+v, writes %v", p, f.writes)
	}

	if f.comments[0].body != marker+"\npasted by a person: never touched" || f.comments[1].body != marker+"\nnew" {
		t.Errorf("comments now %+v", f.comments)
	}
}

func TestFindsTheCommentOnALaterPage(t *testing.T) {
	t.Parallel()

	f := &fake{}
	for i := range 150 {
		f.add("alice", fmt.Sprint("comment ", i))
	}

	f.add(bot, marker+"\nold") // id 151, on page 2

	p, err := upsert(t, f, false)
	if err != nil {
		t.Fatal(err)
	}

	if p.Action != github.ActionUpdate || p.Update != 151 {
		t.Errorf("plan %+v", p)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	f := &fake{}
	f.add(bot, marker+"\nfirst")
	f.add(bot, marker+"\nduplicate")

	p, err := upsert(t, f, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(f.writes) != 0 || p.Action != github.ActionUpdate || !slices.Equal(p.Delete, []int64{2}) {
		t.Errorf("plan %+v, writes %v", p, f.writes)
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(&fake{})
	t.Cleanup(srv.Close)

	for name, u := range map[string]github.Upsert{
		"bad token":   {Repo: "o/r", Issue: 7},
		"bad repo":    {Repo: "o", Issue: 7},
		"nested repo": {Repo: "o/r/x", Issue: 7},
		"no issue":    {Repo: "o/r", Issue: 8},
	} {
		token := "secret"
		if name == "bad token" {
			token = "wrong"
		}

		c := &github.Client{HTTP: srv.Client(), BaseURL: srv.URL, Token: token, UserAgent: "test"}
		if _, err := c.UpsertComment(context.Background(), &u); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
