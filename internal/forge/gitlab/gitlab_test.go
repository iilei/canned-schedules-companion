package gitlab_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/forge"
	"github.com/iilei/canned-schedules-companion/internal/forge/gitlab"
)

const (
	marker  = "<!-- canned-schedules:issue-preview -->"
	bot     = "project_1_bot"
	project = "/api/v4/projects/group%2Fsub%2Fcatalog"
	issue   = project + "/issues/7"
	webURL  = "https://gitlab.example/group/sub/catalog/-/issues/7"
)

type fakeNote struct {
	author, body string
	id           int64
	system       bool
}

// fake is a minimal GitLab: one issue (#7 in group/sub/catalog), its notes in creation order, and a log of writes.
type fake struct {
	notes  []fakeNote
	writes []string
	mu     sync.Mutex
	next   int64
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("PRIVATE-TOKEN") != "secret" {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)

		return
	}

	path := r.URL.EscapedPath()

	switch {
	case path == "/api/v4/user":
		_, _ = io.WriteString(w, `{"username":"`+bot+`"}`)
	case path == issue:
		_, _ = io.WriteString(w, `{"web_url":"`+webURL+`"}`)
	case path == issue+"/notes" && r.Method == http.MethodGet:
		f.list(w, r)
	case path == issue+"/notes" && r.Method == http.MethodPost:
		f.add(fakeNote{author: bot, body: decodeBody(r)})
		f.writes = append(f.writes, "create")
		_, _ = fmt.Fprintf(w, `{"id":%d}`, f.next)
	case strings.HasPrefix(path, issue+"/notes/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, issue+"/notes/"), 10, 64)
		f.change(w, r, id)
	case path == project+"/uploads" && r.Method == http.MethodPost:
		f.upload(w, r)
	default:
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
	}
}

func (f *fake) add(n fakeNote) {
	f.next++
	n.id = f.next
	f.notes = append(f.notes, n)
}

func (f *fake) list(w http.ResponseWriter, r *http.Request) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	from := min((page-1)*per, len(f.notes))
	to := min(from+per, len(f.notes))

	out := []map[string]any{}
	for _, n := range f.notes[from:to] {
		out = append(out, map[string]any{
			"id": n.id, "body": n.body, "system": n.system, "author": map[string]string{"username": n.author},
		})
	}

	_ = json.NewEncoder(w).Encode(out)
}

func (f *fake) change(w http.ResponseWriter, r *http.Request, id int64) {
	i := slices.IndexFunc(f.notes, func(n fakeNote) bool { return n.id == id })
	if i < 0 {
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)

		return
	}

	if r.Method == http.MethodDelete {
		f.notes = slices.Delete(f.notes, i, i+1)
		f.writes = append(f.writes, "delete "+strconv.FormatInt(id, 10))
		w.WriteHeader(http.StatusNoContent)

		return
	}

	f.notes[i].body = decodeBody(r)
	f.writes = append(f.writes, "update "+strconv.FormatInt(id, 10))
	_, _ = fmt.Fprintf(w, `{"id":%d}`, id)
}

func (f *fake) upload(w http.ResponseWriter, r *http.Request) {
	file, head, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	defer file.Close()

	data, _ := io.ReadAll(file)
	f.writes = append(f.writes, "upload "+head.Filename+" "+string(data))
	_, _ = io.WriteString(w, `{"url":"/uploads/abc/`+head.Filename+`"}`)
}

func decodeBody(r *http.Request) string {
	var in struct {
		Body string `json:"body"`
	}

	_ = json.NewDecoder(r.Body).Decode(&in)

	return in.Body
}

func client(t *testing.T, f http.Handler) *gitlab.Client {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	return &gitlab.Client{HTTP: srv.Client(), BaseURL: srv.URL + "/api/v4/", Token: "secret", UserAgent: "test"}
}

func upsert(dry bool) *forge.Upsert {
	return &forge.Upsert{
		Repo: "group/sub/catalog", Issue: 7, Marker: marker, Author: bot, Body: marker + "\nnew", DryRun: dry,
	}
}

func TestUpsertNoteCreates(t *testing.T) {
	f := &fake{}
	f.add(fakeNote{author: "alice", body: marker + "\npasted by a person"})
	f.add(fakeNote{author: bot, body: "another note of the bot"})
	f.add(fakeNote{author: bot, body: marker + "\nsystem", system: true})

	plan, err := client(t, f).UpsertNote(context.Background(), upsert(false))
	if err != nil {
		t.Fatal(err)
	}

	if plan.Action != forge.ActionCreate || plan.URL != webURL+"#note_4" {
		t.Errorf("plan %+v", plan)
	}

	if !slices.Equal(f.writes, []string{"create"}) || f.notes[0].body != marker+"\npasted by a person" {
		t.Errorf("writes %v, notes %+v", f.writes, f.notes)
	}
}

