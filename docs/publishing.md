# Publishing on GitLab and GitHub

Both wrappers run on a **push to the default branch** that changes `catalog-dir/*.canned-schedule.md`:
[`publish fetch`](publish.md#publish-fetch), [`publish prepare`](publish.md#publish-prepare) with the args file of the pushed commit and
the schedule files the push adds or changes, then the team's **post-fetch script** with the output directory, then
[`publish post`](publish.md#publish-post). A direct push (no merged merge request) fails the job and publishes nothing, so
protect the default branch. `dry-run` reads and lays out everything, but runs no post-fetch script and posts no note.

The post-fetch script is a file in the repository, run with the output directory as its argument; it uploads, and
should fail when the upload fails. Its secrets are the job's: CI/CD variables on GitLab, the `env` of the step that
uses the action on GitHub.

**Upload scripts.** Two maintained scripts upload `publish prepare`'s output as it is meant to be stored. Copy the one
you need from the release you pin into your repository (it runs with your secrets, so it belongs under your own
review), e.g. as `scripts/upload-schedules.sh`:

- [`publish/upload-artifactory.sh`](https://github.com/iilei/canned-schedules-companion/blob/master/publish/upload-artifactory.sh): every file to
  `$ARTIFACTORY_URL/$ARTIFACTORY_REPO/[$ARTIFACTORY_PATH/]<path>`, with the schedule's metadata as properties and all
  three checksums from the manifest, so Artifactory verifies each upload (and flags none for lacking SHA-1 and MD5).
  Credentials: `ARTIFACTORY_TOKEN`, or `ARTIFACTORY_USER` and `ARTIFACTORY_PASSWORD`; they reach curl as a header
  file, never on its command line.
- [`publish/upload-s3.sh`](https://github.com/iilei/canned-schedules-companion/blob/master/publish/upload-s3.sh): every file to `s3://$S3_BUCKET/[$S3_PREFIX/]<path>` with its media
  type, the digest as the checksum S3 verifies, and the metadata as `x-amz-meta-*`; the AWS CLI's own configuration
  for credentials, region and `AWS_ENDPOINT_URL`.
- **They never overwrite,** so they suit immutable repositories and write-once buckets: a file already there with
  the same SHA-256 is skipped (a rerun of the job passes), one with another SHA-256 fails the upload. Every publish
  adds new paths only; consumers find a schedule's newest publish by its metadata (`schedule`, `merged-at`), for
  example with an Artifactory AQL query, rather than through a moving `latest` copy.

Both need `jq` (installed with `apk` in the components' Alpine image; `curl` too) and check that the manifest is
version 1. They were run against Artifactory OSS 7.104 and an S3 emulator, and a test runs them with stand-ins for
`curl` and `aws` on every change.

**GitLab:** `templates/publish.yml` needs a project access token with the Planner role (`read_api` with `note: false`)
in `CANNED_SCHEDULES_TOKEN` or the variable `token-variable` names, and runs the script in its `image`,
which needs the script's tools (`curl`, `jq`, the AWS CLI, …). One publish runs at a time (`resource_group`).

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.10/templates/publish.yml
    integrity: sha256-S3MOQ7meUW/AUPh4PlgfEMZhFXL4tYzYgaslDFKc+Ns=
    inputs:
      # a copy of publish/upload-artifactory.sh
      post-fetch-script: scripts/upload-schedules.sh
```

with the CI/CD variables `ARTIFACTORY_URL`, `ARTIFACTORY_REPO` and `ARTIFACTORY_TOKEN` (masked).

**GitHub:** `publish/action.yml`, with `pull-requests: write` (which pull request a commit came from, and the note;
`issues: write` doesn't let the workflow token comment on a pull request):

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
      pull-requests: write
    steps:
      - uses: actions/checkout@<commit SHA> # <release tag>
        with:
          fetch-depth: 0 # The diff needs the commit before the push.
          persist-credentials: false
      - uses: iilei/canned-schedules-companion/publish@<commit SHA> # <release tag>
        env:
          ARTIFACTORY_URL: https://acme.jfrog.io/artifactory
          ARTIFACTORY_REPO: schedules-local
          ARTIFACTORY_TOKEN: ${{ secrets.ARTIFACTORY_TOKEN }}
        with:
          # a copy of publish/upload-artifactory.sh
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

GitLab also takes `stage` (default `deploy`), `image`, `api-url` and `token-variable`; GitHub `token` and `comment-author`, and has
the output `out-dir`. Tested end to end on a local GitLab 19.4.1 against Artifactory OSS: a reviewed merge request
publishes with `reviewed=true` and no note; one changed after its review publishes with `reviewed=false` and the note;
a direct push publishes nothing. The action ran the same way on the demo repo
[`canned-schedules-demo`](https://github.com/iilei/canned-schedules-demo/actions/workflows/schedule-publish.yml).
