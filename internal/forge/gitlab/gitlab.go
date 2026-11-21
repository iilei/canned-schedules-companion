// Package gitlab is the GitLab part of the forge steps: it keeps one marked note per issue up to date through the
// REST API and uploads the files a note shows. Nothing else in the companion talks to GitLab.
package gitlab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iilei/canned-schedules-companion/internal/forge"
)

// DefaultAPIURL is gitlab.com's REST API. A self-managed instance has its own (in a job: CI_API_V4_URL).
const DefaultAPIURL = "https://gitlab.com/api/v4"

const (
	perPage        = 100
	requestTimeout = 30 * time.Second
	// maxErrorBody caps how much of an error response ends up in a message.
	maxErrorBody = 512
	uploadPrefix = "/uploads/"
)

var (
	errRepo   = errors.New("want GROUP/PROJECT or a numeric project ID")
	errUpload = errors.New("the answer names no file under " + uploadPrefix)
	errUser   = errors.New("the token's user has no username")
	errLabel  = errors.New("GitLab reads commas as separators, so a label can't contain one")
)

// Client calls the REST API with one token.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	Token     string
	UserAgent string
}

type note struct {
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
	Body   string `json:"body"`
	ID     int64  `json:"id"`
	System bool   `json:"system"`
}

// errStatus marks an answer whose HTTP status a caller looks at, e.g. 404 for a missing branch or file.
type errStatus struct {
	err  error
	code int
}

// CurrentUser is the username behind the token. A project access token has a generated one, so this is how a step
// finds its own notes without being told.
func (c *Client) CurrentUser(ctx context.Context) (string, error) {
	var user struct {
		Username string `json:"username"`
	}

	if err := c.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", err
	}

	if user.Username == "" {
		return "", errUser
	}

	return user.Username, nil
}

// UpsertNote finds the notes on the issue that start with u.Marker and were written by u.Author. It updates the
// oldest and deletes the others, or creates the note when there is none.
func (c *Client) UpsertNote(ctx context.Context, u *forge.Upsert) (*forge.Plan, error) {
	project, err := projectPath(u.Repo)
	if err != nil {
		return nil, err
	}

	issue := project + resource(u.MergeRequest) + strconv.Itoa(u.Issue)

	var info struct {
		WebURL string `json:"web_url"`
	}

	if err := c.do(ctx, http.MethodGet, issue, nil, &info); err != nil {
		return nil, err
	}

	marked, err := c.marked(ctx, issue, u)
	if err != nil {
		return nil, err
	}

	plan := &forge.Plan{Action: forge.ActionCreate}
	if len(marked) > 0 {
		plan.Action, plan.Update, plan.URL = forge.ActionUpdate, marked[0].ID, noteURL(info.WebURL, marked[0].ID)
		plan.Existing = marked[0].Body
		for _, m := range marked[1:] {
			plan.Delete = append(plan.Delete, m.ID)
		}
	}

	if u.DryRun {
		return plan, nil
	}

	payload := map[string]string{"body": u.Body}

	var got note

	if plan.Action == forge.ActionUpdate {
		err = c.do(ctx, http.MethodPut, issue+"/notes/"+strconv.FormatInt(plan.Update, 10), payload, &got)
	} else {
		err = c.do(ctx, http.MethodPost, issue+"/notes", payload, &got)
	}

	if err != nil {
		return nil, err
	}

	plan.URL = noteURL(info.WebURL, got.ID)

	for _, id := range plan.Delete {
		if err := c.do(ctx, http.MethodDelete, issue+"/notes/"+strconv.FormatInt(id, 10), nil, nil); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

// Issue reads the issue's description and labels.
func (c *Client) Issue(ctx context.Context, repo string, issue int) (*forge.Issue, error) {
	project, err := projectPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
	}

	if err := c.do(ctx, http.MethodGet, project+"/issues/"+strconv.Itoa(issue), nil, &got); err != nil {
		return nil, err
	}

	return &forge.Issue{Body: got.Description, Labels: got.Labels}, nil
}

// AddLabel adds a label to the issue; GitLab creates the label when the project doesn't have it yet.
func (c *Client) AddLabel(ctx context.Context, repo string, issue int, label string) error {
	return c.label(ctx, repo, issue, "add_labels", label)
}

// RemoveLabel removes a label from the issue.
func (c *Client) RemoveLabel(ctx context.Context, repo string, issue int, label string) error {
	return c.label(ctx, repo, issue, "remove_labels", label)
}

// MergeRequest reads a merge request's title, head commit and labels.
func (c *Client) MergeRequest(ctx context.Context, repo string, iid int) (*forge.MergeRequest, error) {
	project, err := projectPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Title  string   `json:"title"`
		SHA    string   `json:"sha"`
		Labels []string `json:"labels"`
	}

	if err := c.do(ctx, http.MethodGet, project+"/merge_requests/"+strconv.Itoa(iid), nil, &got); err != nil {
		return nil, err
	}

	return &forge.MergeRequest{Title: got.Title, SHA: got.SHA, Labels: got.Labels}, nil
}

