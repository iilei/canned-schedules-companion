// Package github is the GitHub part of the forge steps: it keeps one marked comment per issue or pull request up to
// date through the REST API, and reads, labels and renames pull requests. Nothing else in the companion talks to
// GitHub.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iilei/canned-schedules-companion/internal/forge"
)

// DefaultAPIURL is github.com's REST API. GitHub Enterprise Server has its own (the action passes github.api_url).
const DefaultAPIURL = "https://api.github.com"

// DefaultAuthor is the account the default workflow token posts as.
const DefaultAuthor = "github-actions[bot]"

const (
	perPage        = 100
	requestTimeout = 30 * time.Second
	apiVersion     = "2022-11-28"
	// maxErrorBody caps how much of an error response ends up in a message.
	maxErrorBody = 512
)

// Actions that UpsertComment takes.
const (
	ActionCreate = forge.ActionCreate
	ActionUpdate = forge.ActionUpdate
)

var errRepo = errors.New("want OWNER/REPO")

// Client calls the REST API with one token.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	Token     string
	UserAgent string
}

// Plan is what UpsertComment did, or would do with DryRun.
type Plan = forge.Plan

// Upsert describes one comment to keep up to date.
type Upsert = forge.Upsert

type comment struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	ID      int64  `json:"id"`
}

// errStatus marks an answer whose HTTP status a caller looks at, e.g. 404 for a missing branch or file.
type errStatus struct {
	err  error
	code int
}

// UpsertComment finds the comments on the issue that start with u.Marker and were written by u.Author. It updates
// the oldest and deletes the others, or creates the comment when there is none.
func (c *Client) UpsertComment(ctx context.Context, u *Upsert) (*Plan, error) {
	base, err := repoPath(u.Repo)
	if err != nil {
		return nil, err
	}

	marked, err := c.marked(ctx, base, u)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Action: ActionCreate}
	if len(marked) > 0 {
		plan.Action, plan.Update, plan.URL = ActionUpdate, marked[0].ID, marked[0].HTMLURL
		plan.Existing = marked[0].Body
		for _, m := range marked[1:] {
			plan.Delete = append(plan.Delete, m.ID)
		}
	}

	if u.DryRun {
		return plan, nil
	}

	payload := map[string]string{"body": u.Body}

	var got comment

	if plan.Action == ActionUpdate {
		err = c.do(ctx, http.MethodPatch, base+"/issues/comments/"+strconv.FormatInt(plan.Update, 10), payload, &got)
	} else {
		err = c.do(ctx, http.MethodPost, base+"/issues/"+strconv.Itoa(u.Issue)+"/comments", payload, &got)
	}

	if err != nil {
		return nil, err
	}

	plan.URL = got.HTMLURL

	for _, id := range plan.Delete {
		path := base + "/issues/comments/" + strconv.FormatInt(id, 10)
		if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

// Issue reads the issue's body and labels.
func (c *Client) Issue(ctx context.Context, repo string, issue int) (*forge.Issue, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}

	if err := c.do(ctx, http.MethodGet, base+"/issues/"+strconv.Itoa(issue), nil, &got); err != nil {
		return nil, err
	}

	out := &forge.Issue{Body: got.Body}
	for _, l := range got.Labels {
		out.Labels = append(out.Labels, l.Name)
	}

	return out, nil
}

// AddLabel adds a label to the issue; GitHub creates the label when the repository doesn't have it yet.
func (c *Client) AddLabel(ctx context.Context, repo string, issue int, label string) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}

	payload := map[string][]string{"labels": {label}}

	return c.do(ctx, http.MethodPost, base+"/issues/"+strconv.Itoa(issue)+"/labels", payload, nil)
}

// RemoveLabel removes a label from the issue.
func (c *Client) RemoveLabel(ctx context.Context, repo string, issue int, label string) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}

	path := base + "/issues/" + strconv.Itoa(issue) + "/labels/" + url.PathEscape(label)

	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// UpsertNote is UpsertComment under the name the review step uses for both forges. A pull request takes comments
// through the issues API, so u.MergeRequest changes nothing here.
func (c *Client) UpsertNote(ctx context.Context, u *Upsert) (*Plan, error) {
	return c.UpsertComment(ctx, u)
}

