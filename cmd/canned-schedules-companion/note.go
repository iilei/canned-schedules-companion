package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/iilei/canned-schedules-companion/internal/forge"
	"github.com/iilei/canned-schedules-companion/internal/forge/github"
	"github.com/iilei/canned-schedules-companion/internal/preview"
	"github.com/iilei/canned-schedules-companion/internal/publish"
)

type notePostFlags struct {
	forge, repo, apiURL, author, tokenEnv string
	dryRun                                bool
}

// publishPost keeps one note on the merged merge request that lists every schedule published without its reviewed
// digest. When every schedule was published as reviewed it writes nothing.
func publishPost(args []string, stdout, stderr io.Writer) int {
	f, dir, err := parsePublishPost(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := postNote(f, dir, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	return exitOK
}

func postNote(f *notePostFlags, dir string, stdout io.Writer) error {
	raw, err := os.ReadFile(filepath.Join(dir, publish.Manifest)) //nolint:gosec // The directory is the caller's.
	if err != nil {
		return fmt.Errorf("read the manifest (run publish prepare first): %w", err)
	}

	note, err := publish.NoteFor(raw)
	if err != nil {
		return err
	}

	sep := "!"
	if f.forge == forgeGitHub {
		sep = "#"
	}

	where := fmt.Sprintf("%s%s%d", f.repo, sep, note.MergeRequest)

	if note.Body == "" {
		fmt.Fprintf(stdout, "every schedule of %s was published as reviewed: no note\n", where)

		return nil
	}

	maxBody := preview.GitLabMaxBody
	if f.forge == forgeGitHub {
		maxBody = preview.GitHubMaxBody
	}

	if len(note.Body) > maxBody {
		return fmt.Errorf("the note has %d bytes, more than a comment takes (%d)", len(note.Body), maxBody)
	}

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := newMergeRequestClient(ctx, f.forge, f.apiURL, token, &f.author)
	if err != nil {
		return err
	}

	u := &forge.Upsert{
		Repo: f.repo, Issue: note.MergeRequest, MergeRequest: true, Marker: publish.NoteMarker, Author: f.author,
		DryRun: true,
	}

	plan, err := c.UpsertNote(ctx, u)
	if err != nil {
		return err
	}

	if plan.Existing == note.Body && len(plan.Delete) == 0 {
		fmt.Fprintf(stdout, "the note on %s is up to date: %s\n", where, plan.URL)

		return nil
	}

	u.Body = note.Body

	if !f.dryRun {
		u.DryRun = false

		if plan, err = c.UpsertNote(ctx, u); err != nil {
			return err
		}
	}

	action := map[string]string{forge.ActionCreate: "create", forge.ActionUpdate: "update"}[plan.Action]
	if !f.dryRun {
		action += "d"
	}

	fmt.Fprintf(stdout, "%s%s the note on %s for %d schedule(s) published without review %s\n", would(f.dryRun),
		action, where, note.Unreviewed, plan.URL)

	return nil
}

func parsePublishPost(args []string, stderr io.Writer) (*notePostFlags, string, error) {
	fs := flag.NewFlagSet("publish post", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f notePostFlags

	fs.StringVar(&f.forge, "forge", "", "the forge to post to: github or gitlab (required, never guessed)")
	fs.StringVar(&f.repo, "repo", "", "the project: GROUP/PROJECT or a numeric ID on GitLab, OWNER/REPO on GitHub "+
		"(required)")
	fs.StringVar(&f.apiURL, "api-url", "", "the REST API (default: github.com's or gitlab.com's)")
	fs.StringVar(&f.author, "author", "", "only notes by this login count as the step's own (default: the token's "+
		"user on GitLab, "+github.DefaultAuthor+" on GitHub)")
	fs.StringVar(&f.tokenEnv, "token-env", "", "the environment variable holding the token (default: GITHUB_TOKEN "+
		"or GITLAB_TOKEN)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "read and say what would change, but write nothing")

	if err := fs.Parse(args); err != nil {
		return nil, "", errUsage
	}

	switch {
	case f.forge != forgeGitHub && f.forge != forgeGitLab:
		return nil, "", errForge
	case f.repo == "":
		return nil, "", errors.New("--repo is required")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, "", errUsage
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

	return &f, fs.Arg(0), nil
}
