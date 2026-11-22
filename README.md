# canned-schedules-companion

The lifecycle around [canned-schedules](https://github.com/iilei/canned-schedules): it turns a schedule proposal (an
issue written from the template) into feedback on that issue, and later moves approved requests to the catalog
("catalog"), publishes them and keeps them tidy.

**Work in progress.** Step 1, issue feedback: `preview render` (local), `preview post --forge github|gitlab` and a
GitHub Action that runs both.

## Design

- **One binary, local first.** Every step has a local part that reads and writes files only (no network, no tokens)
  and a thin forge part that posts, opens or uploads. The local part runs the same on a laptop, in any CI and with
  any forge; GitHub and GitLab get small wrappers that install the binary and run it.
- **Commands are `<step> <verb>`.** The verb says whether it writes to a forge:

  | Step          | Local (files only) | Forge                           |
  | ------------- | ------------------ | ------------------------------- |
  | issue preview | `preview render`   | `preview post`                  |
  | promotion     | `promote prepare`  | `promote open`                  |
  | publishing    | `publish prepare`  | `publish fetch`, `publish post` |
  | housekeeping  | `housekeep check`  | `housekeep retire`              |

  `preview render` and `preview post` exist so far. Forge verbs support `--dry-run` and take the forge explicitly
  (`--forge github` or `--forge gitlab`).
- **Built on the canned-schedules Go library** (`github.com/iilei/canned-schedules`), pinned in `go.mod`. Its
  results are the CLI's results, byte for byte.

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
canned-schedules-companion preview render --fiddle-link-base-uri https://schedules.example.com/preflight/ request.md
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
canned-schedules-companion preview post --forge github|gitlab --repo OWNER/REPO --issue N [flags] dir
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
after the schedule's title, **Propose change** copies the schedule and opens the edit page of a schedule the catalog has (neither forge
prefills an existing file), **Open as issue** a new
issue (see "Your own playground" in the core's README).

The command is local: it reads files and writes files, nothing else. Download the zip first and verify it (the GPG
signature on `checksums.txt`, then its SHA-256), and publish the directory afterwards; the
[GitHub Action](#github-pages-playground) and the [GitLab component](#gitlab-pages-playground) do both.

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
canned-schedules-companion review post --forge github|gitlab --repo OWNER/REPO --merge-request N [flags] dir
```

Reviews a merge request (a pull request on GitHub) that changes the catalog, for example one proposed from the
[self-hosted playground](#playground-build). `review render` checks each changed schedule file with the core and
the catalog's rule that **a file is named after its schedule** (`Night Shift` → `night-shift.canned-schedule.md`), and
writes one comment: an overview, then each file's report and week. Each valid schedule's week is also written as
`week-N.svg`, and **on GitLab `review post` uploads it and shows it inline** above that schedule's section, as the
issue preview does; on GitHub, which strips inline SVG and has no upload API for bots, the text form stands alone. Paths with characters a catalog file can't have are
reported without being echoed. Exit codes: 0 all valid, 1 problems, 2 unusable input.

**The comment records what it vouches for:** every file that passed is listed with its digest (`sha256:<64 hex>` of the
bytes as reviewed) in its section and in a hidden block under the marker line, which [publishing](#publish-prepare)
reads to tell whether the merged file is the one reviewed:

```text
<!-- canned-schedules:digests
schedules/night-shift.canned-schedule.md sha256:a64ca733…
-->
```

`review post` keeps **one comment per merge request**, like the issue preview: the same stamp leaves it alone, and when
the merge request has moved on to another commit than `--head-sha`, nothing is written, so that commit's pipeline
posts. `--label` adds the label when every schedule is valid and removes it otherwise. `--retitle` names the merge
request after its schedules (`Schedule: Night Shift`), but only while it has the title the web editor gave it ("Add
new file", "Update schedules/….canned-schedule.md", GitHub's "Create night-shift.canned-schedule.md"), never one a
person wrote. On GitHub the comment, the labels and the title go through the REST API with the workflow token; the
default `--author` is `github-actions[bot]`, on GitLab the token's user.

| Flag              | Meaning                                                                          |
| ----------------- | -------------------------------------------------------------------------------- |
| `--head-sha sha`  | the commit the review is for (`$CI_COMMIT_SHA`)                                  |
| `--label name`    | added when every schedule is valid, removed otherwise                            |
| `--retitle`       | name the merge request after its schedules while it has the default title        |
| `--token-env`     | the variable with the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`)           |
| `--api-url`       | the REST API (default github.com's or gitlab.com's)                              |
| `--author`        | only this login's comments count as the step's own                               |
| `--dry-run`       | read and say what would change, but write nothing                                |

`templates/review.yml` runs both in merge request pipelines that touch `catalog-dir/*.canned-schedule.md`. It reads
the flags from the args file **on the target branch**, so a merge request can't loosen its own check, and needs the
issue preview's `CANNED_SCHEDULES_TOKEN`, with the Developer role to label and rename merge requests.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.7/templates/review.yml
    integrity: sha256-MMyaH77cMIzJ+NEx2bNlmO2Sjai8cv9mPJNy7PYseRg=
```

Merge requests from forks run their pipelines in the fork, where the variable isn't set; let the project run fork
pipelines in the parent project, or review them by hand. On GitHub, [`review/action.yml`](#github-pull-request-review)
runs both.

## promote prepare / promote open

```sh
canned-schedules-companion promote prepare [flags] --catalog-dir schedules --out-dir dir issue.md
canned-schedules-companion promote open --forge github|gitlab --repo P --issue N --base-branch main [flags] dir
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
      # The body is untrusted: it reaches the shell only through an environment variable and a file.
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
event added the approval label. It needs the issue preview's `CANNED_SCHEDULES_TOKEN` with the Developer role (to
push the branch and open the merge request); the merge request runs the merge request review like any other. Setting
labels takes the Reporter role or more.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.7/templates/promote.yml
    integrity: sha256-UmZcbECNMDoh58sRWuTFV4q3l4Xa8isKJ74iDqDdJ+A=
