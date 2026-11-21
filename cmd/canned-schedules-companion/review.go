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
	"github.com/iilei/canned-schedules-companion/internal/forge/gitlab"
	"github.com/iilei/canned-schedules-companion/internal/preview"
	"github.com/iilei/canned-schedules-companion/internal/review"
)

// reviewJSON is what review render leaves for review post besides the comment.
const reviewJSON = "review.json"

var errNotYet = errors.New("--forge github: the merge request review supports GitLab for now")

type reviewFlags struct {
	forge, repo, apiURL, author, tokenEnv, label, headSHA string
	mergeRequest                                          int
	retitle, dryRun                                       bool
}

type reviewSummary struct {
	Names []string `json:"names"`
	Valid bool     `json:"valid"`
}

// reviewRender checks the schedule files a merge request changes and writes one comment for it.
func reviewRender(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review render", flag.ContinueOnError)
	fs.SetOutput(stderr)

	ev := addEvalFlags(fs)
	outDir := fs.String("out-dir", "canned-schedule-review", "where the files go")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	opts, err := ev.options()
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	files := make([]review.File, 0, fs.NArg())

	for _, name := range fs.Args() {
		src, err := readInput(name)
		if err != nil {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)

			return exitUsage
		}

		files = append(files, review.File{Path: filepath.ToSlash(name), Src: src})
	}

	res, err := review.Render(files, opts, nil, "canned-schedules-companion@"+ownVersion())
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	summary, err := json.Marshal(reviewSummary{Names: append([]string{}, res.Names...), Valid: res.Valid})
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	if err := os.MkdirAll(*outDir, dirMode); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	for name, data := range map[string][]byte{preview.Comment: res.Comment, reviewJSON: append(summary, '\n')} {
		if err := writeFile(filepath.Join(*outDir, name), data); err != nil {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)

			return exitUsage
		}
	}

	fmt.Fprintf(stdout, "wrote %s/%s for %d file(s)\n", *outDir, preview.Comment, len(files))

	if !res.Valid {
		return exitInvalid
	}

	return exitOK
}

