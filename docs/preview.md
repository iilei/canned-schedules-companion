# Issue preview

## preview render

```sh
canned-schedules-companion preview render [flags] request.md
```

Checks one proposal, for example an issue body saved to a file, and writes into `--out-dir` (default
`canned-schedule-preview`):

| File            | Content                                                                   |
| --------------- | ------------------------------------------------------------------------- |
| `report.md`     | the error report (GitHub-flavoured Markdown, user text inside fences)     |
| `report.json`   | the same as JSON (canned-schedules' report schema)                        |
| `comment.md`    | the comment to post: a marker line, the report and, if valid, the preview |
| `schedule.json` | the canonical output (valid schedules only)                               |
| `schedule.txt`  | the text form: the canonical schedule with the ASCII week (valid only)    |
| `schedule.svg`  | the SVG week, following the viewer's light or dark theme (valid only)     |

Files of an earlier run in the same directory are removed first, so a schedule that became invalid leaves no stale
preview. The comment starts with `<!-- canned-schedules:issue-preview -->`, by which a forge step finds and updates
its earlier comment.

### Playground link (opt-in)

By default, everything stays in CI and on the forge: the comment carries the report and the text form, and the
files go where your workflow puts them. Teams that host the canned-schedules preflight page themselves (built with
the core repo's `mise run web:build`, served from their own GitHub Pages or intranet) can opt in:

```sh
canned-schedules-companion preview render \
  --fiddle-link-base-uri https://schedules.example.com/preflight/ request.md
```

The comment then ends with "Try this schedule in the playground and see its week", a link to that page with the
schedule in the URL fragment (`#doc=`, base64url): the canonical form of a valid schedule, the schedule as written
otherwise, so the playground shows the errors at their lines. Browsers never send the fragment to the server, so
the schedule stays in the reader's browser even on your own host. The base must be `https` without a fragment; a
query is kept (e.g. `?theme=dark`). A link longer than 32 KiB is left out with a notice.

The link also carries the run's options, so the playground checks the schedule as the run did:
`?sample-interval=` and `?downsample=` (they replace the base's), and the other flags as `?arg=`, one argument
each (`--require-base`, `--base`, `--error-unless-alert-pattern`, `--set-config-at` / `--set-config-value`). The
page shows them under "Equivalent CLI flags" and the reader can drop them. `?arg=` needs a page built from
canned-schedules after `v0.2.1`; an older page ignores it and checks with its defaults.

The link locks the playground's settings by default (`?lock=1`): the reader edits only the schedule, checked with
the run's flags, which stay visible but can't be changed or dropped. `--fiddle-link-lock=false` leaves them open
(and removes a `lock` the base carries). The lock shows who owns what; it isn't security, since anyone can edit the
address. Pages older than the lock ignore it.

Flags with the canned-schedules CLI's names and meaning: `--sample-interval`, `--require-base true|false`, `--base`,
`--error-unless-alert-pattern`, `--set-config-at` / `--set-config-value` (repeatable pairs) and
`--preview-downsample N`.

Exit codes: 0 valid or no schedule at all, 1 the schedule has errors (the files are still written), 2 unusable command
line or input.

## preview post

```sh
canned-schedules-companion preview post --forge github|gitlab --repo OWNER/REPO \
  --issue N [flags] dir
```

Posts `dir/comment.md` (written by `preview render`) to the issue, keeping **one comment per issue**: it lists the
issue's comments, takes those that start with the marker **and** were written by `--author` (default
`github-actions[bot]`, the workflow token's account), updates the oldest and deletes the others; with none, it
creates the comment. A marker someone pasted into their own comment is never touched.

The marker line records what the comment was rendered from: a hash of the proposal, the companion's version and a hash
of the options. When the comment already carries the same, `preview post` leaves it alone (on GitLab it uploads
nothing either), so events that don't change the proposal, such as a new title, cost no writes. Before writing, it
reads the issue again: when the text has changed since `preview render` read it, it writes nothing, and the run for
the newer text posts instead. Comments from earlier releases have no such record and are rendered again once.

With `--label`, the label says whether the schedule is valid: added when it is, removed when it isn't. Only CI should
set it, so it means "holds a valid schedule".

Text without a schedule directive (one only shown in backticks or a code block doesn't count) is not a proposal:
`preview render` exits 0 and writes a short notice as the comment, and `preview post` posts it (without links) only over an earlier
comment, when the schedule was edited away, and removes the label. An issue that never held a schedule gets nothing.

| Flag               | Meaning                                                                         |
| ------------------ | ------------------------------------------------------------------------------- |
| `--link label=URL` | a link under the comment, e.g. to an uploaded artifact (repeatable, https only) |
| `--note text`      | text in parentheses after the links                                             |
| `--label name`     | added to the issue when the schedule is valid, removed when it isn't            |
| `--author login`   | whose comments count as the step's own (locally: your login)                    |
| `--token-env NAME` | the variable holding the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`)       |
| `--api-url URL`    | the REST API, for self-hosted forges (default: github.com's or gitlab.com's)    |
| `--dry-run`        | read the comments and say what would change, but write nothing                  |

A comment longer than GitHub's 65,536 characters becomes a short notice plus the links. The token needs only the
`issues: write` permission (labels included). Locally:

```sh
GITHUB_TOKEN=$(gh auth token) canned-schedules-companion preview post --forge github \
  --repo you/catalog --issue 12 --author you --dry-run canned-schedule-preview
```

### On GitLab

`--forge gitlab` keeps one note per issue the same way, and also uploads the week to the project and shows it inline in
the note. The upload is named `schedule-<issue>-<hash>.svg`, with the first 12 characters of the SVG's SHA-256, so
another job can tell which week a note shows. `--repo` is the project's path (`group/subgroup/project`) or numeric ID. Comments count as the
step's own when the token's user wrote them, unless `--author` says otherwise.

Use a project access token with the `api` scope and the **Planner** role. Guest can write the note but can't set the
label or see confidential issues. With `read_api`, only `--dry-run` works. `CI_JOB_TOKEN` can neither write notes nor upload
files. Every changed note uploads the week again, and earlier uploads stay in the project.

```sh
GITLAB_TOKEN=glpat-… canned-schedules-companion preview post --forge gitlab \
  --repo group/catalog --issue 12 --api-url https://gitlab.example.com/api/v4 \
  --dry-run canned-schedule-preview
```