```

Inputs: `stage` (`deploy`), `version`, `image`, `approved-label`, `catalog-dir`, `args-file` (read from the
pipeline's checkout of the default branch), `base-branch` (`$CI_DEFAULT_BRANCH`), `branch-prefix`, `api-url` and
`dry-run`.

## publish fetch

```sh
canned-schedules-companion publish fetch --forge github|gitlab --repo P --commit SHA [flags]
```

**Finds the merge request a pushed commit came from** (a pull request on GitHub) and writes it, with the digests its
review vouched for, as `merge-request.json` for [`publish prepare`](#publish-prepare). It only reads from the forge,
so it has no `--dry-run`.

- **The forge's records, not git's:** it asks which merged merge request has the commit as its result (GitLab `GET
  repository/commits/:sha/merge_requests`, GitHub `GET commits/:sha/pulls`), which works after squash and rebase
  merges. The times, the author and who merged come from there, in UTC.
- **The review is the comment the review step keeps,** found by its marker and author as `review post` finds it. A
  merge request without one, or with one an older release wrote (no digest block), gets `"review": null`, and every
  schedule is published as not reviewed.
- **A commit no merged merge request produced** (a direct push) exits 1 and writes nothing. Protect the default branch.

| Flag          | Meaning                                                                    |
| ------------- | -------------------------------------------------------------------------- |
| `--commit`    | the pushed commit (`$CI_COMMIT_SHA`, `${{ github.sha }}`); required        |
| `--out-file`  | where it goes (default `merge-request.json`)                               |
| `--author`    | only this login's review comment counts (default as for `review post`)     |
| `--token-env` | the variable with the token (default `GITHUB_TOKEN` or `GITLAB_TOKEN`)     |
| `--api-url`   | the REST API (default github.com's or gitlab.com's)                        |

Exit codes: 0 written, 1 no merged merge request for the commit, 2 unusable input or a forge error.

## publish prepare

```sh
canned-schedules-companion publish prepare --merge-request-file merge-request.json [flags] file...
```

**Lays out what a merged merge request publishes** (step 3, after [`publish fetch`](#publish-fetch), before
[`publish post`](#publish-post); the wrappers come next). It is local: it reads the merged schedule files and `merge-request.json`, and writes into `--out-dir`, which
must be empty or missing, so nothing stale is published.

- **Always rendered from the file as merged,** with the flags given (the args file of the commit being published).
  Review outputs are never reused: they come from the merge request's own pipeline. A file that doesn't render without
  errors, or isn't named after its title, stops the publish (exit 1, nothing written).
- **The review only vouches by digest:** `review.digests` in the input maps each reviewed path to its digest
  (`sha256:<64 hex>`, as Docker writes them). A file whose digest differs (a rebase, an edit after the review, a
  conflict resolved in the merge), that the review didn't cover, or a merge request without a review is published
  anyway, with `reviewed: false` and a reason (`changed`, `not-reviewed`, `no-review`); stdout lists them, and the
  forge part will post a note on the merge request.
- **One schedule is the unit.** `--path-template` names each schedule's folder, and the files keep their catalog names:

  ```text
  night-shift/42-sha256:a64ca73340b7/
    night-shift.canned-schedule.md               the file as merged
    night-shift.canned-schedule.json             .svg  .txt
    night-shift.canned-schedule.provenance.json
  manifest.json
  ```

  `manifest.json` always holds a list, `{"version": 1, "merge-request": {…}, "schedules": [ … ]}`: per schedule its
  name, slug, path, digest, `reviewed`, `reviewed-digest` and `reason`, its `dir`, its `files` (path, digest, media
  type, and `checksums` with the SHA-1 and MD5 that Artifactory also wants from a client) and its metadata. One schedule is a list of one, a merge request without schedule changes an empty list, so a
  post-fetch script just loops.
- **The provenance** comes from the forge's records, not from git: the merge request (number, URL, title, author,
  merged by, created and merged at in UTC, target branch, the merge commit for reference only), the review (whether
  the digests match, the reviewed digest, the reason, the review comment's URL), the companion and core versions with
  the digest of the flags, and the digest (and checksums) of every other file in the folder.

**Templates.** The path and the metadata use the same placeholders as `site.json`, and every variable works in both:

| Variable                                     | Value                                                                           |
| -------------------------------------------- | ------------------------------------------------------------------------------- |
| `name`, `slug`, `path`, `digest`, `reviewed` | the schedule's title, file stem, path in the repository, digest, `true`/`false` |
| `mr`, `title`, `author`, `merged-by`         | the merge request's number, title, author and merger                            |
| `created-at`, `merged-at`, `merged-date`     | the forge's times in UTC: `2026-11-22T14:30:00Z`, `2026-11-22`                  |
| `forge`, `host`, `project`, `branch`         | where it came from; `branch` is the target branch                               |
| `tool-version`, `core-version`               | the companion and the core                                                      |

Helpers chain with `|`: `toLower`, `toUpper`, `reverseByDotDelim` (`gitlab.example.com` → `com.example.gitlab`),
`truncate N` (the first N characters: `{digest | truncate 19}` is `sha256:` and 12 hex) and `afterLast "sep"` (what
follows the last separator, the whole value without one: `{digest | afterLast ":" | truncate 12}` is 12 hex). Windows
can't create files or folders with a `:` in their name, so keep the digest's prefix out of the path there. Anything
else is the post-fetch script's job. `{{` and `}}` stand for a literal `{` and `}` (`{{slug}}` is the text
`{slug}`). An unknown variable or helper is an error, and a filled path must stay inside the output
directory (relative, no empty, `.` or `..` folders); otherwise the layout is yours. Two schedules writing the same
file fail the run unless `--force-overwrite`.

**Metadata** is one `key=value` template per line (blank lines and `#` comments skipped); keys are lower-case letters,
digits, `.`, `_` and `-`. The manifest carries each schedule's `metadata` as a map and in two ready forms, so values a
person typed (names, titles) need no escaping in the script:

- `artifactory`: matrix parameters to append to the deploy URL, every value percent-encoded
  (`;merged-by=J%C3%BCrgen;…`). Checked against Artifactory OSS 7.104: the properties read back exactly.
- `s3`: a map for `aws s3 cp --metadata "$(jq -c .s3 …)"` or `x-amz-meta-*` headers, with values outside printable
  ASCII RFC 2047 encoded as S3 returns them; `null` (and a warning) when it exceeds S3's 2 KB.

```sh
# Upload one publish to Artifactory: every file with its checksums and the schedule's properties. Artifactory flags
# a file whose client sent no SHA-1 and MD5, so the manifest has them next to the digest.
jq -r '.schedules[] | .artifactory as $p | .files[]
    | [.path, .digest, .checksums.sha1, .checksums.md5, $p] | @tsv' manifest.json |
  while IFS="$(printf '\t')" read -r path digest sha1 md5 props; do
    curl -fsS -u "$ARTIFACTORY_USER:$ARTIFACTORY_TOKEN" -H "X-Checksum-Sha256: ${digest#sha256:}" \
      -H "X-Checksum-Sha1: $sha1" -H "X-Checksum-Md5: $md5" -T "$path" "$ARTIFACTORY_URL/catalog-local/$path$props"
  done
```

