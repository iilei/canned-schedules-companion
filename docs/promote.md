# promote prepare / promote open

```sh
canned-schedules-companion promote prepare [flags] --catalog-dir schedules \
  --out-dir dir issue.md

canned-schedules-companion promote open --forge github|gitlab --repo P --issue N \
  --base-branch main [flags] dir
```

**Promotes an approved issue to the catalog:** someone with the right to set labels adds the approval label
(`Schedule approved` by default) to an issue that proposes a schedule, and a pull request (GitHub) or merge request
(GitLab) appears that adds it to the catalog and closes the issue. Merging it is the last step, as for proposals from
the playground.

`promote prepare` is local: it checks the issue's text with the catalog's flags and writes `schedule.md` (the text as
the author wrote it, with LF line endings and a final newline), `report.md` and `promote.json` (`valid`, `name`,
`path`, `title`). The file's path is `catalog-dir/` plus the file name of the schedule's title (`Night Shift` →
`schedules/night-shift.canned-schedule.md`). Exit codes: 0 valid, 1 no valid schedule, 2 unusable input.

`promote open` commits the file to the branch `canned-schedules/issue-<N>` (from `--base-branch` the first time) and
opens one request titled after the schedule (`Schedule: Night Shift`) whose description closes the issue. A second run
changes nothing, an edited issue updates the branch, and when the base branch already holds the same text there is
nothing to promote. When the issue holds no valid schedule (an edit after the approval broke it), `--approved-label`
is removed again and the command exits 1, so the label never stands for something CI rejects.

| Flag               | Meaning                                                                     |
| ------------------ | --------------------------------------------------------------------------- |
| `--base-branch`    | the branch the request targets (required)                                   |
| `--branch-prefix`  | the branch name before the issue number (default `canned-schedules/issue-`) |
| `--approved-label` | removed from the issue when its schedule has errors                         |
| `--token-env`      | the variable with the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`)      |
| `--api-url`        | the REST API (default github.com's or gitlab.com's)                         |
| `--dry-run`        | read and say what would change, but write nothing                           |

**On GitHub,** `promote/action.yml` runs both on `issues: labeled`. The workflow token may open pull requests only with
Settings → Actions → General → "Allow GitHub Actions to create and approve pull requests", and a pull request it opens
starts no workflows, so the review action doesn't run on it (the promotion checked the schedule with the same flags).
Setting labels takes the Triage role or more.

```yaml
name: schedule promotion

on:
  issues:
    types: [labeled]

permissions:
  contents: read

concurrency:
  group: schedule-promotion-${{ github.event.issue.number }}
  cancel-in-progress: false

jobs:
  promote:
    if: github.event.label.name == 'Schedule approved'
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    permissions:
      contents: write # Pushes the branch.
      pull-requests: write # Opens the pull request.
      issues: write # Takes the label back when the schedule has errors.
    steps:
      - uses: actions/checkout@<commit SHA> # <release tag>
        with:
          persist-credentials: false
      # The body is untrusted: it reaches the shell only through an environment variable
      # and a file.
      - name: Write the issue body to a file
        env:
          BODY: ${{ github.event.issue.body }}
        run: printf '%s' "$BODY" > "$RUNNER_TEMP/issue.md"
      - uses: iilei/canned-schedules-companion/promote@<commit SHA> # <release tag>
        with:
          body-file: ${{ runner.temp }}/issue.md
```

| Input            | Meaning                                                               | Default                    |
| ---------------- | --------------------------------------------------------------------- | -------------------------- |
| `body-file`      | the issue body as a file (required)                                   |                            |
| `version`        | the companion release to install                                      | this action's release      |
| `issue`          | the approved issue                                                    | the triggering issue       |
| `base-branch`    | the branch the pull request targets                                   | the default branch         |
| `catalog-dir`    | the catalog's folder                                                  | `schedules`                |
| `args-file`      | the flags to check with, from the checkout; a missing file means none | `canned-schedules.args`    |
| `approved-label` | taken back when the issue holds no valid schedule; empty: left alone  | `Schedule approved`        |
| `branch-prefix`  | the branch name before the issue number                               | `canned-schedules/issue-`  |
| `token`          | pushes the branch, opens the pull request, removes the label          | `github.token`             |
| `dry-run`        | `true` reads the repository and says what would change                | `false`                    |

Outputs: `valid` and `out-dir`.

**On GitLab,** `templates/promote.yml` runs in the pipeline the issue preview's webhook starts, and acts only when the
event added the approval label. It needs a project access token with the Developer role (to push the branch and open
the merge request; Planner and Reporter get 403) in `CANNED_SCHEDULES_TOKEN`, or the variable `token-variable` names;
the merge request runs the merge request review like any other. Setting labels takes the Planner role or more, so
that is who approves.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.10/templates/promote.yml
    integrity: sha256-g646HUWbj727xwmvcAJE6zqYBRR4l2sCyG90UngrCZ4=
```

Inputs: `stage` (`deploy`), `version`, `image`, `approved-label`, `catalog-dir`, `args-file` (read from the
pipeline's checkout of the default branch), `base-branch` (`$CI_DEFAULT_BRANCH`), `branch-prefix`, `api-url` and
`dry-run`.
