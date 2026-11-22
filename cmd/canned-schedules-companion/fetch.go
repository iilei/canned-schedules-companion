package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/iilei/canned-schedules-companion/internal/forge"
	"github.com/iilei/canned-schedules-companion/internal/forge/github"
	"github.com/iilei/canned-schedules-companion/internal/publish"
	"github.com/iilei/canned-schedules-companion/internal/review"
)

// fetchForge is what publish fetch reads from a forge: the merged merge request behind a commit, and the review
// comment the review step keeps on it (found as review post finds it, through a dry-run upsert).
type fetchForge interface {
	MergedRequest(ctx context.Context, repo, sha string) (*forge.Merged, error)
	UpsertNote(ctx context.Context, u *forge.Upsert) (*forge.Plan, error)
}

type fetchFlags struct {
	forge, repo, commit, apiURL, author, tokenEnv, out string
}

// publishFetch finds the merged merge request a pushed commit came from and writes it, with the digests its review
// vouched for, as publish prepare's input. It only reads from the forge.
func publishFetch(args []string, stdout, stderr io.Writer) int {
	f, err := parseFetch(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := fetch(f, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		if errors.Is(err, forge.ErrNotMerged) {
			return exitInvalid
		}

		return exitUsage
	}

	return exitOK
}

func fetch(f *fetchFlags, stdout io.Writer) error {
	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := f.client(ctx, token)
	if err != nil {
		return err
	}

	in, err := fetchInput(ctx, c, f, stdout)
	if err != nil {
		return err
	}

	raw, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}

	// What publish prepare will read: check it here, so a forge's odd answer fails in the step that read it.
	if _, err := publish.ParseInput(raw); err != nil {
		return err
	}

	if err := writeFile(f.out, append(raw, '\n')); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "wrote %s\n", f.out)

	return nil
}

func fetchInput(ctx context.Context, c fetchForge, f *fetchFlags, stdout io.Writer) (*publish.Input, error) {
	mr, err := c.MergedRequest(ctx, f.repo, f.commit)
	if err != nil {
		return nil, err
	}

	in := &publish.Input{MergeRequest: publish.MergeRequest{
		CreatedAt: mr.CreatedAt.UTC(), MergedAt: mr.MergedAt.UTC(), Forge: f.forge, Host: mr.Host, Project: mr.Project,
		URL: mr.URL, Title: mr.Title, Author: mr.Author, MergedBy: mr.MergedBy, TargetBranch: mr.TargetBranch,
		MergeCommit: mr.MergeCommit, Number: mr.Number,
	}}

	fmt.Fprintf(stdout, "%s came from %s, merged by %s at %s\n", f.commit, mr.URL, mr.MergedBy,
		in.MergeRequest.MergedAt.Format("2006-01-02T15:04:05Z"))

	plan, err := c.UpsertNote(ctx, &forge.Upsert{
		Repo: f.repo, Issue: mr.Number, MergeRequest: true, Marker: review.Marker, Author: f.author, DryRun: true,
	})
	if err != nil {
		return nil, err
	}

	switch digests, ok := review.Digests(plan.Existing); {
	case plan.Existing == "":
		fmt.Fprintf(stdout, "no review comment by %s: every schedule is published as not reviewed\n", f.author)
	case !ok:
		fmt.Fprintln(stdout, "the review comment lists no digests (an older release wrote it): every schedule is "+
			"published as not reviewed")
	default:
		in.Review = &publish.Review{Digests: digests, URL: plan.URL}
		fmt.Fprintf(stdout, "the review %s vouches for %d file(s)\n", plan.URL, len(digests))
	}

	return in, nil
}

// client makes the forge's client, as review post does: on GitLab an empty --author is the token's user.
func (f *fetchFlags) client(ctx context.Context, token string) (fetchForge, error) {
	return newMergeRequestClient(ctx, f.forge, f.apiURL, token, &f.author)
}

func parseFetch(args []string, stderr io.Writer) (*fetchFlags, error) {
	fs := flag.NewFlagSet("publish fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f fetchFlags

	fs.StringVar(&f.forge, "forge", "", "the forge to read from: github or gitlab (required, never guessed)")
	fs.StringVar(&f.repo, "repo", "", "the project: GROUP/PROJECT or a numeric ID on GitLab, OWNER/REPO on GitHub "+
		"(required)")
	fs.StringVar(&f.commit, "commit", "", "the pushed commit to publish ($CI_COMMIT_SHA, github.sha); required")
	fs.StringVar(&f.apiURL, "api-url", "", "the REST API (default: github.com's or gitlab.com's)")
	fs.StringVar(&f.author, "author", "", "only the review comment by this login counts (default: the token's user "+
		"on GitLab, "+github.DefaultAuthor+" on GitHub)")
	fs.StringVar(&f.tokenEnv, "token-env", "", "the environment variable holding the token (default: GITHUB_TOKEN "+
		"or GITLAB_TOKEN)")
	fs.StringVar(&f.out, "out-file", "merge-request.json", "where the merge request and its review go")

	if err := fs.Parse(args); err != nil {
		return nil, errUsage
	}

	switch {
	case f.forge != forgeGitHub && f.forge != forgeGitLab:
		return nil, errForge
	case f.repo == "" || f.commit == "":
		return nil, errors.New("--repo and --commit are required")
	case strings.ContainsAny(f.commit, "/?#% "):
		return nil, errors.New("--commit: want a commit SHA")
	case fs.NArg() != 0:
		fmt.Fprint(stderr, usage)

		return nil, errUsage
	}

	apiURL, tokenEnv := forgeDefaults(f.forge)
	if f.forge == forgeGitHub && f.author == "" {
		f.author = github.DefaultAuthor
	}

	if f.apiURL == "" {
		f.apiURL = apiURL
	}

	if f.tokenEnv == "" {
		f.tokenEnv = tokenEnv
	}

	return &f, nil
}
