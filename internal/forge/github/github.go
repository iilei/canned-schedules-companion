// Package github is the GitHub part of the forge steps: it keeps one marked comment per issue up to date through the
// REST API. Nothing else in the companion talks to GitHub.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	ActionCreate = "create"
	ActionUpdate = "update"
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
type Plan struct {
	// Action is ActionCreate or ActionUpdate.
	Action string
	// URL is the comment's page; empty for a create in a dry run.
	URL string
	// Delete lists further marked comments by the author, removed as duplicates.
	Delete []int64
	// Update is the comment updated (0 for a create).
	Update int64
}

// Upsert describes one comment to keep up to date.
type Upsert struct {
	// Repo is OWNER/REPO.
	Repo string
	// Marker starts the comment; only comments that start with it count.
	Marker string
	// Author is the login whose comments count, so a pasted marker by someone else is never touched.
	Author string
	// Body is the full comment, marker included.
	Body string
	// Issue is the issue (or pull request) number.
	Issue int
	// DryRun reads but never writes.
	DryRun bool
}

type comment struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	ID      int64  `json:"id"`
}

// UpsertComment finds the comments on the issue that start with u.Marker and were written by u.Author. It updates
// the oldest and deletes the others, or creates the comment when there is none.
func (c *Client) UpsertComment(ctx context.Context, u *Upsert) (*Plan, error) {
	owner, name, ok := strings.Cut(u.Repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("repo %q: %w", u.Repo, errRepo)
	}

	base := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)

	marked, err := c.marked(ctx, base, u)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Action: ActionCreate}
	if len(marked) > 0 {
		plan.Action, plan.Update, plan.URL = ActionUpdate, marked[0].ID, marked[0].HTMLURL
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