// MergeRequest reads a pull request's title, head commit and labels.
func (c *Client) MergeRequest(ctx context.Context, repo string, number int) (*forge.MergeRequest, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Title string `json:"title"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}

	if err := c.do(ctx, http.MethodGet, base+"/pulls/"+strconv.Itoa(number), nil, &got); err != nil {
		return nil, err
	}

	out := &forge.MergeRequest{Title: got.Title, SHA: got.Head.SHA}
	for _, l := range got.Labels {
		out.Labels = append(out.Labels, l.Name)
	}

	return out, nil
}

// AddMergeRequestLabel adds a label to a pull request (through the issues API, like any issue).
func (c *Client) AddMergeRequestLabel(ctx context.Context, repo string, number int, label string) error {
	return c.AddLabel(ctx, repo, number, label)
}

// RemoveMergeRequestLabel removes a label from a pull request.
func (c *Client) RemoveMergeRequestLabel(ctx context.Context, repo string, number int, label string) error {
	return c.RemoveLabel(ctx, repo, number, label)
}

// SetMergeRequestTitle renames a pull request.
func (c *Client) SetMergeRequestTitle(ctx context.Context, repo string, number int, title string) error {
	base, err := repoPath(repo)
	if err != nil {
		return err
	}

	return c.do(ctx, http.MethodPatch, base+"/pulls/"+strconv.Itoa(number), map[string]string{"title": title}, nil)
}

func (e *errStatus) Error() string { return e.err.Error() }

func (e *errStatus) Unwrap() error { return e.err }

// notFound reports whether err is the API's 404.
func notFound(err error) bool {
	var s *errStatus

	return errors.As(err, &s) && s.code == http.StatusNotFound
}

// BranchExists reports whether the branch exists.
func (c *Client) BranchExists(ctx context.Context, repo, branch string) (bool, error) {
	base, err := repoPath(repo)
	if err != nil {
		return false, err
	}

	err = c.do(ctx, http.MethodGet, base+"/branches/"+url.PathEscape(branch), nil, nil)
	if notFound(err) {
		return false, nil
	}

	return err == nil, err
}

// File reads a file on a branch; nil without an error means the branch has no such file.
func (c *Client) File(ctx context.Context, repo, ref, file string) (*forge.File, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}

	var got struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}

	err = c.do(ctx, http.MethodGet, base+"/contents/"+escapeFilePath(file)+"?ref="+url.QueryEscape(ref), nil, &got)
	if notFound(err) {
		return nil, nil //nolint:nilnil // No file is an answer, not an error.
	}

	if err != nil {
		return nil, err
	}

	if got.Encoding != "base64" {
		return nil, fmt.Errorf("%s: content encoded as %q", file, got.Encoding)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(got.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	return &forge.File{Content: raw, Version: got.SHA}, nil
}

// CommitFile writes one file to a branch, creating the branch from c.From first when it is set.
func (c *Client) CommitFile(ctx context.Context, cm *forge.Commit) error {
	base, err := repoPath(cm.Repo)
	if err != nil {
		return err
	}

	if cm.From != "" {
		var from struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}

		if err := c.do(ctx, http.MethodGet, base+"/git/ref/heads/"+escapeFilePath(cm.From), nil, &from); err != nil {
			return err
		}

		ref := map[string]string{"ref": "refs/heads/" + cm.Branch, "sha": from.Object.SHA}
		if err := c.do(ctx, http.MethodPost, base+"/git/refs", ref, nil); err != nil {
			return err
		}
	}

	payload := map[string]string{
		"message": cm.Message,
		"content": base64.StdEncoding.EncodeToString(cm.Content),
		"branch":  cm.Branch,
	}
	if cm.Replace != nil {
		payload["sha"] = cm.Replace.Version
	}

	return c.do(ctx, http.MethodPut, base+"/contents/"+escapeFilePath(cm.Path), payload, nil)
}

// OpenRequest finds the open pull request from the branch, or opens one; it returns the pull request's page and
// whether it was opened now.
func (c *Client) OpenRequest(ctx context.Context, r *forge.Request, dryRun bool) (string, bool, error) {
	base, err := repoPath(r.Repo)
	if err != nil {
		return "", false, err
	}

	owner, _, _ := strings.Cut(r.Repo, "/")

	var open []struct {
		URL string `json:"html_url"`
	}

	q := "?state=open&head=" + url.QueryEscape(owner+":"+r.Branch)
	if err := c.do(ctx, http.MethodGet, base+"/pulls"+q, nil, &open); err != nil {
		return "", false, err
	}

	if len(open) > 0 {
		return open[0].URL, false, nil
	}

	if dryRun {
		return "", true, nil
	}

	var made struct {
		URL string `json:"html_url"`
	}

	payload := map[string]string{"title": r.Title, "head": r.Branch, "base": r.Base, "body": r.Body}
	if err := c.do(ctx, http.MethodPost, base+"/pulls", payload, &made); err != nil {
		return "", false, err
	}

	return made.URL, true, nil
}

// escapeFilePath escapes each segment of a repository path, keeping the slashes.
func escapeFilePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}

	return strings.Join(parts, "/")
}

func repoPath(repo string) (string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("repo %q: %w", repo, errRepo)
	}

	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

// marked lists the issue's comments by u.Author that start with u.Marker, oldest first (the API's order).
func (c *Client) marked(ctx context.Context, base string, u *Upsert) ([]comment, error) {
	var marked []comment

	for page := 1; ; page++ {
		var cs []comment

		path := fmt.Sprintf("%s/issues/%d/comments?per_page=%d&page=%d", base, u.Issue, perPage, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &cs); err != nil {
			return nil, err
		}

		for _, cm := range cs {
			if cm.User.Login == u.Author && strings.HasPrefix(cm.Body, u.Marker) {
				marked = append(marked, cm)
			}
		}

		if len(cs) < perPage {
			return marked, nil
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader

	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		body = bytes.NewReader(raw)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.UserAgent)

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
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