// AddMergeRequestLabel adds a label to a merge request.
func (c *Client) AddMergeRequestLabel(ctx context.Context, repo string, iid int, label string) error {
	return c.updateMergeRequest(ctx, repo, iid, "add_labels", label)
}

// RemoveMergeRequestLabel removes a label from a merge request.
func (c *Client) RemoveMergeRequestLabel(ctx context.Context, repo string, iid int, label string) error {
	return c.updateMergeRequest(ctx, repo, iid, "remove_labels", label)
}

// SetMergeRequestTitle renames a merge request.
func (c *Client) SetMergeRequestTitle(ctx context.Context, repo string, iid int, title string) error {
	return c.updateMergeRequest(ctx, repo, iid, "title", title)
}

// Upload stores a file in the project and returns its path, relative to the project, for use in a note.
func (c *Client) Upload(ctx context.Context, repo, name string, data []byte) (string, error) {
	project, err := projectPath(repo)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	form := multipart.NewWriter(&buf)

	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}

	if _, err := part.Write(data); err != nil {
		return "", err
	}

	if err := form.Close(); err != nil {
		return "", err
	}

	var got struct {
		URL string `json:"url"`
	}

	err = c.send(ctx, http.MethodPost, project+"/uploads", form.FormDataContentType(), &buf, &got)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(got.URL, uploadPrefix) {
		return "", fmt.Errorf("upload of %s: %w", name, errUpload)
	}

	return got.URL, nil
}

// BranchExists reports whether the branch exists.
func (c *Client) BranchExists(ctx context.Context, repo, branch string) (bool, error) {
	base, err := projectPath(repo)
	if err != nil {
		return false, err
	}

	err = c.do(ctx, http.MethodGet, base+"/repository/branches/"+url.PathEscape(branch), nil, nil)
	if notFound(err) {
		return false, nil
	}

	return err == nil, err
}

// File reads a file on a branch; nil without an error means the branch has no such file.
func (c *Client) File(ctx context.Context, repo, ref, file string) (*forge.File, error) {
	base, err := projectPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}

	path := base + "/repository/files/" + url.PathEscape(file) + "?ref=" + url.QueryEscape(ref)

	err = c.do(ctx, http.MethodGet, path, nil, &got)
	if notFound(err) {
		return nil, nil //nolint:nilnil // No file is an answer, not an error.
	}

	if err != nil {
		return nil, err
	}

	if got.Encoding != "base64" {
		return nil, fmt.Errorf("%s: content encoded as %q", file, got.Encoding)
	}

	raw, err := base64.StdEncoding.DecodeString(got.Content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	return &forge.File{Content: raw}, nil
}

// CommitFile writes one file to a branch in one commit, creating the branch from cm.From when it is set.
func (c *Client) CommitFile(ctx context.Context, cm *forge.Commit) error {
	base, err := projectPath(cm.Repo)
	if err != nil {
		return err
	}

	action := "create"
	if cm.Replace != nil {
		action = "update"
	}

	payload := map[string]any{
		"branch":         cm.Branch,
		"commit_message": cm.Message,
		"actions": []map[string]string{{
			"action": action, "file_path": cm.Path, "content": base64.StdEncoding.EncodeToString(cm.Content),
			"encoding": "base64",
		}},
	}
	if cm.From != "" {
		payload["start_branch"] = cm.From
	}

	return c.do(ctx, http.MethodPost, base+"/repository/commits", payload, nil)
}

