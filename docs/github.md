# On GitHub

## GitHub Action

The action at the root of this repository checks an issue that proposes a schedule and keeps one comment on it up to date. It
installs a signed release, runs `preview render`, uploads the files as workflow artifacts and runs `preview post`. A
proposal with errors still gets its comment; the job fails afterwards. It is tested on Linux runners and is available
from `v0.1.1`.

```yaml
name: schedule proposal

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

| Input                  | Meaning                                                                           | Default               |
| ---------------------- | --------------------------------------------------------------------------------- | --------------------- |
| `body-file`            | the issue body as a file (required)                                               |                       |
| `version`              | the release to install                                                            | this action's release |
| `issue`                | the issue to comment on                                                           | the triggering issue  |
| `out-dir`              | where the files go                                                                | a temporary directory |
| `args`                 | evaluation flags, one argument per line, as in `canned-schedules.args`            |                       |
| `fiddle-link-base-uri` | a preflight page you host, linked from the comment                                | no link               |
| `retention-days`       | how many days the uploaded files are kept                                         | `30`                  |
| `token`                | reads and writes the issue's comments                                             | `github.token`        |
| `comment-author`       | the login behind `token`                                                          | `github-actions[bot]` |
| `label`                | added when the schedule is valid, removed when not; empty leaves labels alone     | `Schedule`            |
| `dry-run`              | `true` writes no comment but says what would change; the files are still uploaded | `false`               |

Outputs: `valid` (`true` or `false`), `schedule` (`false` when the issue holds no schedule), `out-dir`, `svg-url`
(valid schedules only) and `files-url` (not for an issue without a schedule).

The zip also holds the proposal exactly as submitted, named after its origin, for example
`acme-catalog-issue-12.md`.

## GitHub Pages playground

`playground/action.yml` builds a [self-hosted playground](playground.md#playground-build) for the catalog and uploads it for GitHub
Pages. It installs the companion (mise verifies the release's provenance attestation), downloads the core's site zip
(by default the version the companion is built with, so the playground checks as the review does), verifies it with GPG
against the release key, runs `playground build --forge github` and uploads the site with `upload-pages-artifact`.
Deploying takes a job of its own, since only a job can name the `github-pages` environment. Run it on every push to
the default branch, so `site.json` lists the catalog's schedules as they are after each merge. Linux runners only.

One-time setup: Settings → Pages → Build and deployment → Source: "GitHub Actions".

```yaml
name: playground

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: playground
  cancel-in-progress: false

jobs:
  build:
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@<commit SHA> # <release tag>
        with:
          persist-credentials: false
      - uses: iilei/canned-schedules-companion/playground@<commit SHA> # <release tag>
        with:
          name: Acme schedules

  deploy:
    needs: build
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    permissions:
      pages: write
      id-token: write
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - id: deployment
        uses: actions/deploy-pages@<commit SHA> # <release tag>
```

| Input          | Meaning                                                            | Default                    |
| -------------- | ------------------------------------------------------------------ | -------------------------- |
| `version`      | the companion release to install                                   | this action's release      |
| `site-version` | the canned-schedules release whose page to publish                 | the companion's core       |
| `server-url`   | the forge's web address as readers reach it (https)                | `github.server_url`        |
| `repository`   | where proposals go, `OWNER/REPO`                                   | `github.repository`        |
| `branch`       | the branch proposals target                                        | the default branch         |
| `catalog-dir`  | the catalog's folder                                               | `schedules`                |
| `args-file`    | the flags your CI checks with; a missing file means none           | `canned-schedules.args`    |
| `name`         | shown in the page's header                                         |                            |
| `fork`         | `true` links readers who can't push to the fork page               | `false`                    |
| `token`        | lets mise read the release from GitHub's API                       | `github.token`             |

Outputs: `site-dir` (the built site) and `artifact-id` (the uploaded Pages artifact).

GitHub's new-file page takes the schedule from the link; its edit page doesn't, so **Propose change** copies the
schedule and opens the page. `site.json` is as public as the Pages site: whether a private repository's Pages site can be
restricted depends on the plan.

## GitHub pull request review

`review/action.yml` runs [`review render` and `review post`](review.md#review-render--review-post) on a pull request that
changes the catalog: one comment kept up to date, the label, and a title named after the schedules while the pull
request has the web editor's default. The flags come from the args file **on the base branch**, so a pull request
can't loosen its own check. The review also goes to the job summary. A schedule with errors still gets its comment;
the job fails afterwards.

```yaml
name: schedule review

on:
  pull_request:
    paths:
      - schedules/*.canned-schedule.md

permissions:
  contents: read

concurrency:
  group: schedule-review-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  review:
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    permissions:
      contents: read
      pull-requests: write # The comment, the label and the title.
    steps:
      - uses: actions/checkout@<commit SHA> # <release tag>
        with:
          fetch-depth: 0 # The diff needs the base commit.
          persist-credentials: false
      - uses: iilei/canned-schedules-companion/review@<commit SHA> # <release tag>
```

| Input            | Meaning                                                                          | Default                 |
| ---------------- | -------------------------------------------------------------------------------- | ----------------------- |
| `version`        | the companion release to install                                                 | this action's release   |
| `catalog-dir`    | the catalog's folder; only its `*.canned-schedule.md` files are reviewed         | `schedules`             |
| `args-file`      | the flags to check with, read from the base branch                               | `canned-schedules.args` |
| `label`          | added when every changed schedule is valid, removed otherwise; empty: no labels  | `Schedule`              |
| `retitle`        | `true` names the pull request after its schedules while it has the default title | `true`                  |
| `token`          | comments on, labels and renames the pull request                                 | `github.token`          |
| `comment-author` | the login behind `token`                                                         | `github-actions[bot]`   |
| `dry-run`        | `true` reads the pull request and says what would change, but writes nothing     | `false`                 |

Outputs: `valid` (`true` or `false`) and `out-dir`.

Pull requests from forks get a read-only token on `pull_request`: the review then goes to the job summary only. The
action doesn't support `pull_request_target`, which would run with write access next to a fork's files.
