// Package playground is the local part of publishing a self-hosted playground: it writes the site.json that sets the
// core's preflight page up for one catalog (the flags its CI checks with, its schedules, and the forge pages that
// propose a schedule). It touches neither the network nor the file system; the command reads the inputs, and the
// GitHub Action or GitLab component downloads the page and publishes it.
package playground

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	cs "github.com/iilei/canned-schedules"
)

// Forges.
const (
	GitHub = "github"
	GitLab = "gitlab"
)

// Default servers, when the forge is the public one.
const (
	DefaultGitHubServer = "https://github.com"
	DefaultGitLabServer = "https://gitlab.com"
)

// Suffix ends a schedule file in a catalog.
const Suffix = ".canned-schedule.md"

var (
	errForge   = errors.New("want github or gitlab")
	errProject = errors.New("want OWNER/REPO on GitHub, GROUP/PROJECT (subgroups allowed) on GitLab")
	errServer  = errors.New("want an https URL with a host and nothing after it, e.g. https://gitlab.example.com " +
		"(http only for localhost)")
	errBranch = errors.New("want a branch name")
	errSite   = errors.New("the core refuses the site.json")
)

// Options describe the catalog a playground is set up for.
type Options struct {
	// Forge is GitHub or GitLab.
	Forge string
	// Server is the forge's web address; empty means the public one.
	Server string
	// Project is OWNER/REPO on GitHub, GROUP/PROJECT on GitLab.
	Project string
	// Branch is where proposals go, usually the default branch.
	Branch string
	// CatalogDir is the catalog's folder in the repository; empty means schedules.
	CatalogDir string
	// Name is shown in the page's header; optional.
	Name string
	// Args are the flags the catalog's CI checks with (canned-schedules.args).
	Args []string
	// Names are the schedules the catalog has, as slugs (file names without Suffix).
	Names []string
	// Fork adds a link for readers who aren't members (GitLab's new-file and edit pages need a fork for them).
	Fork bool
}

// site is written as site.json; its field order is the file's key order, kept readable on purpose.
type site struct { //nolint:govet // fieldalignment: the key order of site.json.
	Name    string   `json:"name,omitempty"`
	Args    []string `json:"args"`
	Catalog catalog  `json:"catalog"`
	Propose propose  `json:"propose"`
}

type catalog struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

// propose is written in the order a reader looks for the targets.
type propose struct {
	Create string `json:"create"`
	Update any    `json:"update"`
	Issue  string `json:"issue"`
	Fork   string `json:"fork,omitempty"`
}

// SiteJSON writes site.json for o and checks it with the core (cannedschedules.CheckSite), so a playground never
// ships a setup its page would ignore.
func SiteJSON(o *Options) ([]byte, error) {
	base, err := projectURL(o)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(o.Branch) == "" || strings.ContainsAny(o.Branch, " \t\r\n") {
		return nil, fmt.Errorf("--branch %q: %w", o.Branch, errBranch)
	}

	branch := escapePath(o.Branch)

	s := site{Name: o.Name, Args: o.Args, Catalog: catalog{Dir: o.CatalogDir, Names: o.Names}}
	if s.Args == nil {
		s.Args = []string{}
	}

	if s.Catalog.Dir == "" {
		s.Catalog.Dir = "schedules"
	}

	if s.Catalog.Names == nil {
		s.Catalog.Names = []string{}
	}

	switch o.Forge {
	case GitLab:
		// GitLab's edit page drops ?content=: its editor loads the stored file through the API. So the page
		// copies the schedule and opens the edit page, as on GitHub.
		s.Propose = propose{
			Create: base + "/-/new/" + branch + "?file_name={path}&content={content}",
			Update: map[string]any{"url": base + "/-/edit/" + branch + "/{path}", "prefill": false},
			Issue:  base + "/-/issues/new?issue[title]={name}&issue[description]={content}",
		}

		if o.Fork {
			s.Propose.Fork = base + "/-/forks/new"
		}
	default:
		// GitHub's edit page takes no content: the page copies the schedule and opens it.
		s.Propose = propose{
			Create: base + "/new/" + branch + "?filename={path}&value={content}",
			Update: map[string]any{"url": base + "/edit/" + branch + "/{path}", "prefill": false},
			Issue:  base + "/issues/new?title={name}&body={content}",
		}

		if o.Fork {
			s.Propose.Fork = base + "/fork"
		}
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // Keeps & and the placeholders readable.
	enc.SetIndent("", "  ")

	if err := enc.Encode(s); err != nil {
		return nil, err
	}

	if problems := cs.CheckSite(buf.Bytes()); len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", errSite, strings.Join(problems, "; "))
	}

	return buf.Bytes(), nil
}

// projectURL is the project's web address on its forge.
func projectURL(o *Options) (string, error) {
	server, err := serverURL(o.Forge, o.Server)
	if err != nil {
		return "", err
	}

	if !validProject(o.Forge, o.Project) {
		return "", fmt.Errorf("--project %q: %w", o.Project, errProject)
	}

	return server + "/" + escapePath(o.Project), nil
}

// serverURL checks the forge's web address, or picks the public one.
func serverURL(forge, server string) (string, error) {
	if server == "" {
		switch forge {
		case GitHub:
			server = DefaultGitHubServer
		case GitLab:
			server = DefaultGitLabServer
		}
	}

	if forge != GitHub && forge != GitLab {
		return "", fmt.Errorf("--forge %q: %w", forge, errForge)
	}

	u, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || !loopback(u.Hostname()))) {
		return "", fmt.Errorf("--server-url %q: %w", server, errServer)
	}

	return strings.TrimRight(u.String(), "/"), nil
}

// loopback reports whether host is this machine (localhost, *.localhost, 127.0.0.0/8, ::1), where the core allows
// http proposal targets: a forge in Docker, such as a local GitLab demo.
func loopback(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// validProject accepts OWNER/REPO on GitHub and GROUP/…/PROJECT on GitLab, without empty or dot segments.
func validProject(forge, project string) bool {
	parts := strings.Split(project, "/")
	if len(parts) < 2 || (forge == GitHub && len(parts) != 2) || strings.ContainsAny(project, " ?#{}\\") {
		return false
	}

	return !slices.Contains(parts, "") && !slices.Contains(parts, "..") && !slices.Contains(parts, ".")
}

// escapePath escapes each segment of a slash-separated path, keeping the slashes.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}

	return strings.Join(parts, "/")
}

// ReadArgs reads the args file format of canned-schedules.args: one argument per line, blank lines and lines
// starting with # skipped, CRLF read as LF.
func ReadArgs(text string) []string {
	var args []string

	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		args = append(args, line)
	}

	return args
}

// Names returns the slugs of the schedule files among file names, sorted: the names that end in Suffix, without it.
func Names(files []string) []string {
	names := []string{}

	for _, f := range files {
		if slug, ok := strings.CutSuffix(f, Suffix); ok && slug != "" {
			names = append(names, slug)
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}
