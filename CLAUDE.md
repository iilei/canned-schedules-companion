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
    deliberately unsupported. Permissions: `contents: read`, `pull-requests: write` (title), `issues: write`.
  - `playground/action.yml` (2026-11-21): mise installs the companion; the site zip is fetched and verified with GPG
    as in `templates/playground.yml`; `playground build --forge github`, then `upload-pages-artifact`. The caller
    deploys with `deploy-pages` in a job of its own (only a job can name the `github-pages` environment). Linux only.
    Live on `canned-schedules-demo` (Pages source "GitHub Actions", enabled 2026-11-21) from the branch commit.
  - `templates/review.yml`: merge request pipelines that change `catalog-dir/*.canned-schedule.md`, `GIT_DEPTH: 0`,
    files from `git diff --diff-filter=ACMR` against `$CI_MERGE_REQUEST_DIFF_BASE_SHA`, flags from the args file at
    that base commit (an MR can't loosen its own check). Needs `CANNED_SCHEDULES_TOKEN` with the Developer role.
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
    checkout, and needs `CANNED_SCHEDULES_TOKEN` with the Developer role.
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
  confidential issues, every role can post on locked and closed issues, and `read_api` only allows `--dry-run`. A
  new project access token can answer 404 for a few seconds, until GitLab has applied its membership.
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

## Open

1. **Step 3, publishing** (designed 2026-11-21 and 2026-11-22 in the core's CLAUDE.md, "Step 3, publishing"; not
   built): the merge request review attaches each changed schedule's generated files to the merge request (job
   artifacts on GitLab, workflow artifacts on GitHub), with each file's digest (`sha256:<hex>`, as Docker writes
   them); on merge, `publish fetch --forge …` finds the merge request the pushed commit came from (works after squash
   and rebase merges) and downloads its latest review files, and `publish prepare` uses a schedule's reviewed files
   when the merged file's digest equals the reviewed one and renders it again otherwise with the published commit's
   args file (a rebase, any later edit, or expired review files; the provenance says which, with both digests). One
   schedule is the unit: a path template (the `site.json` placeholders, every variable allowed, helpers `toLower`,
   `toUpper`, `reverseByDotDelim`, `truncate N`) names each schedule's folder, files keep their catalog names, and
   `manifest.json` always lists the schedules, with metadata from a template of its own, encoded for Artifactory and
   S3. Two schedules writing one file fail the run unless `--force-overwrite`. A user-provided post-fetch script
   runs once over the output (S3, Artifactory, a commit elsewhere). Local part: compare digests, render, write layout,
   provenance, metadata and manifest; forge part: find the merge request, download its review's files. Open
   questions are listed there.
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
- `playground/action.yml`, `review/action.yml`: the GitHub Actions for the playground on Pages and the pull request
  review; `action:version` checks their version defaults with the others.
- `internal/preview`: step 1's local part (`Render`, `Body`, `ParseLink`, `ParseFiddleBase`, `FiddleLink`), pure.
- `internal/forge`: what the forge clients share.
- `internal/forge/github`: the only code that talks to GitHub.
- `internal/forge/gitlab`: the only code that talks to GitLab.
- `testdata/`: proposal inputs (`office-hours.md` is the core's GitLab issue template).
