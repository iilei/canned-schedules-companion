# canned-schedules-companion

The lifecycle around [canned-schedules](https://github.com/iilei/canned-schedules): it turns a schedule request (an
issue written from the template) into feedback on that issue, and later moves approved requests to the catalog
("shelf"), publishes them and keeps them tidy.

**Work in progress.** Step 1, issue feedback: `preview render` (local), `preview post --forge github|gitlab` and a
GitHub Action that runs both.

## Design

- **One binary, local first.** Every step has a local part that reads and writes files only (no network, no tokens)
  and a thin forge part that posts, opens or uploads. The local part runs the same on a laptop, in any CI and with
  any forge; GitHub and GitLab get small wrappers that install the binary and run it.
- **Commands are `<step> <verb>`.** The verb says whether it writes to a forge:

  | Step          | Local (files only) | Forge (writes)       |
  | ------------- | ------------------ | -------------------- |
  | issue preview | `preview render`   | `preview post`       |
  | promotion     | `promote prepare`  | `promote open`       |
  | housekeeping  | `housekeep check`  | `housekeep retire`   |

  `preview render` and `preview post` exist so far. Forge verbs support `--dry-run` and take the forge explicitly
  (`--forge github` or `--forge gitlab`).
- **Built on the canned-schedules Go library** (`github.com/iilei/canned-schedules`), pinned in `go.mod`. Its
  results are the CLI's results, byte for byte.

## preview render

```sh
canned-schedules-companion preview render [flags] request.md
```

Checks one request, for example an issue body saved to a file, and writes into `--out-dir` (default
`canned-schedule-preview`):

