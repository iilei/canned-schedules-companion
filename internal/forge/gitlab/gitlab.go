// Package gitlab is the GitLab part of the forge steps: it keeps one marked note per issue up to date through the
// REST API and uploads the files a note shows. Nothing else in the companion talks to GitLab.
package gitlab

import (
	"bytes"
	"context"
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

	issue := project + "/issues/" + strconv.Itoa(u.Issue)

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

		return fmt.Errorf("%s %s: %s: %s", method, req.URL.Path, resp.Status, bytes.TrimSpace(msg))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, req.URL.Path, err)
	}

	return nil
}
