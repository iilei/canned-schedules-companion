# Agent steering — canned-schedules-companion (work in progress)

Read this before changing anything. The core project's `CLAUDE.md` (`../canned-schedules/CLAUDE.md`, section
"Companion tooling") holds the decisions this repo follows; this file adds what is specific to here. Update both when
a decision changes.

## Goal

The lifecycle steps around canned schedules (issue #38 in the core repo): 1 issue feedback, 2 promotion of an approved
proposal to the catalog (built 2026-11-21), 3 publishing, 4 housekeeping. The core stays pure (no network, no forge flags); everything that
talks to a forge lives here.

## Decisions

- **One binary, local first (decided 2026-11-08).** Every step splits into a local part (files in, files out; no
  network, no tokens: the core's guarantees) and a thin forge part. The GitHub Action and the GitLab CI/CD component
  only install the binary and run it. Local use matters: reproducing CI, other forges, teams without an issue bot.
- **Commands are `<step> <verb>`;** the verb says whether it writes to a forge. Local: `render`, `prepare`, `check`.
  Forge: `post`, `open`, `publish`, `retire`. Forge verbs support `--dry-run` and take `--forge github|gitlab`
  explicitly, never guessed from `GITHUB_ACTIONS` or `GITLAB_CI`.
- **The core is imported as a Go library** (`github.com/iilei/canned-schedules`, package `cannedschedules`), never run
  as a subprocess. Its pin in `go.mod` is a decision (Dependabot ignores it); release notes name it. When something
  is missing from the library, it is added there as a compatible change, never worked around here.
- **Step 1 files and layout match the core's `examples/consumer` `schedule:check`:** `report.md`, `report.json`,
  `comment.md`, `schedule.json`, `schedule.txt`, `schedule.svg` (theme auto, no source embed); the comment is
  the stamped marker (below), the Markdown report and, when valid, the text form in a tilde fence of
  at least 8 (longer when the text holds a longer run). Only the marker line differs from `schedule:check`'s
  plain `<!-- canned-schedules:issue-preview -->`. The example will call this binary instead of its shell.
  `preview render` removes the files of an earlier run first.
- **Stamp, label and trigger (implemented 2026-11-21, core CLAUDE.md "Issue trigger and label"):**
  - **The marker line is stamped:** `<!-- canned-schedules:issue-preview sha256=<proposal> tool=canned-schedules-companion@<version>
    args=<options> -->` (`preview.Stamp`). `sha256` reads CRLF as LF (`BodyDigest`), because the text comes from the
    event or the API; `args` hashes the effective options and the playground base (`ArgsDigest`), so spelling a flag
    differently changes nothing. Forge steps match `preview.Marker`, which ends in a space, so old plain markers are
    still found; they have no stamp and are rendered again once.
  - **`preview post` stops early:** it reads the issue first and writes nothing when the text's digest differs from the
    stamp (a newer edit's run posts instead); it leaves a comment with the same stamp alone (on GitLab without
    uploading). A dry-run upsert finds the existing comment first, so the comparison needs no new client method.
  - **`--label`** adds the label for a valid schedule and removes it for an invalid one (`report.json`'s
    `summary.errors`), matched ignoring case as GitHub does. Both forges create a missing label (checked live on
    GitLab CE 19.4.1 with a root token, 2026-11-21: create, up to date, invalid edit, stale run). The action's
    `label` input defaults to `Schedule`; the component's `label` changed meaning from filter to managed label.
  - **Trigger:** the README's workflow runs on `opened`, `reopened`, `edited` when the body contains `[!SCHEDULE`
    (`contains()` ignores case) or the issue has the label. The component runs on open, reopen and description
    changes (no longer on label changes, so its own label can't start it) and checks issues whose description
    matches `[!schedule` (jq, case-insensitive) or that have the label.
  - **Silent without a schedule (2026-11-21, core `v0.3.2`'s `Result.HasDirectives`):** `preview render` exits 0
    for text without a directive (a code span or block doesn't count) and writes the reports and a comment of the
    marker line plus `preview.NoSchedule`, no schedule files. `preview post` never starts a comment with it, replaces
    an earlier one (the schedule was edited away, so no stale preview stays) and removes the label. The action's new
    `schedule` output is false then, and it neither uploads the week nor fails. Checked live on GitLab CE 19.4.1.
  - **Next:** the core's templates and example drop their label once a release has all this.
- **`playground build` (implemented 2026-11-21, core CLAUDE.md "Self-hosted playground and proposals"):** the local
  part of a self-hosted playground. It unpacks the core's `canned-schedules-site_<version>.zip` into a new or empty
  directory (regular files only, no absolute or `..` paths, at most 256 MiB, `index.html` at the root) and writes
  `site.json` (`internal/playground`): the flags from `--args-file`, `catalog.names` from the catalog folder's
  `*.canned-schedule.md` files, URL templates from `--forge`, `--server-url`, `--project` and `--branch` (GitLab:
  new-file page with content, the edit page as `{url, prefill: false}`, `forks/new`; GitHub: new-file page with
  `value`, the edit page as `{url, prefill: false}`, `/fork`; GitLab 19.4's editor loads the stored file through the
  API and drops `?content=`, found 2026-11-21), checked with the core's `CheckSite` before writing. Key order of `site.json` is
  kept readable (fieldalignment exemptions). Downloading and verifying the zip and publishing stay with the wrappers
  (next: the GitLab component with a `pages` job, then the Action); the binary never touches the network. Checked
  with the real `v0.3.3` zip, verified against `checksums.txt` and its signature.
- **GitLab Pages playground (implemented 2026-11-21):** `templates/playground.yml`, a job named `pages` (GitLab
  publishes that job's `public/`) on pipelines of the default branch, so `site.json` follows every merge. It installs
  the companion like the issue preview does, reads the core version from `--version` (`site-version` overrides),
  downloads the core's site zip from that release and verifies both downloads with one helper (`fetch_verified`:
  signature on `checksums.txt` against the pinned fingerprint, then the file's hash; both projects share the release
  key), then runs `playground build --forge gitlab` with `$CI_SERVER_URL`, `$CI_PROJECT_PATH` and
  `$CI_DEFAULT_BRANCH` (inputs), `--no-catalog` when the folder is missing and `--args-file` only when the file
  exists. No token, no webhook. `action:version` checks its default version and the README's include (tag and
  integrity) like the issue preview's. The project can't have its own `pages` job next to it.
- **Merge request review (implemented 2026-11-21, GitLab first):** the step `review`, for merge requests from the
  playground's proposals (core CLAUDE.md, "Self-hosted playground and proposals": the MR pipeline checks the changed
  files, posts the report, sets the label, renames the MR from the H1).
  - `review render [eval flags] file…` (`internal/review`): the core's check per file plus the catalog rule that a
    file's name is `cannedschedules.FileName` of its H1 (core `v0.3.4`); one comment with an overview and a
    `<details>` section per file (report, then the text form for a valid file). Paths outside
    `[A-Za-z0-9._/-]` are reported without being echoed (user data in Markdown). `review.json` carries `valid` and the
    names for the forge part. The marker is `<!-- canned-schedules:merge-request-review` with the issue preview's
    stamp (`preview.Stamp.LineFor`); the body digest covers paths and contents.
  - **The week inline on GitLab (implemented 2026-11-22, the user expected the SVG in a schedule's merge request):**
    `review render` writes each valid file's week as `week-N.svg` (theme auto, as the issue preview's) and puts a
    placeholder line `<!-- canned-schedules:week week-N.svg -->` above its section; `review post` replaces it with
    `![Week as SVG](<upload>)` when the client can upload (GitLab's uploads API, named `preview.UploadName`), and
    leaves it, invisible, on GitHub. Names other than `week-<n>.svg` are refused (no paths). Uploads happen only when
    the note changes (same stamp: nothing written). Checked on the local GitLab (publish-test !1).
  - **The review's digests (implemented 2026-11-22):** the comment's second line opens a hidden block,
    `<!-- canned-schedules:digests`, one `<path> <digest>` line per file that passed (`publish.Digest` of the raw
    bytes), then `-->`; each valid section shows the digest as a `Digest:` line. Files with problems get none: the review
    vouches only for what passed. `review.Digests(comment)` reads the block back for `publish fetch` (CRLF tolerated);
    a missing or malformed block is false, read as no review. No escaping: `safePath` rules out spaces and `-->`.
  - `review post --forge gitlab`: the GitLab client's notes take `forge.Upsert.MergeRequest` (`/merge_requests/`),
    plus `MergeRequest`, merge request labels and `SetMergeRequestTitle`. Skips when the MR's head isn't
    `--head-sha`; leaves a note with the same stamp alone; `--label` as for issues; `--retitle` only while the title
    is the web editor's default (`review.DefaultTitle`: "Add new file", "Add|Update|Create|Edit|Delete
    <…canned-schedule.md>"), to `Schedule: <name>` or `Schedules: a, b`.
  - `review post --forge github` (2026-11-21): the command takes a `reviewForge` interface; the GitHub client adds
    `UpsertNote` (`UpsertComment`: pull requests take comments through the issues API), `MergeRequest` (`GET pulls/N`:
    title, `head.sha`, labels), labels through the issues API and `SetMergeRequestTitle` (`PATCH pulls/N`). Defaults as
    in `preview post`: `GITHUB_TOKEN`, github.com's API, author `github-actions[bot]`; 65,536 bytes per comment.
    Checked live with a local build on `canned-schedules-demo` #10 (as the user's account): comment created, label
    added, "Create night-shift.canned-schedule.md" renamed, a second run up to date.
  - `review/action.yml` (2026-11-21): on `pull_request` only (refuses other events), after a checkout with
    `fetch-depth: 0`; files from `git diff --diff-filter=ACMR base...head`, flags from the args file at the base SHA.
    The binary's path is looked up from a work directory, so mise never reads a `mise.toml` of the checkout, and runs
    in the checkout (files keep their repository paths). The comment also goes to the job summary; a pull request
    from a fork (head repo ≠ repository) stops before posting, since its token is read-only. `pull_request_target` is
    deliberately unsupported. Permissions: `contents: read`, `pull-requests: write` (see "Minimal token scopes").
  - `playground/action.yml` (2026-11-21): mise installs the companion; the site zip is fetched and verified with GPG
    as in `templates/playground.yml`; `playground build --forge github`, then `upload-pages-artifact`. The caller
    deploys with `deploy-pages` in a job of its own (only a job can name the `github-pages` environment). Linux only.
    Live on `canned-schedules-demo` (Pages source "GitHub Actions", enabled 2026-11-21) from the branch commit.
  - `templates/review.yml`: merge request pipelines that change `catalog-dir/*.canned-schedule.md`, `GIT_DEPTH: 0`,
    files from `git diff --diff-filter=ACMR` against `$CI_MERGE_REQUEST_DIFF_BASE_SHA`, flags from the args file at
    that base commit (an MR can't loosen its own check). Needs a Developer token (see "Minimal token scopes").
    Fork MRs run in the fork without the variable.
  - Checked live on the local GitLab (`root/canned-schedules-demo` !1, run from the command line until a release has
    it): note created, label added, "Add new file" renamed to "Schedule: Night Shift", a second run up to date, a
    stale head writes nothing, a misnamed file reported.
- **Evaluation flags keep the core CLI's names and meaning** (`--require-base true|false` takes a value), so a
  `canned-schedules.args` line works here too.
- **Exit codes:** 0 valid, 1 errors in the schedule (files still written), 2 usage or environment.
- **The bundle holds the proposal (decided 2026-11-18):** the action's zip and the GitLab job's artifacts contain the
  submitted text as `<project path with hyphens>-<kind>-<number>.md` (`acme-catalog-issue-12.md`), so a downloaded file
  says where it came from. On GitHub the kind is always `issue`; on GitLab it follows the work item's type. GitLab
  removes quick actions from a description when it stores it, so its copy is the stored text.
- **Promotion, step 2 (decided and implemented 2026-11-21):** an approved issue becomes a pull or merge request that
  adds its schedule to the catalog.
  - **A label approves** (`Schedule approved` by default, chosen by the user over a `/promote` comment and a manual
    run): setting labels takes Triage (GitHub) or Reporter (GitLab), so that permission is the approval, and both
    forges send the change as an issue event the existing triggers already receive.
  - **The issue's text goes in verbatim** (the user's choice over the canonical form, as for the playground's
    `{content}`), with LF line endings and one final newline only (`promote.Normalize`).
  - `promote prepare` (`internal/promote`, local): `cs.Evaluate` with the catalog's flags; `promote.json` (`valid`,
    `schedule`, `name`, `path`, `title`), `schedule.md` (valid only), `report.md`. The path is `catalog-dir/` plus
    `cs.FileName` of the H1; `catalog-dir` follows `site.json`'s rule. Exit 1 without a valid schedule.
  - `promote open` (forge): branch `canned-schedules/issue-<N>` (`--branch-prefix`), created from `--base-branch` with
    the first commit; the file compared with the branch (or the base when the branch is new): same text on the base
    means nothing to promote, same on the branch no commit; then one open request per branch (found or opened),
    titled `review.Title`, its body "Closes #N." Without a valid schedule `--approved-label` is removed and it exits
    1. Clients: `BranchExists`, `File` (GitHub keeps the blob SHA for the update), `CommitFile` (GitHub: create the
    ref, then the contents API; GitLab: one commit with `start_branch`), `OpenRequest`; 404s are `errStatus`.
  - `promote/action.yml` on `issues: labeled` (the caller's `if:` names the label), after a checkout of the default
    branch for the args file. GitHub needs "Allow GitHub Actions to create and approve pull requests", and a pull
    request opened with the workflow token starts no workflows (no review run; the promotion checked the schedule).
    `templates/promote.yml` runs on the issue preview's webhook pipeline and acts only when the event's
    `changes.labels` added the label; it reads the issue's current text through the API, the args file from the
    checkout, and needs a Developer token (see "Minimal token scopes").
- **`publish fetch` (implemented 2026-11-22, step 3's forge part, read-only):** `forge.Merged` from each client's
  `MergedRequest(repo, sha)`: GitLab `repository/commits/:sha/merge_requests`, matching a merged one whose
  `merge_commit_sha`, `squash_commit_sha` or head `sha` (fast-forward) is the commit, `merge_user` before the
  deprecated `merged_by`, the project from `references.full`; GitHub `commits/:sha/pulls`, matching `merge_commit_sha`
  (merge, squash and the last rebased commit), then `pulls/:n` for `merged_by`. Neither found is `forge.ErrNotMerged`,
  exit 1. The review comment comes from a dry-run `UpsertNote` (the comment `review post` keeps), read with
  `review.Digests`; none or no block is `"review": null`. The written file goes through `publish.ParseInput` first.
  `newMergeRequestClient` is shared with `review post`. Checked against the local GitLab 19.4.1 (demo !2, a merge
  commit; its v0.3.1 review reads as no digests).
- **`afterLast "sep"` (added 2026-11-22, the user's choice over a replace helper):** the text after the last
  separator, the whole value without one (`${v##*sep}`); one double-quoted separator without spaces, quotes, braces or
  pipes. Found when a Windows runner couldn't create `42-sha256:…`.
- **Single braces with `{{`/`}}` escapes (decided 2026-11-22):** the user kept `{…}` (as `site.json`) over `{{…}}`,
  which suggests Go templates or Helm and reads as a nested map in unquoted YAML; `{{` and `}}` are a literal brace,
  read left to right (`{{{name}` is `{` plus the name), so paths and metadata can hold braces.
- **Shipped upload scripts (2026-11-22, the user's request: a documented way to Artifactory):**
  `publish/upload-artifactory.sh` and `publish/upload-s3.sh`, POSIX sh, copied by teams into their repository (they
  run with the team's secrets, so they belong under its review), then named as `post-fetch-script`. Artifactory: all
  three checksums from the manifest, the `artifactory` properties, a bearer token or user and password sent as a
  header file (never in argv). S3: media type, the digest as `--checksum-sha256` (xxd, base64), the `s3` metadata
  (left out with a warning when null). No `latest/` copies (the user: Artifactory repositories are immutable): the scripts never overwrite; a path
  already there with the same SHA-256 is skipped (reruns pass), another SHA-256 fails; consumers look up the newest
  publish by metadata (AQL on `schedule` and `merged-at`). `internal/publish/scripts_test.go` runs both on a `Prepare` output
  with stand-ins for curl and aws that record uploads and answer lookups (not there, same, other; skipped on
  Windows or without sh, jq, xxd, base64, sha256sum); both also ran against the local Artifactory and an S3 emulator
  (first run, rerun skipped, changed file refused).
- **Checksums next to the digest (decided 2026-11-22, the user's choice over computing them in scripts):** every
  file in the manifest and the provenance carries `checksums: {sha1, md5}` (hex), because Artifactory flags an
  upload without `X-Checksum-Sha1` and `X-Checksum-Md5` ("Client did not publish a checksum value") and checks all
  three (a wrong one is a 409). They are not digests: nothing matches by them, and `digest` stays `sha256:<hex>`. A
  compatible addition (manifest and provenance stay `"version": 1`). Scripts pass them on; `sha1sum`/`md5sum` differ
  on macOS (`shasum`, `md5`), which the manifest makes moot.
- **The publish wrappers (implemented 2026-11-22):** `templates/publish.yml` and `publish/action.yml` run on a push to
  the default branch that changes catalog files: `publish fetch` (exit 1, a direct push, fails the job), `publish
  prepare` with the pushed commit's args file and `git diff before..after` (`CI_COMMIT_BEFORE_SHA`,
  `github.event.before`; an all-zero or missing before fails), the post-fetch script with the output directory, then
  `publish post`. `dry-run` skips the script (it writes elsewhere) and posts no note. GitLab: one publish at a time
  (`resource_group`), the output as artifacts. GitHub: script secrets through the `env` of the `uses:` step. Both are
  in `action:version`. **End to end on the local GitLab (2026-11-22):** project `root/canned-schedules-publish-test`
  (id 44), the components included with `local:` and a dev image (`canned-schedules-companion:dev`, pull policy
  if-not-present), the README's Artifactory loop as the script, Artifactory joined to `demo-network` for the run:
  reviewed MR !1 → `reviewed=true`, no note, properties and checksum read back (`{{literal}}` as `{literal}`); !2
  changed after its review with `-o ci.skip` → `reviewed=false`, `changed`, the note with both digests, a title with
  `;`, `:` and `ü` intact; a direct push → failed, nothing published. GitLab's `$[[ inputs ]]` carried the default
  path template's quotes and braces unchanged. **On GitHub (v0.3.5, 2026-11-22):** the demo repo's `schedule publish`
  workflow (demo #18; the post-fetch script writes the job summary, the output is an artifact) published PR #19 as
  reviewed, with no note: `commits/:sha/pulls` found the merge commit, the review's digest matched.
- **`publish post` (implemented 2026-11-22):** `publish.NoteFor(manifest)` writes the note (marker
  `<!-- canned-schedules:publish-note -->`, a table of path, reason, published and reviewed digest; paths and the
  merge commit echoed only when plain, digests only when well-formed), empty when every schedule was reviewed: then
  nothing is written. The command upserts it on the merge request the manifest names (`newMergeRequestClient`), leaves
  an identical note alone and exits 0 either way: the note is the signal, not a failure.
- **`publish prepare` (implemented 2026-11-22, step 3's local part, `internal/publish`):**
  - **Always rendered from the file as merged (decided 2026-11-22, revising "use the reviewed files"):** review outputs
    come from the merge request's own pipeline, which the merge request can change, so reusing them could publish
    crafted files under a correct-looking provenance. The review vouches by digest only; a digest that differs, a file
    the review didn't cover and a merge request without a review are published anyway (`reviewed: false`, reasons
    `changed`, `not-reviewed`, `no-review`) and the forge part posts a note on the merge request (the user's choice
    over blocking: git and the published files never disagree). Retention of review artifacts stops mattering.
  - **Input:** `--merge-request-file` (`merge-request.json`: `merge-request` with the forge's facts, `review` with
    `url` and `digests` by path, `null` without a review comment; unknown keys are errors, `number`, `created-at` and
    `merged-at` required, times converted to UTC), the merged files as arguments, the evaluation flags.
  - **Output:** `--out-dir` must be empty or missing. Per schedule a folder from `--path-template` (default
    `{slug}/{mr}-{digest | afterLast ":" | truncate 12}`, without a `:`, which Windows refuses in file names; until
    2026-11-22 it was `{digest | truncate 19}`, `sha256:` included) with `<slug>.canned-schedule.md` (the file as merged), `.json`, `.svg` (theme
    auto, no source embed, as `schedule:check`), `.txt` and `.provenance.json`; `manifest.json` at the root, always a
    list. Both carry `"version": 1`; no JSON Schema yet (add one when a consumer relies on the shape). A schedule with
    errors or a file not named after its H1 is exit 1 with nothing written.
  - **Templates** (`Parse`, `ParseMetadata`): `{var | helper …}`, every variable of `Variables` in both, helpers
    `toLower`, `toUpper`, `reverseByDotDelim`, `truncate N` (runes). No escape for braces (a stray `{` or `}` is an
    error). Checked: unknown names, and a filled path must be relative without empty, `.` or `..` segments,
    backslashes or control characters. Collisions fail unless `--force-overwrite` (the later schedule wins, the earlier
    entry stops listing the file).
  - **Metadata:** default keys `schedule`, `digest`, `reviewed`, `merge-request`, `merged-at`, `merged-by`,
    `project`; keys `^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`, unique, no control characters in values. The manifest has
    `metadata` (map), `artifactory` (matrix parameters, every byte outside RFC 3986's unreserved set percent-encoded;
    verified against the local Artifactory OSS 7.104.12: umlauts, `;`, `,` and `=` read back exactly, with an
    `X-Checksum-Sha256` upload) and `s3` (non-ASCII values RFC 2047 B-encoded; `null` plus a warning above 2 KB).
- **`preview post --forge github` (implemented 2026-11-08):** `internal/forge/github` with plain `net/http` (no
  dependency): lists the issue's comments (100 per page), takes those starting with the marker and written by
  `--author` (default `github-actions[bot]`), updates the oldest, deletes the others, or creates one. Everything is
  a flag (`--repo`, `--issue`, `--api-url`, `--author`); only the token comes from the environment, from the
  variable `--token-env` names (default `GITHUB_TOKEN`). `--dry-run` reads but never writes. The body comes from
  `preview.Body`: `comment.md` plus a line of `--link label=https://…` (parentheses percent-encoded, labels plain
  text) and `--note`; over GitHub's 65,536 characters it falls back to the marker, a short notice and the links.
  Exit 2 for API failures, like any unusable environment. Tested against a fake API (`httptest`); a dry run against
  the real one read core issue #38.
- **`preview post --forge gitlab` (implemented 2026-11-18):** `internal/forge/gitlab` with plain `net/http`, the same
  upsert as GitHub (`internal/forge` holds the shared `Plan` and `Upsert`): oldest marked note by the author updated,
  others deleted, system notes ignored. `--repo` is the project path or numeric ID. `--author` defaults to the
  token's user (`GET /user`), since project access tokens have generated names; `--token-env` to `GITLAB_TOKEN`.
  `schedule.svg` goes through the uploads API and shows inline (`preview.GitLabBody`; a note is limited to 1 MiB in
  bytes, GitHub counts characters); a dry run uploads nothing.
  The token never follows a redirect, because Go keeps `PRIVATE-TOKEN` across hosts. Tested against a fake API and
  live against GitLab CE 19.4.1 (2026-11-18, with a root token): create, update, duplicate removal, the upload byte for
  byte and shown in the browser, and quick actions (`/close`, `/title`, …) inside the comment's tilde fences stay inert, while the same lines
  outside a fence do fire. So user text must only ever reach a note inside a fence.
  **Token: a project access token with `api` and the Planner role** (tested Guest to Maintainer): Guest gets a 404 on
  confidential issues and, since the label (`v0.2.0`), a 403 on labels; every role can post on locked and closed issues, and `read_api` only allows `--dry-run`. A
  new project access token can answer 404 for a few seconds, until GitLab has applied its membership.
- **Minimal token scopes (decided and tested 2026-11-28; the core's "Still open" item):** `docs/permissions.md` holds
  the tables. Tested on GitLab CE 19.4.1 with one project access token per role against every call the components
  make (private project), and on GitHub with a throwaway workflow on the demo repo that called every endpoint once per
  permission set with bodies GitHub refuses (403 = missing, 422 = present; the `X-Accepted-GitHub-Permissions`
  header only comes back to app tokens such as the workflow token, not to `gh auth token`).
  - **GitLab roles:** issue preview Planner, publishing Planner (`read_api` with `note: false`), review and promotion
    Developer (Planner and Reporter get 403 on labelling or renaming a merge request, pushing a branch and opening a
    merge request). Guest can't label (403) or see merge requests in a private project. Planner labels issues, so it
    approves promotions (the docs said Reporter).
  - **`token-variable` input on all four token components** (default `CANNED_SCHEDULES_TOKEN`, regex
    `^[A-Za-z_][A-Za-z0-9_]*$`, re-checked in the script before an `eval` reads the variable), the user's choice
    over documenting one shared token: the issue preview (anyone's issue text) and publishing (the post-fetch script
    sees the job's variables) can then run on a Planner token while review and promotion use a Developer one. Each
    token is its own bot user, so moving a step to another token gives its issues a second note. Checked live
    (review with `DEV_TOKEN`, preview with `PLANNER_TOKEN`; a bad name fails `ci/lint`).
  - **GitHub:** a pull request's comments and labels go through the issues API but need `pull-requests: write`;
    `issues: write` alone is 403 there (and `pull-requests: write` doesn't cover issues). So review and publishing are
    `contents: read` + `pull-requests: write` (publishing's documented `pull-requests: read` + `issues: write` could
    never post its note), the issue preview `issues: write`, promotion `contents: write`, `pull-requests: write`,
    `issues: write`. Deleting a duplicate pull request comment wasn't exercised (GitHub names the same permissions as
    for editing, which passed).
- **Playground link, opt-in only (decided 2026-11-08):** previews are CI's job, so nobody has to wonder where a
  proposal's data lands; the public Pages site is never linked by default. A team that hosts the preflight page
  itself opts in with `preview render --fiddle-link-base-uri <https URI>`: `comment.md` then ends with
  "Try this schedule in the playground and see its week", linking to the base plus `#doc=` and the base64url
  document. The label names the playground
  (the full preflight page with its editor), not a timetable view, which doesn't exist. Valid schedules travel in
  canonical form (no prose), invalid ones as written, so the playground marks their errors; one label for both. The
  base must be `https` with a host, no user info and no `#` (the fragment is the document); a query stays. A link
  over `MaxFiddleLink` (32 KiB) gives way to a notice. Base64url without padding, as the page writes and reads it
  (checked with the page's own `fromHash` logic). Without the flag, `comment.md` stays byte-identical to the core
  example's `schedule:check`.
  - **The link carries the run's options (decided 2026-11-09, core #57):** `FiddleLink(base, doc, opts)` sets
    `?sample-interval=` and `?downsample=` (only when not the default; they replace the base's) and appends the
    other options as `?arg=`, one argument each, after the base's own, so the run's config overrides come later and
    win. Without options beyond the defaults the base stays byte for byte. The page checks `?arg=` itself
    (`internal/web/link.go` in the core); pages built before that ignore it.
  - **Locked by default (decided 2026-11-09, core #58):** `LockFiddle` sets `?lock=1` on the base, so the page
    leaves only the Markdown editable: the requester changes the schedule, CI owns the flags.
    `--fiddle-link-lock=false` unlocks (and removes a `lock` in the base). Not security, since the address can be
    edited.
- **On GitHub, a binary can't upload workflow artifacts:** the action runs `preview render`, then
  `actions/upload-artifact`, then `preview post` with the links.
- **Release pipeline (decided 2026-11-09): the core's, adapted.** `.goreleaser.yml`, `.github/workflows/release.yml`
  and the tasks `licenses`, `release:snapshot`, `release:smoke`, `release:verify` follow the core's (same targets,
  SBOMs, GPG signatures, provenance attestations, smoke tests on six runners, draft until they pass), without
  `api:diff` (no library here). **Signed with the core's release key** (`release-key.asc`,
  `B645234C9CB8175ECA5D78A5C8447A2252E9E7B6`): one key for both projects, so a user checks one fingerprint. The
  release notes name the core version `go.mod` pins (`CORE_VERSION`). `THIRD_PARTY_LICENSES.md` lists the Go modules
  in the binary and the Go standard library: no fonts, since the core's library never links the Sphinx kit (core
  #61). `release:smoke` runs `--version` and `preview render` on `testdata/office-hours.md` and requires all six
  files. Release by pushing a signed tag (`git tag -s vX.Y.Z -m vX.Y.Z && git push origin vX.Y.Z`), never on the
  Releases page; the build job refuses a tag whose release is already published. **`v0.1.0`, the first release
  (2026-11-09):** all smoke tests and `release:verify` passed, and `mise use
  github:iilei/canned-schedules-companion@0.1.0` installs it with the attestation verified. The `release`
  environment (deployments limited to `v*` tags) holds `GPG_PRIVATE_KEY` and `GPG_PASSPHRASE` of the core's key.
- **Composite action at the repo root (implemented 2026-11-18, first released in `v0.1.1`):** `action.yml`, for
  GitHub's issue preview. It
  installs the release through `jdx/mise-action` (attestation verified, no cache), runs `preview render`, uploads
  `schedule.svg` unzipped (valid schedules only) and every file as one zip, runs `preview post`, and fails last when
  the schedule has errors. The caller sets the trigger, the `Schedule` label condition, `concurrency` per issue and
  `permissions: contents: read, issues: write`.
  - **`version` defaults to a literal release, not `github.action_ref` (decided 2026-11-18):** callers pin actions to
    commit SHAs, and a SHA names no release. The PR that prepares a release bumps the default; `action:version`
    fails the release build when it differs from the tag. CI passes the newest published release explicitly, so that
    PR stays green before the tag exists.
  - **Everything runs from a new directory under `RUNNER_TEMP`,** so a `mise.toml` in the caller's checkout can't
    change what gets installed, and reports name the proposal `request.md`, not a path on the runner.
  - **`comment-author` is an input,** because a `token` other than `github.token` writes under another login.
  - Tested in `check.yml` (job `action`) with a valid and an invalid schedule, as dry runs against an existing pull
    request; `lint:actions` runs `actionlint`.
- **Dependabot auto-merge (decided 2026-11-18), as in the core:** on pull requests opened by Dependabot, `check.yml`
  adds `binaries` (`release:snapshot`) and `automerge`, which needs `check`, `test`, `action` and `binaries`, then
  runs `gh pr merge --auto --rebase` with the job's `GITHUB_TOKEN`. `needs` is the gate, so the ruleset's required
  checks stay as they are.
- **GitLab CI/CD component (implemented 2026-11-18):** `templates/issue-preview.yml`, one job. An issue webhook calls a
  pipeline trigger URL, and the job reads the event from `TRIGGER_PAYLOAD`. The event only names the issue: the text and
  labels come from the API, so a forged payload can't supply them. It runs on open, reopen and updates that change the
  description or labels (closing an issue is an update too), one at a time (`resource_group`), without a checkout
  (`GIT_STRATEGY: none`).
  - **Included from the release tag (decided 2026-11-18):** teams use `include: remote` with the raw GitHub URL of
    `refs/tags/<tag>` and an `integrity` hash, because a real `component:` include only works within one GitLab instance.
    The PR that prepares a release updates the tag and the hash in the README, and `action:version` fails the release
    when they don't match. Immutable releases are on, so a published tag can't move (from `v0.1.2`; earlier ones rely
    on the hash) and a published version number can't be reused.
  - **PNG and designs stay out of the component (decided 2026-11-18):** GitLab's designs reject SVG, so they would need
    a PNG, and that is a sidecar for a demo, not part of the check. The upload is named
    `schedule-<issue>-<first 12 characters of the SVG's SHA-256>.svg` and the job keeps `schedule.svg` as an artifact, so
    a later job can convert it and swap the note's link when the hash still matches.
  - **Rotate the token, never replace it (decided 2026-11-18):** rotation keeps the bot user (tested). Only a Maintainer
    can edit or delete another user's note (403 for Planner, Reporter and Developer), so taking over an old bot's notes
    was rejected: it would give a job that reads untrusted text far more rights. Rotating an expired token is untested.
  - `CANNED_SCHEDULES_TOKEN` is a project access token with `api` and the Planner role; `CI_JOB_TOKEN` can read the
    issue but gets 401 for notes and uploads (tested). The download is verified with GPG against the pinned release key,
    then against `checksums.txt`; an image that already has the binary skips it. `action:version` also checks this
    file's default. The catalog project on gitlab.com doesn't exist yet.
- **Licence: FSL-1.1-ALv2**, like the core; the binary includes the core anyway.
- **Same conventions as the core:** mise tasks only in CI, actions pinned to SHAs, mise itself pinned in every
  `jdx/mise-action` step, runner labels pinned to explicit images (`ubuntu-26.04`, `macos-26`, `windows-2025`),
  versions at least two weeks old, the strict `.golangci.yml`, conventional commits.

## Documentation site (decided and built 2026-11-22)

- **The release check reads the docs (2026-11-22):** the GitLab includes with tag and integrity live in `docs/*.md`
  since the README went compact, so `mise run action:version` looks there; the PR that prepares a release updates the
  tags and hashes in those pages.
- **Like the core's (the user's choices):** Sphinx with Furo and MyST, sources in `docs/`, built by `mise run
  docs:build` (`docs/build.sh`) into `dist/docs` with `-W`; `docs/requirements.txt` is the core's file (hash-pinned,
  two-week rule), the virtualenv cached under `~/.cache/canned-schedules-companion/docs-venv`; `python = "3.14"` in
  `mise.toml`. No Sphinx kit: these docs show no schedules. `check.yml` builds it; `pages.yml` publishes it to
  <https://iilei.github.io/canned-schedules-companion/> (Pages source "GitHub Actions", a one-time repo setting).
- **Fonts (the user's choice, 2026-11-28):** Source Serif 4 Black (900 only) for headings and the sidebar brand, Inter
  (variable) for text, Noto Sans Mono (variable; "Noto Mono" is the discontinued predecessor) for code. Served from
  `docs/_static/fonts/`, never a font CDN: the Latin and Latin Extended woff2 subsets of Fontsource's npm packages
  (`@fontsource-variable/inter` 5.3.0, `@fontsource/source-serif-4` 5.3.1, `@fontsource-variable/noto-sans-mono`
  5.3.0), their OFL licences beside them. `docs/_static/fonts.css` holds the `@font-face` rules; `conf.py` sets Furo's
  `font-stack`, `font-stack--headings` and `font-stack--monospace` with system fallbacks.
- **Accents and highlighting (the user's choices, 2026-11-28):** the playground's teal (`#0b7a68` / `#5ad1b8`) for
  links, the sidebar and the search highlight, its violet (`#8250df` / `#c4a5ff`) for visited links, the hover
  underline, the current page and "important" admonitions: Furo variables in `conf.py`, two rules in
  `docs/_static/theme.css` (current page, the playground's focus ring). Everything else stays Furo's. Pygments
  `a11y-light` and `a11y-dark`, one project for both (Furo's dark default, `native`, is unrelated). The playground
  in turn took Noto Sans Mono for its code (core CLAUDE.md, "Fonts"); its colours stay as they are.
- **No flash of the headline font (2026-11-28):** `docs/_templates/base.html` extends Furo's `base.html` and preloads
  the two fonts every page uses (Source Serif 4 Latin 900, Inter Latin normal) in the `linktags` block, which comes
  before the stylesheets; Source Serif 4 has `font-display: block` (headings wait briefly rather than jump), the rest
  `swap`. GitHub Pages fixes `Cache-Control: max-age=600` with an ETag; later visits revalidate with a 304.
- **Code blocks wrap at 90 characters where possible (the user's rule, 2026-11-28):** commands continue with a
  trailing backslash and two spaces of indent, separate commands get a blank line between them, YAML comments wrap
  onto a second comment line, long expressions fold with `>-`, JSON objects split by key. Left long: URLs, including
  the GitLab includes (`action:version` greps those lines), and the image digest in `png.md`.
- **The README** keeps the pitch, the steps with their commands and a map of the docs; the sections moved verbatim to
  pages (one per step, `gitlab.md`, `github.md`, `publishing.md`, `json.md`, `png.md`, `development.md`), headings up
  one level where a page holds one section, anchors rewritten to the page that holds them, repo paths as GitHub URLs.
  Links into the core's README anchors point at the core's docs pages. The stale introduction ("step 1 only", a
  commands table without review and playground) was corrected on the way.

## Open

1. **Step 3, publishing** (designed 2026-11-21 and 2026-11-22 in the core's CLAUDE.md, "Step 3, publishing";
   the commands and both wrappers built, the GitLab component tested end to end on the local GitLab against the local
   Artifactory, the action on the demo repo, released in v0.3.5, see Decisions): next a real catalog uploading for a
   few weeks (the core's item 2), and what it needs.
2. **GitLab catalog, only if teams ask:** a project on gitlab.com for catalog search and the short `component:` syntax.
3. **The screen recording** with the public demo repo
   [`canned-schedules-demo`](https://github.com/iilei/canned-schedules-demo), a copy of the core's `examples/consumer`.
   Its first proposal (issue #1) ran the action for real on 2026-11-18: the comment was created, updated in place for an
   invalid and a fixed edit, and the artifacts were uploaded.

## Layout

- `action.yml`: the composite GitHub Action (step 1).
- `templates/issue-preview.yml`: the GitLab CI/CD component (step 1).
- `templates/playground.yml`: the GitLab component that publishes the self-hosted playground on Pages.
- `cmd/canned-schedules-companion`: the binary, flag parsing, input reading, atomic writes, exit codes.
- `internal/playground`: `playground build`'s local part (`SiteJSON`, `ReadArgs`, `Names`), pure.
- `internal/review`: the merge request review's local part (`Render`, `Title`, `DefaultTitle`), pure.
- `templates/review.yml`: the GitLab component that reviews merge requests.
- `promote/action.yml`, `templates/promote.yml`, `internal/promote`: promotion of an approved issue (step 2).
- `internal/publish`: `publish prepare`'s local part (`ParseInput`, `Parse`, `ParseMetadata`, `Prepare`, `Digest`,
  `Artifactory`, `S3`), pure (step 3).
- `playground/action.yml`, `review/action.yml`: the GitHub Actions for the playground on Pages and the pull request
  review; `action:version` checks their version defaults with the others.
- `internal/preview`: step 1's local part (`Render`, `Body`, `ParseLink`, `ParseFiddleBase`, `FiddleLink`), pure.
- `internal/forge`: what the forge clients share.
- `internal/forge/github`: the only code that talks to GitHub.
- `internal/forge/gitlab`: the only code that talks to GitLab.
- `testdata/`: proposal inputs (`office-hours.md` is the core's GitLab issue template).