| File            | Content                                                                    |
| --------------- | -------------------------------------------------------------------------- |
| `report.md`     | the error report (GitHub-flavoured Markdown, user text inside fences)      |
| `report.json`   | the same as JSON (canned-schedules' report schema)                         |
| `comment.md`    | the comment to post: a marker line, the report and, if valid, the preview  |
| `schedule.json` | the canonical output (valid requests only)                                 |
| `schedule.txt`  | the text form: the canonical schedule with the ASCII week (valid only)     |
| `schedule.svg`  | the SVG week, following the viewer's light or dark theme (valid only)      |

Files of an earlier run in the same directory are removed first, so a request that became invalid leaves no stale
preview. The comment starts with `<!-- canned-schedules:issue-preview -->`, by which a forge step finds and updates
its earlier comment.

### Playground link (opt-in)

By default, everything stays in CI and on the forge: the comment carries the report and the text form, and the
files go where your workflow puts them. Teams that host the canned-schedules preflight page themselves (built with
the core repo's `mise run web:build`, served from their own GitHub Pages or intranet) can opt in:

```sh
canned-schedules-companion preview render --fiddle-link-base-uri https://schedules.example.com/preflight/ request.md
```

The comment then ends with "Try this schedule in the playground and see its week", a link to that page with the
schedule in the URL fragment (`#doc=`, base64url): the canonical form of a valid request, the request as written
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

Exit codes: 0 valid or no schedule at all, 1 the request has errors (the files are still written), 2 unusable command
line or input.

## preview post

```sh
canned-schedules-companion preview post --forge github|gitlab --repo OWNER/REPO --issue N [flags] dir
```

Posts `dir/comment.md` (written by `preview render`) to the issue, keeping **one comment per issue**: it lists the
issue's comments, takes those that start with the marker **and** were written by `--author` (default
`github-actions[bot]`, the workflow token's account), updates the oldest and deletes the others; with none, it
creates the comment. A marker someone pasted into their own comment is never touched.

The marker line records what the comment was rendered from: a hash of the request, the companion's version and a hash
of the options. When the comment already carries the same, `preview post` leaves it alone (on GitLab it uploads
nothing either), so events that don't change the request, such as a new title, cost no writes. Before writing, it
reads the issue again: when the text has changed since `preview render` read it, it writes nothing, and the run for
the newer text posts instead. Comments from earlier releases have no such record and are rendered again once.

With `--label`, the label says whether the request is valid: added when it is, removed when it isn't. Only CI should
set it, so it means "holds a valid schedule".

Text without a schedule directive (one only shown in backticks or a code block doesn't count) is not a request:
`preview render` exits 0 and writes a short notice as the comment, and `preview post` posts it only over an earlier
comment, when the schedule was edited away, and removes the label. An issue that never held a schedule gets nothing.

| Flag                        | Meaning                                                                         |
| --------------------------- | ------------------------------------------------------------------------------- |
| `--link label=URL`          | a link under the comment, e.g. to an uploaded artifact (repeatable, https only) |
| `--note text`               | text in parentheses after the links                                             |
| `--label name`              | added to the issue when the request is valid, removed when it isn't             |
| `--author login`            | whose comments count as the step's own (locally: your login)                    |
| `--token-env NAME`          | the variable holding the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`)       |
| `--api-url URL`             | the REST API, for self-hosted forges (default: github.com's or gitlab.com's)    |
| `--dry-run`                 | read the comments and say what would change, but write nothing                  |

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

Use a project access token with the `api` scope and the **Planner** role. Guest also works, except on confidential
issues, which it can't see. With `read_api`, only `--dry-run` works. `CI_JOB_TOKEN` can neither write notes nor upload
files. Every changed note uploads the week again, and earlier uploads stay in the project.

```sh
GITLAB_TOKEN=glpat-… canned-schedules-companion preview post --forge gitlab \
  --repo group/catalog --issue 12 --api-url https://gitlab.example.com/api/v4 --dry-run canned-schedule-preview
```

## GitLab CI/CD component

`templates/issue-preview.yml` checks a schedule request issue and keeps one note on it up to date, with the week shown
inline. GitLab can't start a pipeline from an issue event by itself, so a webhook calls a pipeline trigger.

1. **Create a project access token** in the project that holds the issues, with the `api` scope and the Planner role.
2. **Store it** as the CI/CD variable `CANNED_SCHEDULES_TOKEN`, masked. Before it expires, **rotate** it and store the
   new value. Rotation keeps the token's bot user, so the bot goes on updating its notes. A newly created token is a
   new user: it can't edit the old bot's notes, so each issue would get a second note.
3. **Create a pipeline trigger token** under Settings > CI/CD > Pipeline trigger tokens.
4. **Add a webhook** under Settings > Webhooks, with **Issues events** (and **Confidential issues events** if you use
   them) and this URL:

   ```text
   https://gitlab.example.com/api/v4/projects/<project ID>/ref/<default branch>/trigger/pipeline?token=<trigger token>
   ```

5. **Include the template** in the project's `.gitlab-ci.yml`, directly from this repository's release tag:

   ```yaml
   include:
     - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.2.0/templates/issue-preview.yml
       integrity: sha256-TwNPUKiLal+0nogu0vTfCZrIpYaUFEtVhsPmTe82f2Y=
   ```

   GitLab rejects the file if it doesn't match `integrity`. Inputs go under `inputs:` of the same entry.

The job runs when an issue is opened or reopened and when its description changes, and checks it when the description
holds a schedule directive (`[!SCHEDULE`, any case) or the issue has the label. It reads the text from the API, not from
the event. It adds the label to an issue with a valid schedule and removes it otherwise; label changes alone don't start
it, so its own label doesn't either. A request with errors gets its note, and the job then fails. The
job keeps its files as artifacts under `canned-schedule-preview/`, for further jobs of your own. They include the request
as GitLab stores it, named after its origin, for example `acme-catalog-issue-12.md` or `acme-catalog-task-13.md`.

[The GitLab flow in screenshots](docs/gitlab-flow/README.md) shows each step, from the new issue to the updated note.

| Input                  | Meaning                                                                          | Default          |
| ---------------------- | -------------------------------------------------------------------------------- | ---------------- |
| `stage`                | the job's stage                                                                  | `test`           |
| `version`              | the release to install; its signature and checksum are verified                  | this file's      |
| `image`                | the job's image; one with the binary, `curl` and `jq` skips the download         | `alpine:3.22`    |
| `label`                | added when the schedule is valid, removed when not; empty leaves labels alone    | `Schedule`       |
| `api-url`              | the REST API as a job reaches it                                                 | `$CI_API_V4_URL` |
| `args`                 | evaluation flags, one argument per line, as in `canned-schedules.args`           |                  |
| `fiddle-link-base-uri` | a preflight page you host, linked from the note                                  | no link          |
| `expire-in`            | how long the job's files are kept                                                | `30 days`        |
| `dry-run`              | says what would change, but writes nothing                                       | `false`          |

## GitHub Action

The action at the root of this repository checks a schedule request issue and keeps one comment on it up to date. It
installs a signed release, runs `preview render`, uploads the files as workflow artifacts and runs `preview post`. A
request with errors still gets its comment; the job fails afterwards. It is tested on Linux runners and is available
from `v0.1.1`.

```yaml
name: schedule request

on:
  issues:
    types: [opened, reopened, edited]

permissions:
  contents: read

concurrency:
  group: schedule-request-${{ github.event.issue.number }}
  cancel-in-progress: true

jobs:
  preview:
    # A schedule directive in the body (contains() ignores case), or the label the action keeps up to date.
    if: contains(github.event.issue.body, '[!SCHEDULE') || contains(github.event.issue.labels.*.name, 'Schedule')
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    permissions:
      contents: read
      issues: write
    steps:
      # The body is untrusted: it reaches the shell only through an environment variable and a file.
      - name: Write the issue body to a file
        env:
          BODY: ${{ github.event.issue.body }}
        run: printf '%s' "$BODY" > "$RUNNER_TEMP/request.md"

      - uses: iilei/canned-schedules-companion@<commit SHA> # <release tag>
        with:
          body-file: ${{ runner.temp }}/request.md
```

| Input                  | Meaning                                                                                 | Default               |
| ---------------------- | --------------------------------------------------------------------------------------- | --------------------- |
| `body-file`            | the issue body as a file (required)                                                     |                       |
| `version`              | the release to install                                                                  | this action's release |
| `issue`                | the issue to comment on                                                                 | the triggering issue  |
| `out-dir`              | where the files go                                                                      | a temporary directory |
| `args`                 | evaluation flags, one argument per line, as in `canned-schedules.args`                  |                       |
| `fiddle-link-base-uri` | a preflight page you host, linked from the comment                                      | no link               |
| `retention-days`       | how many days the uploaded files are kept                                               | `30`                  |
| `token`                | reads and writes the issue's comments                                                   | `github.token`        |
| `comment-author`       | the login behind `token`                                                                | `github-actions[bot]` |
| `label`                | added when the request is valid, removed when not; empty leaves labels alone            | `Schedule`            |
| `dry-run`              | `true` writes no comment but says what would change; the files are still uploaded       | `false`               |

Outputs: `valid` (`true` or `false`), `schedule` (`false` when the issue holds no schedule), `out-dir`, `svg-url`
(valid requests only) and `files-url`.

The zip also holds the request exactly as submitted, named after its origin, for example
`acme-catalog-issue-12.md`.

## Using the JSON

Every valid request produces `schedule.json`: the canonical week, its transitions with cron expressions, and the rules
the request was checked against. The [canned-schedules](https://github.com/iilei/canned-schedules) repository shows how
to put it to work:

| Looking for                                   | Where                                                                                                                        |
| --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| What each field means                         | [Output](https://github.com/iilei/canned-schedules#output)                                                                   |
| The JSON Schema                               | [`schema/output.schema.json`](https://github.com/iilei/canned-schedules/blob/master/schema/output.schema.json)               |
| Scheduling capacity on AWS, step by step      | [AWS Auto Scaling scheduled actions](https://github.com/iilei/canned-schedules#aws-auto-scaling-scheduled-actions)           |
| Working Terraform and AWS CLI files to copy   | [`examples/aws`](https://github.com/iilei/canned-schedules/tree/master/examples/aws)                                         |

## The week as a PNG

`preview render` writes the week as `schedule.svg`. To use it outside the forge, for example on a slide or in a chat
message, convert it with `rsvg-convert`. Both recipes expect `schedule.svg` in the current folder. `font-ubuntu` provides
Ubuntu Mono, the SVG's first choice of font, and `ttf-dejavu` provides the `→` that Ubuntu Mono lacks.

The SVG decides the theme. `preview render` writes one that follows the viewer, and `rsvg-convert` draws that in light
colours. For a dark PNG, write the SVG with the
[canned-schedules](https://github.com/iilei/canned-schedules) CLI first:

```sh
canned-schedules --svg-theme dark --svg-output-file schedule.svg schedule.md > /dev/null
```

<details>
<summary>Nonroot Wolfi image: build it once, then render without network access</summary>

```dockerfile
# Tested with cgr.dev/chainguard/wolfi-base@sha256:580c9ae79c0229cc381475fbfbed0d91fab5d6606e57cd78732f9836de109e90
FROM cgr.dev/chainguard/wolfi-base
RUN apk add --no-cache rsvg-convert fontconfig font-ubuntu ttf-dejavu
USER 65532:65532
ENTRYPOINT ["rsvg-convert"]
```

```sh
docker build -t schedule-png .

docker run --rm --network none --read-only --tmpfs /tmp \
  --cap-drop ALL --security-opt no-new-privileges \
  --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v "$PWD:/work" \
  schedule-png --zoom 2 --background-color white \
  --output /work/schedule.png /work/schedule.svg
```

The PNG belongs to your own user.

</details>

<details>
<summary>Alpine one-off: nothing to build, but it runs as root</summary>

```sh
docker run --rm -v "$PWD:/work" alpine:3.22 sh -c '
  apk add --no-cache --quiet rsvg-convert fontconfig font-ubuntu ttf-dejavu
  rsvg-convert --zoom 2 --background-color white \
    --output /work/schedule.png /work/schedule.svg
'
```

The PNG belongs to `root`.

</details>

## Development

```sh
mise run check   # format check, vet, lint, Markdown lint, tests (race detector), build
```

## License

[FSL-1.1-ALv2](LICENSE.md), like canned-schedules: any use except a competing commercial product or service; each
release becomes Apache-2.0 two years after it is published. The binary includes canned-schedules, under the same
licence.