```sh
# Upload one publish to S3: every file with its media type, its digest as the checksum and the schedule's metadata.
jq -r '.schedules[] | (.s3 // {} | tojson | @base64) as $m | .files[] | [.path, .digest, .["media-type"], $m] | @tsv' \
  manifest.json |
  while IFS="$(printf '\t')" read -r path digest type meta; do
    aws s3api put-object --bucket "$BUCKET" --key "$path" --body "$path" --content-type "$type" \
      --metadata "$(printf '%s' "$meta" | base64 -d)" \
      --checksum-sha256 "$(printf '%s' "${digest#sha256:}" | xxd -r -p | base64)"
  done
```

| Flag                   | Meaning                                                                     | Default                                                                                |
| ---------------------- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `--merge-request-file` | the merge request and its review, as `publish fetch` writes them (required) |                                                                                        |
| `--path-template`      | each schedule's folder                                                      | `{slug}/{mr}-{digest \| afterLast ":" \| truncate 12}`                                 |
| `--metadata`           | one `key=value` template per line; empty: none                              | `schedule`, `digest`, `reviewed`, `merge-request`, `merged-at`, `merged-by`, `project` |
| `--force-overwrite`    | let a later schedule overwrite an earlier one's file                        | `false`                                                                                |
| `--out-dir`            | where the files go; must be empty or missing                                | `canned-schedule-publish`                                                              |

plus the evaluation flags of `review render`. Exit codes: 0 laid out, 1 a schedule doesn't render, 2 unusable input.

`merge-request.json`, as `publish fetch` writes it or by hand for a local run (unknown keys are errors):

```json
{
  "merge-request": {
    "forge": "gitlab", "host": "gitlab.example.com", "project": "ops/catalog", "number": 42,
    "url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42", "title": "Schedule: Night Shift",
    "author": "ana", "merged-by": "jo", "created-at": "2026-11-20T09:15:00Z", "merged-at": "2026-11-22T14:30:00Z",
    "target-branch": "main", "merge-commit": "4c202b5"
  },
  "review": {
    "url": "https://gitlab.example.com/ops/catalog/-/merge_requests/42#note_7",
    "digests": {"schedules/night-shift.canned-schedule.md": "sha256:a64ca733…"}
  }
}
```

`number`, `created-at` and `merged-at` are required; `review` is `null` when the merge request has no review comment.

## publish post

```sh
canned-schedules-companion publish post --forge github|gitlab --repo P [flags] dir
```

**The loud signal:** keeps one note on the merged merge request, read from `dir/manifest.json` (written by `publish
prepare`), that lists every schedule published without the review vouching for it: the path, why (changed after the
last review, not in the review, no review comment), the published digest and the reviewed one. When every schedule was
published as reviewed, it writes nothing. The same note again is left alone. Flags as for `review post`
(`--author`, `--token-env`, `--api-url`, `--dry-run`); the marker is `<!-- canned-schedules:publish-note -->`.

## Publishing on GitLab and GitHub

