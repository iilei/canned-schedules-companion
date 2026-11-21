package playground_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	cs "github.com/iilei/canned-schedules"

	"github.com/iilei/canned-schedules-companion/internal/playground"
)

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	return m
}

func TestSiteJSONGitLab(t *testing.T) {
	t.Parallel()

	raw, err := playground.SiteJSON(&playground.Options{
		Forge: playground.GitLab, Server: "https://gitlab.example.com/", Project: "acme/ops/catalog",
		Branch: "release/main", Name: "Acme schedules", Args: []string{"--require-base", "true"},
		Names: []string{"office-hours"}, Fork: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if p := cs.CheckSite(raw); len(p) != 0 {
		t.Fatalf("the core refuses it: %q", p)
	}

	s := string(raw)
	for _, want := range []string{
		`"create": "https://gitlab.example.com/acme/ops/catalog/-/new/release/main?file_name={path}&content={content}"`,
		`"update": "https://gitlab.example.com/acme/ops/catalog/-/edit/release/main/{path}?content={content}"`,
		`"issue": "https://gitlab.example.com/acme/ops/catalog/-/issues/new?issue[title]={name}&issue[description]={content}"`,
		`"fork": "https://gitlab.example.com/acme/ops/catalog/-/forks/new"`,
		`"dir": "schedules"`, `"name": "Acme schedules"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
}

func TestSiteJSONGitHub(t *testing.T) {
	t.Parallel()

	raw, err := playground.SiteJSON(&playground.Options{Forge: playground.GitHub, Project: "o/r", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}

	m := decode(t, raw)
	p := m["propose"].(map[string]any)

	if p["create"] != "https://github.com/o/r/new/main?filename={path}&value={content}" ||
		p["issue"] != "https://github.com/o/r/issues/new?title={name}&body={content}" {
		t.Errorf("propose %v", p)
	}

	// GitHub's edit page takes no content: copy, then open.
	if u := p["update"].(map[string]any); u["prefill"] != false ||
		u["url"] != "https://github.com/o/r/edit/main/{path}" {
		t.Errorf("update %v", u)
	}

	if _, ok := p["fork"]; ok || len(m["args"].([]any)) != 0 ||
		len(m["catalog"].(map[string]any)["names"].([]any)) != 0 {
		t.Errorf("site %v", m)
	}
}

func TestSiteJSONRefuses(t *testing.T) {
	t.Parallel()

	ok := playground.Options{Forge: playground.GitLab, Project: "g/p", Branch: "main"}

	for name, change := range map[string]func(o *playground.Options){
		"forge":        func(o *playground.Options) { o.Forge = "forgejo" },
		"http":         func(o *playground.Options) { o.Server = "http://gitlab.example.com" },
		"server path?": func(o *playground.Options) { o.Server = "https://gitlab.example.com/?x=1" },
		"one segment":  func(o *playground.Options) { o.Project = "catalog" },
		"github sub":   func(o *playground.Options) { o.Forge, o.Project = playground.GitHub, "o/r/x" },
		"dot dot":      func(o *playground.Options) { o.Project = "g/../p" },
		"no branch":    func(o *playground.Options) { o.Branch = " " },
		"page flag":    func(o *playground.Options) { o.Args = []string{"--ascii-symbols", "braille"} },
		"bad name":     func(o *playground.Options) { o.Names = []string{"Office Hours"} },
	} {
		o := ok
		change(&o)

		if _, err := playground.SiteJSON(&o); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestReadArgs(t *testing.T) {
	t.Parallel()

	got := playground.ReadArgs("# comment\r\n--error-unless-alert-pattern\r\n(?i)NOTE|TIP\r\n\r\n--sample-interval\n15")
	if !slices.Equal(got, []string{"--error-unless-alert-pattern", "(?i)NOTE|TIP", "--sample-interval", "15"}) {
		t.Errorf("%q", got)
	}
}

func TestNames(t *testing.T) {
	t.Parallel()

	got := playground.Names([]string{
		"on-call.canned-schedule.md", "office-hours.canned-schedule.md", "office-hours.canned-schedule.json",
		"README.md", ".canned-schedule.md",
	})
	if !slices.Equal(got, []string{"office-hours", "on-call"}) {
		t.Errorf("%q", got)
	}
}
