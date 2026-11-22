package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/iilei/canned-schedules-companion/internal/forge"
)

type login struct {
	Login string `json:"login"`
}

type pull struct {
	MergedAt *time.Time `json:"merged_at"`
	MergedBy *login     `json:"merged_by"`
	Base     struct {
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
		Ref string `json:"ref"`
	} `json:"base"`
	CreatedAt      time.Time `json:"created_at"`
	User           login     `json:"user"`
	HTMLURL        string    `json:"html_url"`
	Title          string    `json:"title"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	Number         int       `json:"number"`
}

// MergedRequest finds the merged pull request whose merge commit is sha: the merge commit, the squash commit or the
// last rebased commit, so it works after every merge method. Without one the error is forge.ErrNotMerged.
func (c *Client) MergedRequest(ctx context.Context, repo, sha string) (*forge.Merged, error) {
	base, err := repoPath(repo)
	if err != nil {
		return nil, err
	}

	var pulls []pull

	if err := c.do(ctx, http.MethodGet, base+"/commits/"+url.PathEscape(sha)+"/pulls", nil, &pulls); err != nil {
		return nil, err
	}

	for i := range pulls {
		if pulls[i].MergedAt == nil || pulls[i].MergeCommitSHA != sha {
			continue
		}

		// The list leaves out merged_by; the pull request itself has it.
		var p pull
		if err := c.do(ctx, http.MethodGet, base+"/pulls/"+strconv.Itoa(pulls[i].Number), nil, &p); err != nil {
			return nil, err
		}

		return merged(&p)
	}

	return nil, fmt.Errorf("commit %s: %w", sha, forge.ErrNotMerged)
}

func merged(p *pull) (*forge.Merged, error) {
	u, err := url.Parse(p.HTMLURL)
	if err != nil || u.Host == "" || p.MergedAt == nil {
		return nil, fmt.Errorf("pull request #%d: the answer has no usable html_url or merged_at", p.Number)
	}

	out := &forge.Merged{
		CreatedAt: p.CreatedAt, MergedAt: *p.MergedAt, Host: u.Host, Project: p.Base.Repo.FullName, URL: p.HTMLURL,
		Title: p.Title, Author: p.User.Login, TargetBranch: p.Base.Ref, MergeCommit: p.MergeCommitSHA, Number: p.Number,
	}

	if p.MergedBy != nil {
		out.MergedBy = p.MergedBy.Login
	}

	return out, nil
}