// reviewPost keeps one review note on a merge request up to date, and its label and title.
func reviewPost(args []string, stdout, stderr io.Writer) int {
	f, dir, err := parseReviewPost(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := postReview(f, dir, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	return exitOK
}

func postReview(f *reviewFlags, dir string, stdout io.Writer) error {
	raw, err := os.ReadFile(filepath.Join(dir, preview.Comment)) //nolint:gosec // The directory is the caller's.
	if err != nil {
		return fmt.Errorf("read the comment (run review render first): %w", err)
	}

	stamp, ok := preview.ParseStampFor(review.Marker, string(raw))
	if !ok {
		return fmt.Errorf("%s has no review stamp: render it with this release's review render", preview.Comment)
	}

	var summary reviewSummary

	sraw, err := os.ReadFile(filepath.Join(dir, reviewJSON)) //nolint:gosec // The directory is the caller's.
	if err != nil || json.Unmarshal(sraw, &summary) != nil {
		return fmt.Errorf("read %s (run review render first)", reviewJSON)
	}

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := &gitlab.Client{
		HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token,
		UserAgent: "canned-schedules-companion/" + ownVersion(),
	}

	if f.author == "" {
		if f.author, err = c.CurrentUser(ctx); err != nil {
			return err
		}
	}

	mr, err := c.MergeRequest(ctx, f.repo, f.mergeRequest)
	if err != nil {
		return err
	}

	where := fmt.Sprintf("%s!%d", f.repo, f.mergeRequest)

	// A pipeline for an older commit must not overwrite what the pipeline for the newer one posts.
	if f.headSHA != "" && mr.SHA != f.headSHA {
		fmt.Fprintf(stdout, "%s has moved on to another commit; that commit's pipeline reviews it\n", where)

		return nil
	}

	if err := upsertReview(ctx, c, f, stamp, string(raw), stdout); err != nil {
		return err
	}

	if err := syncReviewLabel(ctx, c, f, mr, summary.Valid, stdout); err != nil {
		return err
	}

	return retitle(ctx, c, f, mr, summary, stdout)
}

func upsertReview(ctx context.Context, c *gitlab.Client, f *reviewFlags, stamp preview.Stamp, body string,
	stdout io.Writer,
) error {
	u := &forge.Upsert{
		Repo: f.repo, Issue: f.mergeRequest, MergeRequest: true, Marker: review.Marker, Author: f.author, DryRun: true,
	}

	plan, err := c.UpsertNote(ctx, u)
	if err != nil {
		return err
	}

	if old, ok := preview.ParseStampFor(review.Marker, plan.Existing); ok && old == stamp && len(plan.Delete) == 0 {
		fmt.Fprintf(stdout, "the review on %s!%d is up to date: %s\n", f.repo, f.mergeRequest, plan.URL)

		return nil
	}

	if len(body) > preview.GitLabMaxBody {
		return fmt.Errorf("the review has %d bytes, more than a note takes (%d)", len(body), preview.GitLabMaxBody)
	}

	u.Body = body

	if !f.dryRun {
		u.DryRun = false

		if plan, err = c.UpsertNote(ctx, u); err != nil {
			return err
		}
	}

	verb := would(f.dryRun)
	action := map[string]string{forge.ActionCreate: "create", forge.ActionUpdate: "update"}[plan.Action]

	if !f.dryRun {
		action = map[string]string{forge.ActionCreate: "created", forge.ActionUpdate: "updated"}[plan.Action]
	}

	fmt.Fprintf(stdout, "%s%s the review on %s!%d %s\n", verb, action, f.repo, f.mergeRequest, plan.URL)

	return nil
}

func syncReviewLabel(ctx context.Context, c *gitlab.Client, f *reviewFlags, mr *forge.MergeRequest, valid bool,
	stdout io.Writer,
) error {
	if f.label == "" {
		return nil
	}

	have := ""

	for _, l := range mr.Labels {
		if strings.EqualFold(l, f.label) {
			have = l
		}
	}

	verb := would(f.dryRun)

	switch {
	case valid && have == "":
		fmt.Fprintf(stdout, "%s%s the label %s to %s!%d\n", verb, pastOr(f.dryRun, "add", "added"), f.label, f.repo,
			f.mergeRequest)

		if !f.dryRun {
			return c.AddMergeRequestLabel(ctx, f.repo, f.mergeRequest, f.label)
		}
	case !valid && have != "":
		fmt.Fprintf(stdout, "%s%s the label %s from %s!%d\n", verb, pastOr(f.dryRun, "remove", "removed"), have,
			f.repo, f.mergeRequest)

		if !f.dryRun {
			return c.RemoveMergeRequestLabel(ctx, f.repo, f.mergeRequest, have)
		}
	}

	return nil
}

// retitle names the merge request after its schedules, but only while it has the title the forge's web editor gave
// it, so nothing a person wrote is overwritten.
func retitle(ctx context.Context, c *gitlab.Client, f *reviewFlags, mr *forge.MergeRequest, s reviewSummary,
	stdout io.Writer,
) error {
	title := review.Title(s.Names)
	if !f.retitle || !s.Valid || title == "" || title == mr.Title || !review.DefaultTitle(mr.Title) {
		return nil
	}

	verb := would(f.dryRun)
	fmt.Fprintf(stdout, "%s%s %s!%d to %q\n", verb, pastOr(f.dryRun, "rename", "renamed"), f.repo, f.mergeRequest,
		title)

	if f.dryRun {
		return nil
	}

	return c.SetMergeRequestTitle(ctx, f.repo, f.mergeRequest, title)
}

func parseReviewPost(args []string, stderr io.Writer) (*reviewFlags, string, error) {
	fs := flag.NewFlagSet("review post", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f reviewFlags

	fs.StringVar(&f.forge, "forge", "", "the forge to post to: gitlab (required, never guessed; github follows)")
	fs.StringVar(&f.repo, "repo", "", "the merge request's project: GROUP/PROJECT or a numeric ID (required)")
	fs.IntVar(&f.mergeRequest, "merge-request", 0, "the merge request's number (required)")
	fs.StringVar(&f.headSHA, "head-sha", "", "the commit this review is for; when the merge request has moved on, "+
		"nothing is written")
	fs.StringVar(&f.apiURL, "api-url", gitlab.DefaultAPIURL, "the REST API")
	fs.StringVar(
		&f.author,
		"author",
		"",
		"only notes by this login count as the step's own (default: the token's user)",
	)
	fs.StringVar(&f.tokenEnv, "token-env", "GITLAB_TOKEN", "the environment variable holding the token")
	fs.StringVar(
		&f.label,
		"label",
		"",
		"added when every schedule is valid, removed otherwise (empty: leave labels alone)",
	)
	fs.BoolVar(&f.retitle, "retitle", false, "name the merge request after its schedules while it has the web "+
		"editor's default title")
	fs.BoolVar(&f.dryRun, "dry-run", false, "read and say what would change, but write nothing")

	if err := fs.Parse(args); err != nil {
		return nil, "", errUsage
	}

	switch {
	case f.forge == forgeGitHub:
		return nil, "", errNotYet
	case f.forge != forgeGitLab:
		return nil, "", errForge
	case f.repo == "" || f.mergeRequest < 1:
		return nil, "", errors.New("--repo and --merge-request are required")
	case f.label != strings.TrimSpace(f.label) || strings.ContainsAny(f.label, "\r\n"):
		return nil, "", errors.New("--label: one line without leading or trailing spaces")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, "", errUsage
	}

	return &f, fs.Arg(0), nil
}
