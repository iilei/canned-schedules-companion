# review render / review post

```sh
canned-schedules-companion review render [flags] schedules/night-shift.canned-schedule.md …
canned-schedules-companion review post --forge github|gitlab --repo OWNER/REPO --merge-request N [flags] dir
```

Reviews a merge request (a pull request on GitHub) that changes the catalog, for example one proposed from the
[self-hosted playground](playground.md#playground-build). `review render` checks each changed schedule file with the core and
the catalog's rule that **a file is named after its schedule** (`Night Shift` → `night-shift.canned-schedule.md`), and
writes one comment: an overview, then each file's report and week. Each valid schedule's week is also written as
`week-N.svg`, and **on GitLab `review post` uploads it and shows it inline** above that schedule's section, as the
issue preview does; on GitHub, which strips inline SVG and has no upload API for bots, the text form stands alone. Paths with characters a catalog file can't have are
reported without being echoed. Exit codes: 0 all valid, 1 problems, 2 unusable input.

**The comment records what it vouches for:** every file that passed is listed with its digest (`sha256:<64 hex>` of the
bytes as reviewed) in its section and in a hidden block under the marker line, which [publishing](publish.md#publish-prepare)
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
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.8/templates/review.yml
    integrity: sha256-unRqeZ46KLQDoXT1DTNSiV4C6fpSfI7cyIjaE5bQpus=
```

Merge requests from forks run their pipelines in the fork, where the variable isn't set; let the project run fork
pipelines in the parent project, or review them by hand. On GitHub, [`review/action.yml`](github.md#github-pull-request-review)
runs both.
