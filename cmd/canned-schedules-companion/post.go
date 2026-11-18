package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/iilei/canned-schedules-companion/internal/forge"
	"github.com/iilei/canned-schedules-companion/internal/forge/github"
	"github.com/iilei/canned-schedules-companion/internal/forge/gitlab"
	"github.com/iilei/canned-schedules-companion/internal/preview"
)

const (
	forgeGitHub = "github"
	forgeGitLab = "gitlab"
)

var (
	errForge = errors.New("--forge: want github or gitlab")
	errToken = errors.New("no token")
)

// links collects repeatable --link values.
type links []preview.Link

type postFlags struct {
	forge, repo, apiURL, author, note, tokenEnv string
	links                                       links
	issue                                       int
	dryRun                                      bool
}

func (l *links) String() string { return fmt.Sprint(*l) }

func (l *links) Set(s string) error {
	link, err := preview.ParseLink(s)
	if err != nil {
		return err
	}

	*l = append(*l, link)

	return nil
}

// previewPost posts (or updates) the comment preview render wrote into dir. It is the only command that writes to
// a forge in step 1.
func previewPost(args []string, stdout, stderr io.Writer) int {
	f, dir, err := parsePost(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := post(f, dir, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	return exitOK
}

func post(f *postFlags, dir string, stdout io.Writer) error {
	raw, err := os.ReadFile(filepath.Join(dir, preview.Comment)) //nolint:gosec // The directory is the caller's.
	if err != nil {
		return fmt.Errorf("read the comment (run preview render first): %w", err)
	}

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var plan *forge.Plan

	if f.forge == forgeGitLab {
		plan, err = postGitLab(ctx, f, dir, raw, token, stdout)
	} else {
		plan, err = postGitHub(ctx, f, raw, token)
	}

	if err != nil {
		return err
	}

	report(stdout, f, plan)

	return nil
}

func postGitHub(ctx context.Context, f *postFlags, raw []byte, token string) (*forge.Plan, error) {
	body, err := preview.Body(raw, f.links, f.note, preview.GitHubMaxBody)
	if err != nil {
		return nil, err
	}

	c := &github.Client{
		HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token,
		UserAgent: "canned-schedules-companion/" + ownVersion(),
	}

	return c.UpsertComment(ctx, &forge.Upsert{
		Repo: f.repo, Issue: f.issue, Marker: preview.Marker, Author: f.author, Body: body, DryRun: f.dryRun,
	})
}

// postGitLab uploads the week, when the request has one, and shows it inline in the note.
func postGitLab(ctx context.Context, f *postFlags, dir string, raw []byte, token string,
	stdout io.Writer,
) (*forge.Plan, error) {
	c := &gitlab.Client{
		HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token,
		UserAgent: "canned-schedules-companion/" + ownVersion(),
	}

	author := f.author
	if author == "" {
		var err error
		if author, err = c.CurrentUser(ctx); err != nil {
			return nil, err
		}
	}

	svg, err := os.ReadFile(filepath.Join(dir, preview.ScheduleSVG)) //nolint:gosec // The directory is the caller's.
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read the week: %w", err)
	}

	var image string

	switch {
	case svg == nil:
	case f.dryRun:
		fmt.Fprintf(stdout, "would upload %s to %s\n", preview.ScheduleSVG, f.repo)
	default:
		if image, err = c.Upload(ctx, f.repo, preview.ScheduleSVG, svg); err != nil {
			return nil, err
		}
	}

	body, err := preview.GitLabBody(raw, image, f.links, f.note, preview.GitLabMaxBody)
	if err != nil {
		return nil, err
	}

	return c.UpsertNote(ctx, &forge.Upsert{
		Repo: f.repo, Issue: f.issue, Marker: preview.Marker, Author: author, Body: body, DryRun: f.dryRun,
	})
}

func report(w io.Writer, f *postFlags, p *forge.Plan) {
	verb := map[bool]string{true: "would ", false: ""}[f.dryRun]
	where := fmt.Sprintf("%s#%d", f.repo, f.issue)

	if p.Action == forge.ActionUpdate {
		fmt.Fprintf(w, "%s%s the comment on %s: %s\n", verb, pastOr(f.dryRun, "update", "updated"), where, p.URL)
	} else {
		fmt.Fprintf(w, "%s%s a comment on %s%s\n", verb, pastOr(f.dryRun, "create", "created"), where,
			strings.TrimRight(" "+p.URL, " "))
	}

	if len(p.Delete) > 0 {
		fmt.Fprintf(w, "%s%s %d duplicate comment(s) by %s\n", verb, pastOr(f.dryRun, "delete", "deleted"),
			len(p.Delete), f.author)
	}
}

func pastOr(dry bool, present, past string) string {
	if dry {
		return present
	}

	return past
}

func parsePost(args []string, stderr io.Writer) (*postFlags, string, error) {
	fs := flag.NewFlagSet("preview post", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f postFlags

	fs.StringVar(&f.forge, "forge", "", "the forge to post to: github or gitlab (required, never guessed)")
	fs.StringVar(&f.repo, "repo", "", "the issue's project: OWNER/REPO, on GitLab also a numeric ID (required)")
	fs.IntVar(&f.issue, "issue", 0, "the issue number (required)")
	fs.StringVar(
		&f.apiURL,
		"api-url",
		"",
		"the REST API (default: "+github.DefaultAPIURL+" or "+gitlab.DefaultAPIURL+")",
	)
	fs.StringVar(&f.author, "author", "", "only comments by this login count as the step's own (default: "+
		github.DefaultAuthor+" on GitHub, the token's user on GitLab)")
	fs.StringVar(&f.tokenEnv, "token-env", "", "the environment variable holding the token (default: GITHUB_TOKEN or "+
		"GITLAB_TOKEN)")
	fs.Var(&f.links, "link", "label=https://… shown under the comment (repeatable)")
	fs.StringVar(&f.note, "note", "", "text in parentheses after the links")
	fs.BoolVar(&f.dryRun, "dry-run", false, "read the issue's comments and say what would change, but write nothing")

	if err := fs.Parse(args); err != nil {
		return nil, "", errUsage
	}

	explicitAuthor := false

	fs.Visit(func(fl *flag.Flag) { explicitAuthor = explicitAuthor || fl.Name == "author" })

	switch {
	case f.forge != forgeGitHub && f.forge != forgeGitLab:
		return nil, "", errForge
	case f.repo == "" || f.issue < 1:
		return nil, "", errors.New("--repo and --issue are required")
	case explicitAuthor && strings.TrimSpace(f.author) == "":
		return nil, "", errors.New("--author can't be empty")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, "", errUsage
	}

	f.applyDefaults()

	return &f, fs.Arg(0), nil
}

// applyDefaults fills in what depends on the forge. GitLab's author stays empty: the token's user, asked at run time.
func (f *postFlags) applyDefaults() {
	apiURL, tokenEnv := github.DefaultAPIURL, "GITHUB_TOKEN" //nolint:gosec // The name of a variable, not a credential.
	if f.forge == forgeGitLab {
		apiURL, tokenEnv = gitlab.DefaultAPIURL, "GITLAB_TOKEN"
	} else if f.author == "" {
		f.author = github.DefaultAuthor
	}

	if f.apiURL == "" {
		f.apiURL = apiURL
	}

	if f.tokenEnv == "" {
		f.tokenEnv = tokenEnv
	}
}
