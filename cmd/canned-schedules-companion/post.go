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

	"github.com/iilei/canned-schedules-companion/internal/forge/github"
	"github.com/iilei/canned-schedules-companion/internal/preview"
)

const forgeGitHub = "github"

var (
	errForge = errors.New("--forge: want github (gitlab is not built yet)")
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

	body, err := preview.Body(raw, f.links, f.note, preview.GitHubMaxBody)
	if err != nil {
		return err
	}

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s (e.g. %s=$(gh auth token) locally)", errToken, f.tokenEnv, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := &github.Client{
		HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token,
		UserAgent: "canned-schedules-companion/" + ownVersion(),
	}

	plan, err := c.UpsertComment(ctx, &github.Upsert{
		Repo: f.repo, Issue: f.issue, Marker: preview.Marker, Author: f.author, Body: body, DryRun: f.dryRun,
	})
	if err != nil {
		return err
	}

	report(stdout, f, plan)

	return nil
}

func report(w io.Writer, f *postFlags, p *github.Plan) {
	verb := map[bool]string{true: "would ", false: ""}[f.dryRun]
	where := fmt.Sprintf("%s#%d", f.repo, f.issue)

	if p.Action == github.ActionUpdate {
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

	fs.StringVar(&f.forge, "forge", "", "the forge to post to: github (required, never guessed)")
	fs.StringVar(&f.repo, "repo", "", "OWNER/REPO of the issue (required)")
	fs.IntVar(&f.issue, "issue", 0, "the issue number (required)")
	fs.StringVar(&f.apiURL, "api-url", github.DefaultAPIURL, "the REST API (GitHub Enterprise Server has its own)")
	fs.StringVar(&f.author, "author", github.DefaultAuthor, "only comments by this login count as the step's own")
	fs.StringVar(&f.tokenEnv, "token-env", "GITHUB_TOKEN", "the environment variable holding the token")
	fs.Var(&f.links, "link", "label=https://… shown under the comment (repeatable)")
	fs.StringVar(&f.note, "note", "", "text in parentheses after the links")
	fs.BoolVar(&f.dryRun, "dry-run", false, "read the issue's comments and say what would change, but write nothing")

	if err := fs.Parse(args); err != nil {
		return nil, "", errUsage
	}

	switch {
	case f.forge != forgeGitHub:
		return nil, "", errForge
	case f.repo == "" || f.issue < 1:
		return nil, "", errors.New("--repo and --issue are required")
	case strings.TrimSpace(f.author) == "":
		return nil, "", errors.New("--author can't be empty")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, "", errUsage
	}

	return &f, fs.Arg(0), nil
}