func TestUpsertNoteUpdatesOldestAndDeletesOthers(t *testing.T) {
	f := &fake{}
	f.add(fakeNote{author: bot, body: marker + "\nfirst"})
	f.add(fakeNote{author: "alice", body: "a question"})
	f.add(fakeNote{author: bot, body: marker + "\nsecond"})

	plan, err := client(t, f).UpsertNote(context.Background(), upsert(false))
	if err != nil {
		t.Fatal(err)
	}

	if plan.Action != forge.ActionUpdate || plan.Update != 1 || plan.URL != webURL+"#note_1" {
		t.Errorf("plan %+v", plan)
	}

	if !slices.Equal(f.writes, []string{"update 1", "delete 3"}) || f.notes[0].body != marker+"\nnew" {
		t.Errorf("writes %v, notes %+v", f.writes, f.notes)
	}
}

func TestUpsertNoteDryRunWritesNothing(t *testing.T) {
	f := &fake{}
	f.add(fakeNote{author: bot, body: marker + "\nfirst"})
	f.add(fakeNote{author: bot, body: marker + "\nsecond"})

	plan, err := client(t, f).UpsertNote(context.Background(), upsert(true))
	if err != nil {
		t.Fatal(err)
	}

	if plan.Action != forge.ActionUpdate || !slices.Equal(plan.Delete, []int64{2}) || len(f.writes) != 0 {
		t.Errorf("plan %+v, writes %v", plan, f.writes)
	}
}

func TestUpsertNoteReadsEveryPage(t *testing.T) {
	f := &fake{}
	for range 100 {
		f.add(fakeNote{author: "alice", body: "chatter"})
	}

	f.add(fakeNote{author: bot, body: marker + "\non the second page"})

	plan, err := client(t, f).UpsertNote(context.Background(), upsert(false))
	if err != nil {
		t.Fatal(err)
	}

	if plan.Action != forge.ActionUpdate || plan.Update != 101 {
		t.Errorf("plan %+v", plan)
	}
}

func TestUploadAndCurrentUser(t *testing.T) {
	f := &fake{}
	c := client(t, f)

	got, err := c.Upload(context.Background(), "group/sub/catalog", "schedule.svg", []byte("<svg/>"))
	if err != nil {
		t.Fatal(err)
	}

	if got != "/uploads/abc/schedule.svg" || !slices.Equal(f.writes, []string{"upload schedule.svg <svg/>"}) {
		t.Errorf("got %q, writes %v", got, f.writes)
	}

	user, err := c.CurrentUser(context.Background())
	if err != nil || user != bot {
		t.Errorf("user %q, err %v", user, err)
	}
}

func TestUploadRefusesAnUnexpectedPath(t *testing.T) {
	c := client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"url":"https://elsewhere.example/x.svg"}`)
	}))

	if _, err := c.Upload(context.Background(), "1", "schedule.svg", nil); err == nil {
		t.Error("want an error")
	}
}

func TestProjectForms(t *testing.T) {
	var got string

	c := client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath()

		_, _ = io.WriteString(w, `[]`)
	}))

	u := upsert(true)
	u.Repo = "42"

	// The issue answer is a list here, so only the requested path matters.
	_, _ = c.UpsertNote(context.Background(), u)

	if got != "/api/v4/projects/42/issues/7" {
		t.Errorf("path %q", got)
	}

	for _, bad := range []string{"", "name", "/group/x", "group/x/", "group//x"} {
		u.Repo = bad

		if _, err := c.UpsertNote(context.Background(), u); err == nil {
			t.Errorf("project %q: want an error", bad)
		}
	}
}

// TestTokenNeverFollowsARedirect: Go keeps custom headers on a redirect, even to another host.
func TestTokenNeverFollowsARedirect(t *testing.T) {
	leaked := false

	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("PRIVATE-TOKEN") != ""
	}))
	defer other.Close()

	c := client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))

	if _, err := c.CurrentUser(context.Background()); err == nil {
		t.Error("want an error for the redirect")
	}

	if leaked {
		t.Error("the token reached the other host")
	}
}

func TestAPIErrorsAreReported(t *testing.T) {
	c := client(t, &fake{})
	c.Token = "wrong"

	_, err := c.UpsertNote(context.Background(), upsert(false))
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "wrong") {
		t.Errorf("err %v", err)
	}
}