// OpenRequest finds the open merge request from the branch, or opens one; it returns the merge request's page and
// whether it was opened now.
func (c *Client) OpenRequest(ctx context.Context, r *forge.Request, dryRun bool) (string, bool, error) {
	base, err := projectPath(r.Repo)
	if err != nil {
		return "", false, err
	}

	var open []struct {
		URL string `json:"web_url"`
	}

	q := "?state=opened&source_branch=" + url.QueryEscape(r.Branch)
	if err := c.do(ctx, http.MethodGet, base+"/merge_requests"+q, nil, &open); err != nil {
		return "", false, err
	}

	if len(open) > 0 {
		return open[0].URL, false, nil
	}

	if dryRun {
		return "", true, nil
	}

	var made struct {
		URL string `json:"web_url"`
	}

	payload := map[string]any{
		"source_branch": r.Branch, "target_branch": r.Base, "title": r.Title, "description": r.Body,
		"remove_source_branch": true,
	}
	if err := c.do(ctx, http.MethodPost, base+"/merge_requests", payload, &made); err != nil {
		return "", false, err
	}

	return made.URL, true, nil
}

func (c *Client) label(ctx context.Context, repo string, issue int, field, label string) error {
	// The API reads these fields as comma-separated lists.
	if strings.Contains(label, ",") {
		return fmt.Errorf("label %q: %w", label, errLabel)
	}

	project, err := projectPath(repo)
	if err != nil {
		return err
	}

	return c.do(ctx, http.MethodPut, project+"/issues/"+strconv.Itoa(issue), map[string]string{field: label}, nil)
}

func (c *Client) updateMergeRequest(ctx context.Context, repo string, iid int, field, value string) error {
	if field != "title" && strings.Contains(value, ",") {
		return fmt.Errorf("label %q: %w", value, errLabel)
	}

	project, err := projectPath(repo)
	if err != nil {
		return err
	}

	return c.do(ctx, http.MethodPut, project+"/merge_requests/"+strconv.Itoa(iid), map[string]string{field: value}, nil)
}

// resource is the path segment of the thing a note goes to.
func resource(mergeRequest bool) string {
	if mergeRequest {
		return "/merge_requests/"
	}

	return "/issues/"
}

// marked lists the issue's notes by u.Author that start with u.Marker, oldest first. System notes ("closed the
// issue") never count.
func (c *Client) marked(ctx context.Context, issue string, u *forge.Upsert) ([]note, error) {
	var marked []note

	for page := 1; ; page++ {
		var notes []note

		path := fmt.Sprintf("%s/notes?sort=asc&order_by=created_at&per_page=%d&page=%d", issue, perPage, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &notes); err != nil {
			return nil, err
		}

		for _, n := range notes {
			if !n.System && n.Author.Username == u.Author && strings.HasPrefix(n.Body, u.Marker) {
				marked = append(marked, n)
			}
		}

		if len(notes) < perPage {
			return marked, nil
		}
	}
}

func projectPath(repo string) (string, error) {
	_, notNumeric := strconv.Atoi(repo)
	if repo == "" || strings.HasPrefix(repo, "/") || strings.HasSuffix(repo, "/") || strings.Contains(repo, "//") ||
		(notNumeric != nil && !strings.Contains(repo, "/")) {
		return "", fmt.Errorf("project %q: %w", repo, errRepo)
	}

	// The API takes a path as one segment, with its slashes encoded.
	return "/projects/" + url.PathEscape(repo), nil
}

func noteURL(issue string, id int64) string {
	if issue == "" {
		return ""
	}

	return issue + "#note_" + strconv.FormatInt(id, 10)
}

func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	if payload == nil {
		return c.send(ctx, method, path, "", nil, out)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return c.send(ctx, method, path, "application/json", bytes.NewReader(raw), out)
}

func (c *Client) send(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("User-Agent", c.UserAgent)

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Go keeps custom headers across redirects, even to another host, so the token must never follow one.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

		return &errStatus{
			err:  fmt.Errorf("%s %s: %s: %s", method, req.URL.Path, resp.Status, bytes.TrimSpace(msg)),
			code: resp.StatusCode,
		}
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, req.URL.Path, err)
	}

	return nil
}

func (e *errStatus) Error() string { return e.err.Error() }

func (e *errStatus) Unwrap() error { return e.err }

// notFound reports whether err is the API's 404.
func notFound(err error) bool {
	var s *errStatus

	return errors.As(err, &s) && s.code == http.StatusNotFound
}
