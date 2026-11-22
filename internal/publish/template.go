package publish

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The helpers' names.
const (
	helperLower    = "toLower"
	helperUpper    = "toUpper"
	helperReverse  = "reverseByDotDelim"
	helperTruncate = "truncate"
	helperAfter    = "afterLast"
)

// Variables are the names a template may use, in the order the docs list them. Every one is available in the path
// and in the metadata.
var Variables = []string{
	"name", "slug", "path", "digest", "reviewed",
	"mr", "title", "author", "merged-by", "created-at", "merged-at", "merged-date",
	"forge", "host", "project", "branch",
	"tool-version", "core-version",
}

// Helpers are the functions a placeholder may pipe its value through.
var Helpers = []string{helperLower, helperUpper, helperReverse, helperTruncate, helperAfter}

var errTemplate = errors.New("template")

// Template is a parsed path or metadata template: text with placeholders such as {slug} or
// {digest | truncate 19}, the same braces as site.json's URL templates.
type Template struct {
	parts []part
}

type part struct {
	literal string
	name    string
	helpers []helper
}

type helper struct {
	name string
	sep  string
	n    int
}

// Parse reads a template. Unknown variables and helpers are errors, so a typo can't turn into an empty value. {{ and
// }} stand for a literal { and }, so {{slug}} is the text {slug}.
func Parse(text string) (*Template, error) {
	t := &Template{}

	var lit strings.Builder

	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, part{literal: lit.String()})
			lit.Reset()
		}
	}

	for rest := text; rest != ""; {
		i := strings.IndexAny(rest, "{}")
		if i < 0 {
			lit.WriteString(rest)

			break
		}

		lit.WriteString(rest[:i])
		c, rest2 := rest[i], rest[i+1:]

		if strings.HasPrefix(rest2, string(c)) { // {{ or }}: the brace itself.
			lit.WriteByte(c)
			rest = rest2[1:]

			continue
		}

		if c == '}' {
			return nil, fmt.Errorf("%w %q: a } without a { before it (write }} for a literal })", errTemplate, text)
		}

		end := strings.IndexAny(rest2, "{}")
		if end < 0 || rest2[end] == '{' {
			return nil, fmt.Errorf("%w %q: a { without a } after it (write {{ for a literal {)", errTemplate, text)
		}

		p, err := placeholder(rest2[:end])
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errTemplate, text, err)
		}

		flush()
		t.parts = append(t.parts, p)
		rest = rest2[end+1:]
	}

	flush()

	return t, nil
}

// Execute fills the template. vars must hold every name in Variables.
func (t *Template) Execute(vars map[string]string) string {
	var b strings.Builder

	for _, p := range t.parts {
		if p.name == "" {
			b.WriteString(p.literal)

			continue
		}

		v := vars[p.name]
		for _, h := range p.helpers {
			v = h.apply(v)
		}

		b.WriteString(v)
	}

	return b.String()
}

func placeholder(s string) (part, error) {
	fields := strings.Split(s, "|")

	name := strings.TrimSpace(fields[0])
	if !slices.Contains(Variables, name) {
		return part{}, fmt.Errorf("unknown variable %q (known: %s)", name, strings.Join(Variables, ", "))
	}

	p := part{name: name}

	for _, f := range fields[1:] {
		h, err := parseHelper(strings.Fields(f))
		if err != nil {
			return part{}, err
		}

		p.helpers = append(p.helpers, h)
	}

	return p, nil
}

func parseHelper(words []string) (helper, error) {
	if len(words) == 0 {
		return helper{}, errors.New("an empty helper after |")
	}

	name, args := words[0], words[1:]

	switch name {
	case helperLower, helperUpper, helperReverse:
		if len(args) != 0 {
			return helper{}, fmt.Errorf("%s takes no argument", name)
		}

		return helper{name: name}, nil
	case helperTruncate:
		if len(args) != 1 {
			return helper{}, errors.New("truncate takes one number: truncate 12")
		}

		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return helper{}, fmt.Errorf("truncate wants a positive number, not %q", args[0])
		}

		return helper{name: name, n: n}, nil
	case helperAfter:
		// One quoted separator: the template's braces and pipes, and spaces, can't be part of it.
		if len(args) != 1 || len(args[0]) < 3 || args[0][0] != '"' || args[0][len(args[0])-1] != '"' ||
			strings.Contains(args[0][1:len(args[0])-1], `"`) {
			return helper{}, errors.New(`afterLast takes one quoted separator: afterLast ":"`)
		}

		return helper{name: name, sep: args[0][1 : len(args[0])-1]}, nil
	}

	return helper{}, fmt.Errorf("unknown helper %q (known: %s)", name, strings.Join(Helpers, ", "))
}

func (h helper) apply(v string) string {
	switch h.name {
	case helperLower:
		return strings.ToLower(v)
	case helperUpper:
		return strings.ToUpper(v)
	case helperReverse:
		parts := strings.Split(v, ".")
		slices.Reverse(parts)

		return strings.Join(parts, ".")
	case helperAfter: // what follows the last separator; the whole value without one, like ${v##*sep}.
		if i := strings.LastIndex(v, h.sep); i >= 0 {
			return v[i+len(h.sep):]
		}

		return v
	default: // truncate: the first n characters, not bytes.
		if utf8.RuneCountInString(v) <= h.n {
			return v
		}

		return string([]rune(v)[:h.n])
	}
}
