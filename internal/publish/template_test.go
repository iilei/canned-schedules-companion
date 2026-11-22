package publish_test

import (
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

func TestTemplate(t *testing.T) {
	t.Parallel()

	vars := map[string]string{}
	for _, v := range publish.Variables {
		vars[v] = ""
	}

	vars["host"] = "gitlab.example.com"
	vars["name"] = "Night Shift"
	vars["digest"] = "sha256:3f2a9c1b04de" + strings.Repeat("0", 52)

	for tmpl, want := range map[string]string{
		"{host | reverseByDotDelim}":              "com.example.gitlab",
		"{ name|toLower }":                        "night shift",
		"{name | toUpper | truncate 5}":           "NIGHT",
		"{digest | truncate 19}":                  "sha256:3f2a9c1b04de",
		"{name | truncate 99}":                    "Night Shift",
		"x-{host | toUpper | reverseByDotDelim}/": "x-COM.EXAMPLE.GITLAB/",
		"plain": "plain",
	} {
		p, err := publish.Parse(tmpl)
		if err != nil {
			t.Errorf("%q: %v", tmpl, err)

			continue
		}

		if got := p.Execute(vars); got != want {
			t.Errorf("%q: got %q, want %q", tmpl, got, want)
		}
	}

	for _, tmpl := range []string{
		"{nme}", "{name | lower}", "{name | truncate}", "{name | truncate 0}", "{name | truncate x}",
		"{name | toLower 2}", "{name |}", "{name", "name}", "{{name}}", "{name | truncate 1 2}",
	} {
		if _, err := publish.Parse(tmpl); err == nil {
			t.Errorf("%q: want an error", tmpl)
		}
	}
}

func TestMetadata(t *testing.T) {
	t.Parallel()

	vars := map[string]string{}
	for _, v := range publish.Variables {
		vars[v] = "v"
	}

	vars["title"] = "a;b,c=d e"

	m, err := publish.ParseMetadata("# a comment\n\ntitle={title}\r\n{slug}-key = x\n")
	if err != nil {
		t.Fatal(err)
	}

	pairs, err := m.Execute(vars)
	if err != nil {
		t.Fatal(err)
	}

	if len(pairs) != 2 || pairs[0] != (publish.Pair{Key: "title", Value: "a;b,c=d e"}) ||
		pairs[1] != (publish.Pair{Key: "v-key", Value: " x"}) {
		t.Errorf("pairs %+v", pairs)
	}

	if got := publish.Artifactory(pairs); got != ";title=a%3Bb%2Cc%3Dd%20e;v-key=%20x" {
		t.Errorf("artifactory %q", got)
	}

	if _, err := publish.ParseMetadata("no equals sign"); err == nil {
		t.Error("a line without = : want an error")
	}

	for _, text := range []string{"Upper=x", "a=1\na=2", "bad key=x", "t={title}x\u0007"} {
		m, err := publish.ParseMetadata(text)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := m.Execute(vars); err == nil {
			t.Errorf("%q: want an error", text)
		}
	}

	if _, ok := publish.S3([]publish.Pair{{Key: "k", Value: strings.Repeat("x", 3000)}}); ok {
		t.Error("over 2 KB: want false")
	}
}
