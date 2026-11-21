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
`preview render` exits 0 and writes a short notice as the comment, and `preview post` posts it (without links) only over an earlier
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

## playground build

```sh
canned-schedules-companion playground build --site-zip canned-schedules-site_<version>.zip \
  --forge github|gitlab --project OWNER/REPO --branch main [flags] public
```

Sets up a **self-hosted playground** for a catalog: it unpacks the core's preflight page (the zip every
[canned-schedules release](https://github.com/iilei/canned-schedules/releases) attaches) into the directory and writes
its `site.json`. Published on GitHub Pages or GitLab Pages, the page checks every schedule with the catalog's flags,
and a valid schedule goes straight to the forge: **Propose new schedule** opens the new-file page with the file named
after the schedule's title, **Propose change** the edit page of a schedule the catalog has, **Open as issue** a new
issue (see "Your own playground" in the core's README).

The command is local: it reads files and writes files, nothing else. Download the zip first and verify it (the GPG
signature on `checksums.txt`, then its SHA-256), and publish the directory afterwards; the GitHub Action and the GitLab
component will do both.

| Flag                  | Meaning                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------- |
| `--site-zip file`     | the core's `canned-schedules-site_<version>.zip`, downloaded and verified (required)        |
| `--forge name`        | where proposals go: `github` or `gitlab` (required, never guessed)                          |
| `--project path`      | `OWNER/REPO`, on GitLab `GROUP/PROJECT` with any subgroups (required)                       |
| `--branch name`       | the branch proposals target, usually the default branch (required)                          |
| `--server-url URL`    | the forge's web address (default: github.com or gitlab.com); https, http only for localhost |
| `--catalog-dir dir`   | the catalog's folder in the repository (default `schedules`)                                |
| `--repo-root dir`     | the checked-out repository, where the catalog is read (default `.`)                         |
| `--no-catalog`        | the catalog has no schedules yet                                                            |
| `--args-file file`    | the flags your CI checks with, as `canned-schedules.args`; the page applies them, locked    |
| `--name text`         | shown in the page's header                                                                  |
| `--fork`              | link readers who aren't members to the project's fork page (GitLab needs a fork for them)   |

`catalog.names` comes from the `*.canned-schedule.md` files in the catalog folder, so the page knows whether a schedule
is new or a change: **run it on every merge to the default branch.** `site.json` is checked with the core before it is
written; the output directory must be new or empty, so no file of an earlier page stays behind.

`site.json` is as public as the Pages site. It holds no secrets, but it lists the catalog's schedules: for a private
catalog, restrict the Pages site (GitLab Pages can be limited to project members; on GitHub it depends on the plan).
Protect the default branch, too: members with push rights could otherwise commit straight to it from the forge's
pages.

## review render / review post

```sh
canned-schedules-companion review render [flags] schedules/night-shift.canned-schedule.md …
canned-schedules-companion review post --forge gitlab --repo GROUP/PROJECT --merge-request N [flags] dir
```

Reviews a merge request that changes the catalog, for example one proposed from the
[self-hosted playground](#playground-build). `review render` checks each changed schedule file with the core and
the catalog's rule that **a file is named after its schedule** (`Night Shift` → `night-shift.canned-schedule.md`), and
writes one comment: an overview, then each file's report and week. Paths with characters a catalog file can't have are
reported without being echoed. Exit codes: 0 all valid, 1 problems, 2 unusable input.

`review post` keeps **one note per merge request**, like the issue preview: the same stamp leaves it alone, and when
the merge request has moved on to another commit than `--head-sha`, nothing is written, so that commit's pipeline
posts. `--label` adds the label when every schedule is valid and removes it otherwise. `--retitle` names the merge
request after its schedules (`Schedule: Night Shift`), but only while it has the title the web editor gave it ("Add
new file", "Update schedules/….canned-schedule.md"), never one a person wrote. GitLab for now; GitHub follows.

| Flag              | Meaning                                                                          |
| ----------------- | -------------------------------------------------------------------------------- |
| `--head-sha sha`  | the commit the review is for (`$CI_COMMIT_SHA`)                                  |
| `--label name`    | added when every schedule is valid, removed otherwise                            |
| `--retitle`       | name the merge request after its schedules while it has the default title        |
| `--token-env`     | the variable with the token (default `GITLAB_TOKEN`)                             |
| `--api-url`       | the REST API (default gitlab.com's)                                              |
| `--dry-run`       | read and say what would change, but write nothing                                |

`templates/review.yml` runs both in merge request pipelines that touch `catalog-dir/*.canned-schedule.md`. It reads
the flags from the args file **on the target branch**, so a merge request can't loosen its own check, and needs the
issue preview's `CANNED_SCHEDULES_TOKEN`, with the Developer role to label and rename merge requests.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.1/templates/review.yml
    integrity: sha256-3CF1y1zQo117XM/wMfLIFP67pmNeQduvaHIktpFzQnI=
```

Merge requests from forks run their pipelines in the fork, where the variable isn't set; let the project run fork
pipelines in the parent project, or review them by hand.

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
     - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.1/templates/issue-preview.yml
       integrity: sha256-vxBQa9Sd3Yvp5HAeCpL8Igk57l6MKlFRyhBTCWLp7wo=
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

## GitLab Pages playground

`templates/playground.yml` publishes a [self-hosted playground](#playground-build) for the catalog on GitLab Pages, on
every pipeline of the default branch, so `site.json` lists the catalog's schedules as they are after each merge. It
installs the companion, downloads the core's site zip (by default the version the companion is built with, so the
playground checks as the issue preview does), verifies both with GPG against the release key, runs
`playground build` and publishes `public/`. It needs no token and no webhook.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.1/templates/playground.yml
    integrity: sha256-Ywvz35MA3qorhf/LZvM3JGmTUFbuCqFXuLi1J3NDCsk=
    inputs:
      name: Acme schedules
      fork: true
```

GitLab Pages must be enabled on the instance and the project. The job is named `pages`, so a project can't have a
`pages` job of its own next to it. The page's proposal targets point at `server-url` (default `$CI_SERVER_URL`), which
must be https.

| Input          | Meaning                                                                        | Default                 |
| -------------- | ------------------------------------------------------------------------------ | ----------------------- |
| `stage`        | the job's stage                                                                | `deploy`                |
| `version`      | the companion release to install                                               | this file's             |
| `site-version` | the canned-schedules release whose page to publish                             | the companion's core    |
| `image`        | the job's image; one with the binary, `curl` and `gnupg` skips the downloads   | `alpine:3.22`           |
| `server-url`   | the project's web address as readers reach it (https)                          | `$CI_SERVER_URL`        |
| `branch`       | the branch proposals target                                                    | `$CI_DEFAULT_BRANCH`    |
| `catalog-dir`  | the catalog's folder                                                           | `schedules`             |
| `args-file`    | the flags your CI checks with; a missing file means none                       | `canned-schedules.args` |
| `name`         | shown in the page's header                                                     |                         |
| `fork`         | link readers who aren't members to the fork page                               | `false`                 |
| `expire-in`    | how long the job's files are kept                                              | `30 days`               |

`site.json` is as public as the Pages site; for a private catalog, set the Pages site's visibility to project members
(Settings > General > Visibility > Pages).

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
(valid requests only) and `files-url` (not for an issue without a schedule).

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
