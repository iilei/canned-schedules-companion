package main

import (
	"bytes"
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
	"strconv"
	"strings"

	"github.com/iilei/canned-schedules-companion/internal/forge"
	"github.com/iilei/canned-schedules-companion/internal/forge/github"
	"github.com/iilei/canned-schedules-companion/internal/forge/gitlab"
	"github.com/iilei/canned-schedules-companion/internal/promote"
)

// defaultBranchPrefix starts the branch a promotion commits to; the issue number follows.
const defaultBranchPrefix = "canned-schedules/issue-"

var errInvalidSchedule = errors.New("the schedule has errors")

// promoteForge is what promote open needs from a forge.
type promoteForge interface {
	BranchExists(ctx context.Context, repo, branch string) (bool, error)
	File(ctx context.Context, repo, ref, file string) (*forge.File, error)
	CommitFile(ctx context.Context, cm *forge.Commit) error
	OpenRequest(ctx context.Context, r *forge.Request, dryRun bool) (string, bool, error)
	RemoveLabel(ctx context.Context, repo string, issue int, label string) error
}

type promoteFlags struct {
	forge, repo, apiURL, tokenEnv, base, branchPrefix, approvedLabel string
	issue                                                            int
	dryRun                                                           bool
}

// promotePrepare checks an approved issue's text and writes the catalog file it becomes.
func promotePrepare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("promote prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)

	ev := addEvalFlags(fs)
	outDir := fs.String("out-dir", "canned-schedule-promotion", "where the files go")
	catalogDir := fs.String("catalog-dir", "schedules", "the catalog's folder in the repository")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	if fs.NArg() != 1 {
		fmt.Fprint(stderr, usage)

		return exitUsage
	}

	opts, err := ev.options()
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	src, err := readInput(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	res, err := promote.Prepare(src, filepath.Base(fs.Arg(0)), opts, *catalogDir)
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	summary, err := json.Marshal(res)
	if err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	files := map[string][]byte{promote.JSON: append(summary, '\n'), promote.Report: res.Report}
	if res.Valid {
		files[promote.Schedule] = res.Content
	}

	if err := os.RemoveAll(filepath.Join(*outDir, promote.Schedule)); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	if err := writeAll(*outDir, files); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		return exitUsage
	}

	if !res.Valid {
		fmt.Fprintf(stdout, "the text holds no valid schedule; see %s/%s\n", *outDir, promote.Report)

		return exitInvalid
	}

	fmt.Fprintf(stdout, "wrote %s/%s for %s\n", *outDir, promote.Schedule, res.Path)

	return exitOK
}

// promoteOpen commits the prepared file to a branch and opens a pull or merge request for it, once.
func promoteOpen(args []string, stdout, stderr io.Writer) int {
	f, dir, err := parsePromoteOpen(args, stderr)
	if err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(stderr, "canned-schedules-companion:", err)
		}

		return exitUsage
	}

	if err := openPromotion(f, dir, stdout); err != nil {
		fmt.Fprintln(stderr, "canned-schedules-companion:", err)

		if errors.Is(err, errInvalidSchedule) {
			return exitInvalid
		}

		return exitUsage
	}

	return exitOK
}

