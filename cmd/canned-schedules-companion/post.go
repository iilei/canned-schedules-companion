package main

import (
	"context"
	"encoding/json"
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
	forge, repo, apiURL, author, note, tokenEnv, label string
	links                                              links
	issue                                              int
	dryRun                                             bool
}

// client is what post needs from a forge besides the comment itself.
type client interface {
	Issue(ctx context.Context, repo string, issue int) (*forge.Issue, error)
	AddLabel(ctx context.Context, repo string, issue int, label string) error
	RemoveLabel(ctx context.Context, repo string, issue int, label string) error
}

// target is one forge: its client, how it keeps the comment up to date, and how it builds the comment's body (which
// may upload files, so it runs only when the comment changes).
type target struct {
	client client
	upsert func(ctx context.Context, u *forge.Upsert) (*forge.Plan, error)
	body   func(ctx context.Context) (string, error)
	author string
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

	stamp, ok := preview.ParseStamp(string(raw))
	if !ok {
		return fmt.Errorf("%s has no stamp: render it with this release's preview render", preview.Comment)
	}

	valid, err := readValid(dir)
	if err != nil {
		return err
	}

	// Text without a schedule is never valid, whatever the report says (a heading and prose make an empty one).
	none := preview.IsNoSchedule(raw)
	valid = valid && !none

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var t *target

	if f.forge == forgeGitLab {
		t, err = gitLabTarget(ctx, f, dir, raw, token, stdout)
	} else {
		t = gitHubTarget(f, raw, token)
	}

	if err != nil {
		return err
	}

	issue, err := t.client.Issue(ctx, f.repo, f.issue)
	if err != nil {
		return err
	}

	// A run that checked an older text must not overwrite what the run for the newer one posts.
	if preview.BodyDigest([]byte(issue.Body)) != stamp.Body {
		fmt.Fprintf(stdout, "the text of %s#%d changed since the check; the run for that change posts instead\n",
			f.repo, f.issue)

		return nil
	}

	if err := upsert(ctx, f, t, stamp, none, stdout); err != nil {
		return err
	}

	return syncLabel(ctx, f, t.client, issue, valid, stdout)
}

// upsert leaves a comment with the same stamp alone: the same text, tool and options give the same comment. A
// comment without a stamp (earlier releases) or with another one is rendered again. Text without a schedule (none)
// replaces an earlier comment but never starts one, so the bot stays silent on issues that aren't requests.
func upsert(ctx context.Context, f *postFlags, t *target, stamp preview.Stamp, none bool, stdout io.Writer) error {
	u := &forge.Upsert{Repo: f.repo, Issue: f.issue, Marker: preview.Marker, Author: t.author, DryRun: true}

	plan, err := t.upsert(ctx, u)
	if err != nil {
		return err
	}

	if none && plan.Action == forge.ActionCreate {
		fmt.Fprintf(stdout, "%s#%d holds no schedule and has no comment from this step, so there is nothing to post\n",
			f.repo, f.issue)

		return nil
	}

	if old, ok := preview.ParseStamp(plan.Existing); ok && old == stamp && len(plan.Delete) == 0 {
		fmt.Fprintf(stdout, "the comment on %s#%d is up to date: %s\n", f.repo, f.issue, plan.URL)

		return nil
	}

	if u.Body, err = t.body(ctx); err != nil {
		return err
	}

	if !f.dryRun {
		u.DryRun = false

		if plan, err = t.upsert(ctx, u); err != nil {
			return err
		}
	}

	report(stdout, f, t.author, plan)

	return nil
}

// syncLabel makes the label say whether the request is valid: added when it is, removed when it isn't. CI is the
// only one that sets it, so it can serve as "holds a valid schedule".
func syncLabel(ctx context.Context, f *postFlags, c client, issue *forge.Issue, valid bool, stdout io.Writer) error {
	if f.label == "" {
		return nil
	}

	// GitHub matches label names ignoring case; the name as the issue has it is the one to remove.
	have := ""

	for _, l := range issue.Labels {
		if strings.EqualFold(l, f.label) {
			have = l
		}
	}

	verb := map[bool]string{true: "would ", false: ""}[f.dryRun]
	where := fmt.Sprintf("%s#%d", f.repo, f.issue)

	switch {
	case valid && have == "":
		fmt.Fprintf(stdout, "%s%s the label %s to %s\n", verb, pastOr(f.dryRun, "add", "added"), f.label, where)

		if !f.dryRun {
			return c.AddLabel(ctx, f.repo, f.issue, f.label)
		}
	case !valid && have != "":
		fmt.Fprintf(stdout, "%s%s the label %s from %s\n", verb, pastOr(f.dryRun, "remove", "removed"), have, where)

		if !f.dryRun {
			return c.RemoveLabel(ctx, f.repo, f.issue, have)
		}
	}

	return nil
}

// readValid reads whether the request is valid from the JSON report preview render wrote.
func readValid(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, preview.ReportJSON)) //nolint:gosec // The directory is the caller's.
	if err != nil {
		return false, fmt.Errorf("read the report (run preview render first): %w", err)
	}

	var rep struct {
		Summary *struct {
			Errors *int `json:"errors"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(raw, &rep); err != nil || rep.Summary == nil || rep.Summary.Errors == nil {
		return false, fmt.Errorf("%s has no summary.errors: not written by preview render", preview.ReportJSON)
	}

	return *rep.Summary.Errors == 0, nil
}

func gitHubTarget(f *postFlags, raw []byte, token string) *target {
	c := &github.Client{
		HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token,
		UserAgent: "canned-schedules-companion/" + ownVersion(),
	}

	return &target{
		client: c,
		upsert: c.UpsertComment,
		body: func(context.Context) (string, error) {
			return preview.Body(raw, f.links, f.note, preview.GitHubMaxBody)
		},
		author: f.author,
	}
}

// gitLabTarget uploads the week, when the request has one, and shows it inline in the note.
func gitLabTarget(ctx context.Context, f *postFlags, dir string, raw []byte, token string,
	stdout io.Writer,
) (*target, error) {
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

	body := func(ctx context.Context) (string, error) {
		var image string

		switch {
		case svg == nil:
		case f.dryRun:
			fmt.Fprintf(stdout, "would upload %s to %s\n", preview.UploadName(f.issue, svg), f.repo)
		default:
			var err error
			if image, err = c.Upload(ctx, f.repo, preview.UploadName(f.issue, svg), svg); err != nil {
				return "", err
			}
		}

		return preview.GitLabBody(raw, image, f.links, f.note, preview.GitLabMaxBody)
	}

	return &target{client: c, upsert: c.UpsertNote, body: body, author: author}, nil
}

func report(w io.Writer, f *postFlags, author string, p *forge.Plan) {
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
			len(p.Delete), author)
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
	fs.StringVar(&f.label, "label", "", "added to the issue when the request is valid, removed when it isn't "+
		"(empty: leave labels alone)")
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
	case f.label != strings.TrimSpace(f.label) || strings.ContainsAny(f.label, "\r\n"):
		return nil, "", errors.New("--label: one line without leading or trailing spaces")
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
