package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iilei/canned-schedules-companion/internal/forge"
)

type user struct {
	Username string `json:"username"`
}

type mergeRequest struct {
	MergedAt  *time.Time `json:"merged_at"`
	MergeUser *user      `json:"merge_user"`
	// MergedBy is deprecated in favour of merge_user, which older instances lack.
	MergedBy   *user `json:"merged_by"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
	CreatedAt       time.Time `json:"created_at"`
	Author          user      `json:"author"`
	State           string    `json:"state"`
	WebURL          string    `json:"web_url"`
	Title           string    `json:"title"`
	TargetBranch    string    `json:"target_branch"`
	SHA             string    `json:"sha"`
	MergeCommitSHA  string    `json:"merge_commit_sha"`
	SquashCommitSHA string    `json:"squash_commit_sha"`
	IID             int       `json:"iid"`
}

// MergedRequest finds the merged merge request whose result is sha: its merge commit, its squash commit, or (for a
// fast-forward merge) its head. This works after squash and rebase merges, which give the commits new SHAs. Without
// one the error is forge.ErrNotMerged.
func (c *Client) MergedRequest(ctx context.Context, repo, sha string) (*forge.Merged, error) {
	project, err := projectPath(repo)
	if err != nil {
		return nil, err
	}

	var mrs []mergeRequest

	path := project + "/repository/commits/" + url.PathEscape(sha) + "/merge_requests"
	if err := c.do(ctx, http.MethodGet, path, nil, &mrs); err != nil {
		return nil, err
	}

	for i := range mrs {
		mr := &mrs[i]
		if mr.State != "merged" || mr.MergedAt == nil || (mr.MergeCommitSHA != sha && mr.SquashCommitSHA != sha &&
			mr.SHA != sha) {
			continue
		}

		return merged(mr)
	}

	return nil, fmt.Errorf("commit %s: %w", sha, forge.ErrNotMerged)
}

func merged(mr *mergeRequest) (*forge.Merged, error) {
	u, err := url.Parse(mr.WebURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("merge request !%d: the answer has no usable web_url", mr.IID)
	}

	project, _, _ := strings.Cut(mr.References.Full, "!")
	if project == "" {
		project, _, _ = strings.Cut(strings.TrimPrefix(u.Path, "/"), "/-/")
	}

	by := mr.MergeUser
	if by == nil {
		by = mr.MergedBy
	}

	out := &forge.Merged{
		CreatedAt: mr.CreatedAt, MergedAt: *mr.MergedAt, Host: u.Host, Project: project, URL: mr.WebURL,
		Title: mr.Title, Author: mr.Author.Username, TargetBranch: mr.TargetBranch, Number: mr.IID,
		MergeCommit: mr.MergeCommitSHA,
	}

	if by != nil {
		out.MergedBy = by.Username
	}

	if out.MergeCommit == "" {
		out.MergeCommit = mr.SquashCommitSHA
	}

	if out.MergeCommit == "" {
		out.MergeCommit = mr.SHA
	}

	return out, nil
}