func openPromotion(f *promoteFlags, dir string, stdout io.Writer) error {
	var res promote.Result

	raw, err := os.ReadFile(filepath.Join(dir, promote.JSON)) //nolint:gosec // The directory is the caller's.
	if err != nil || json.Unmarshal(raw, &res) != nil {
		return fmt.Errorf("read %s (run promote prepare first)", promote.JSON)
	}

	token := os.Getenv(f.tokenEnv)
	if token == "" {
		return fmt.Errorf("%w: set %s", errToken, f.tokenEnv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c := f.client(token)

	if !res.Valid {
		return f.takeBack(ctx, c, stdout)
	}

	content, err := os.ReadFile(filepath.Join(dir, promote.Schedule)) //nolint:gosec // The directory is the caller's.
	if err != nil {
		return fmt.Errorf("read %s (run promote prepare first): %w", promote.Schedule, err)
	}

	done, err := f.commit(ctx, c, &res, content, stdout)
	if err != nil || done {
		return err
	}

	return f.request(ctx, c, &res, stdout)
}

// takeBack removes the approval of a text that isn't (or no longer is) a valid schedule, so the label never stands
// for something CI rejects; the issue preview's comment says what is wrong.
func (f *promoteFlags) takeBack(ctx context.Context, c promoteForge, stdout io.Writer) error {
	if f.approvedLabel != "" {
		fmt.Fprintf(stdout, "%s%s the label %s from %s\n", would(f.dryRun), pastOr(f.dryRun, "remove", "removed"),
			f.approvedLabel, f.where())

		if !f.dryRun {
			if err := c.RemoveLabel(ctx, f.repo, f.issue, f.approvedLabel); err != nil {
				return err
			}
		}
	}

	return fmt.Errorf("%w: %s can't be promoted (see %s)", errInvalidSchedule, f.where(), promote.Report)
}

// commit writes the file to the promotion's branch unless it already holds the text. done means the base branch
// holds it already, so no request is needed.
func (f *promoteFlags) commit(ctx context.Context, c promoteForge, res *promote.Result, content []byte,
	stdout io.Writer,
) (bool, error) {
	branch := f.branch()

	exists, err := c.BranchExists(ctx, f.repo, branch)
	if err != nil {
		return false, err
	}

	ref := f.base
	if exists {
		ref = branch
	}

	current, err := c.File(ctx, f.repo, ref, res.Path)
	if err != nil {
		return false, err
	}

	if current != nil && bytes.Equal(current.Content, content) {
		if !exists {
			fmt.Fprintf(stdout, "%s on %s already holds this text: nothing to promote\n", res.Path, f.base)

			return true, nil
		}

		fmt.Fprintf(stdout, "%s on %s is up to date\n", res.Path, branch)

		return false, nil
	}

	action := pastOr(f.dryRun, "add", "added")
	if current != nil {
		action = pastOr(f.dryRun, "update", "updated")
	}

	fmt.Fprintf(stdout, "%s%s %s on %s\n", would(f.dryRun), action, res.Path, branch)

	if f.dryRun {
		return false, nil
	}

	cm := &forge.Commit{
		Repo: f.repo, Branch: branch, Path: res.Path, Content: content, Replace: current,
		Message: fmt.Sprintf("%s\n\nProposed in %s.", res.Title, f.issueRef()),
	}
	if !exists {
		cm.From = f.base
	}

	return false, c.CommitFile(ctx, cm)
}

// request opens the pull or merge request from the promotion's branch, or names the one that is open.
func (f *promoteFlags) request(ctx context.Context, c promoteForge, res *promote.Result, stdout io.Writer) error {
	branch := f.branch()

	url, opened, err := c.OpenRequest(ctx, &forge.Request{
		Repo: f.repo, Branch: branch, Base: f.base, Title: res.Title,
		Body: fmt.Sprintf("Closes %s.\n\nThe schedule as proposed and approved in %s, checked with the catalog's "+
			"flags. Merge it to add it to the catalog.\n", f.issueRef(), f.issueRef()),
	}, f.dryRun)
	if err != nil {
		return err
	}

	if !opened {
		fmt.Fprintf(stdout, "the request from %s is open: %s\n", branch, url)

		return nil
	}

	fmt.Fprintf(stdout, "%s%s a request from %s to %s %s\n", would(f.dryRun), pastOr(f.dryRun, "open", "opened"),
		branch, f.base, url)

	return nil
}

// branch is where the promotion commits: the prefix and the issue number.
func (f *promoteFlags) branch() string { return f.branchPrefix + strconv.Itoa(f.issue) }

func (f *promoteFlags) client(token string) promoteForge {
	agent := "canned-schedules-companion/" + ownVersion()
	if f.forge == forgeGitHub {
		return &github.Client{HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token, UserAgent: agent}
	}

	return &gitlab.Client{HTTP: http.DefaultClient, BaseURL: f.apiURL, Token: token, UserAgent: agent}
}

// where names the issue as the forge writes it in a log.
func (f *promoteFlags) where() string { return f.repo + f.issueRef() }

// issueRef is the issue's reference inside its project: #12 on both forges.
func (f *promoteFlags) issueRef() string { return "#" + strconv.Itoa(f.issue) }

func parsePromoteOpen(args []string, stderr io.Writer) (*promoteFlags, string, error) {
	fs := flag.NewFlagSet("promote open", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f promoteFlags

	fs.StringVar(&f.forge, "forge", "", "the forge: github or gitlab (required, never guessed)")
	fs.StringVar(&f.repo, "repo", "", "the project: OWNER/REPO on GitHub, GROUP/PROJECT or a numeric ID on GitLab "+
		"(required)")
	fs.IntVar(&f.issue, "issue", 0, "the approved issue's number (required)")
	fs.StringVar(&f.base, "base-branch", "", "the branch the request targets, usually the default branch (required)")
	fs.StringVar(&f.branchPrefix, "branch-prefix", defaultBranchPrefix, "the branch name before the issue number")
	fs.StringVar(&f.approvedLabel, "approved-label", "", "removed from the issue when its schedule has errors")
	fs.StringVar(&f.apiURL, "api-url", "", "the REST API (default: github.com's or gitlab.com's)")
	fs.StringVar(&f.tokenEnv, "token-env", "", "the environment variable holding the token (default: GITHUB_TOKEN "+
		"or GITLAB_TOKEN)")
	fs.BoolVar(&f.dryRun, "dry-run", false, "read and say what would change, but write nothing")

	if err := fs.Parse(args); err != nil {
		return nil, "", errUsage
	}

	switch {
	case f.forge != forgeGitHub && f.forge != forgeGitLab:
		return nil, "", errForge
	case f.repo == "" || f.issue < 1 || f.base == "":
		return nil, "", errors.New("--repo, --issue and --base-branch are required")
	case strings.ContainsAny(f.base+f.branchPrefix, " \t\r\n~^:?*[\\") || strings.Contains(f.branchPrefix, ".."):
		return nil, "", errors.New("--base-branch and --branch-prefix must be valid branch names")
	case f.approvedLabel != strings.TrimSpace(f.approvedLabel) || strings.ContainsAny(f.approvedLabel, "\r\n"):
		return nil, "", errors.New("--approved-label: one line without leading or trailing spaces")
	case fs.NArg() != 1:
		fmt.Fprint(stderr, usage)

		return nil, "", errUsage
	}

	apiURL, tokenEnv := forgeDefaults(f.forge)

	if f.apiURL == "" {
		f.apiURL = apiURL
	}

	if f.tokenEnv == "" {
		f.tokenEnv = tokenEnv
	}

	return &f, fs.Arg(0), nil
}