Both wrappers run on a **push to the default branch** that changes `catalog-dir/*.canned-schedule.md`:
[`publish fetch`](#publish-fetch), [`publish prepare`](#publish-prepare) with the args file of the pushed commit and
the schedule files the push adds or changes, then the team's **post-fetch script** with the output directory, then
[`publish post`](#publish-post). A direct push (no merged merge request) fails the job and publishes nothing, so
protect the default branch. `dry-run` reads and lays out everything, but runs no post-fetch script and posts no note.

The post-fetch script is a file in the repository, run with the output directory as its argument; it uploads, and
should fail when the upload fails. Its secrets are the job's: CI/CD variables on GitLab, the `env` of the step that
uses the action on GitHub. The upload loops under [`publish prepare`](#publish-prepare) work as such a script, with
`cd "$1"` first.

**GitLab:** `templates/publish.yml` needs the review's `CANNED_SCHEDULES_TOKEN` and runs the script in its `image`,
which needs the script's tools (`curl`, `jq`, the AWS CLI, …). One publish runs at a time (`resource_group`).

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.7/templates/publish.yml
    integrity: sha256-5R6Py7GDsSz9QUtsh9htai2cu6zZ69WX6lBEITNW7r4=
    inputs:
      post-fetch-script: scripts/upload-schedules.sh
```

**GitHub:** `publish/action.yml`, with `pull-requests: read` (which pull request a commit came from) and
`issues: write` (the note):

```yaml
name: schedule publish

on:
  push:
    branches: [main]
    paths:
      - schedules/*.canned-schedule.md

permissions:
  contents: read

concurrency:
  group: schedule-publish

jobs:
  publish:
    runs-on: ubuntu-26.04
    timeout-minutes: 10
    permissions:
      contents: read
      pull-requests: read
      issues: write
    steps:
      - uses: actions/checkout@<commit SHA> # <release tag>
        with:
          fetch-depth: 0 # The diff needs the commit before the push.
          persist-credentials: false
      - uses: iilei/canned-schedules-companion/publish@<commit SHA> # <release tag>
        env:
          ARTIFACTORY_TOKEN: ${{ secrets.ARTIFACTORY_TOKEN }}
        with:
          post-fetch-script: scripts/upload-schedules.sh
```

| Input               | Meaning                                                                                        | Default                                                |
| ------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| `version`           | the companion release to install                                                               | this wrapper's release                                 |
| `catalog-dir`       | the catalog's folder; only its `*.canned-schedule.md` files are published                      | `schedules`                                            |
| `args-file`         | the flags to render with, read from the pushed commit                                          | `canned-schedules.args`                                |
| `path-template`     | each schedule's folder                                                                         | `{slug}/{mr}-{digest \| afterLast ":" \| truncate 12}` |
| `metadata`          | one `key=value` template per line; empty: the default keys                                     |                                                        |
| `force-overwrite`   | let a later schedule overwrite an earlier one's file                                           | `false`                                                |
| `post-fetch-script` | the script in the repository; empty: GitLab keeps the output as artifacts, GitHub in `out-dir` |                                                        |
| `note`              | keep the note on the merge request for schedules published without review                      | `true`                                                 |
| `dry-run`           | read and lay out, but run no script and post no note                                           | `false`                                                |

GitLab also takes `stage` (default `deploy`), `image` and `api-url`; GitHub `token` and `comment-author`, and has
the output `out-dir`. Tested end to end on a local GitLab 19.4.1 against Artifactory OSS: a reviewed merge request
publishes with `reviewed=true` and no note; one changed after its review publishes with `reviewed=false` and the note;
a direct push publishes nothing. The action ran the same way on the demo repo
[`canned-schedules-demo`](https://github.com/iilei/canned-schedules-demo/actions/workflows/schedule-publish.yml).

## GitLab CI/CD component

`templates/issue-preview.yml` checks an issue that proposes a schedule and keeps one note on it up to date, with the week shown
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
     - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.7/templates/issue-preview.yml
       integrity: sha256-oAkdpOZr/5fW4KWUrt+CFQvE+zSM2SklO/XSJu4hqSs=
   ```

   GitLab rejects the file if it doesn't match `integrity`. Inputs go under `inputs:` of the same entry.

The job runs when an issue is opened or reopened and when its description changes, and checks it when the description
holds a schedule directive (`[!SCHEDULE`, any case) or the issue has the label. It reads the text from the API, not from
the event. It adds the label to an issue with a valid schedule and removes it otherwise; label changes alone don't start
it, so its own label doesn't either. A proposal with errors gets its note, and the job then fails. The
job keeps its files as artifacts under `canned-schedule-preview/`, for further jobs of your own. They include the proposal
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
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.7/templates/playground.yml
    integrity: sha256-yGLsukrc4Pp4wSO4HjKwHcfsgI7Ec6RC407sc6suUGM=
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

`playground/action.yml` builds a [self-hosted playground](#playground-build) for the catalog and uploads it for GitHub
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

`review/action.yml` runs [`review render` and `review post`](#review-render--review-post) on a pull request that
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
      pull-requests: write # Renames the pull request.
      issues: write # Comments and labels go through the issues API.
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

## Using the JSON

Every valid schedule produces `schedule.json`: the canonical week, its transitions with cron expressions, and the rules
the schedule was checked against. The [canned-schedules](https://github.com/iilei/canned-schedules) repository shows how
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
