# Agent steering — canned-schedules-companion (work in progress)

Read this before changing anything. The core project's `CLAUDE.md` (`../canned-schedules/CLAUDE.md`, section
"Companion tooling") holds the decisions this repo follows; this file adds what is specific to here. Update both when
a decision changes.

## Goal

The lifecycle steps around canned schedules (issue #38 in the core repo): 1 issue feedback, 2 promotion of an approved
request to the shelf, 3 publishing, 4 housekeeping. The core stays pure (no network, no forge flags); everything that
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
  `<!-- canned-schedules:issue-preview -->`, the Markdown report and, when valid, the text form in a tilde fence of
  at least 8 (longer when the text holds a longer run). The example will call this binary instead of its shell.
  `preview render` removes the files of an earlier run first.
- **Evaluation flags keep the core CLI's names and meaning** (`--require-base true|false` takes a value), so a
  `canned-schedules.args` line works here too.
- **Exit codes:** 0 valid, 1 request errors (files still written), 2 usage or environment.
- **`preview post --forge github` (implemented 2026-11-08):** `internal/forge/github` with plain `net/http` (no
  dependency): lists the issue's comments (100 per page), takes those starting with the marker and written by
  `--author` (default `github-actions[bot]`), updates the oldest, deletes the others, or creates one. Everything is
  a flag (`--repo`, `--issue`, `--api-url`, `--author`); only the token comes from the environment, from the
  variable `--token-env` names (default `GITHUB_TOKEN`). `--dry-run` reads but never writes. The body comes from
  `preview.Body`: `comment.md` plus a line of `--link label=https://…` (parentheses percent-encoded, labels plain
  text) and `--note`; over GitHub's 65,536 characters it falls back to the marker, a short notice and the links.
  Exit 2 for API failures, like any unusable environment. Tested against a fake API (`httptest`); a dry run against
  the real one read core issue #38.
- **Playground link, opt-in only (decided 2026-11-08):** previews are CI's job, so nobody has to wonder where a
  request's data lands; the public Pages site is never linked by default. A team that hosts the preflight page
  itself opts in with `preview render --fiddle-link-base-uri <https URI>`: `comment.md` then ends with
  "Try this schedule in the playground and see its week", linking to the base plus `#doc=` and the base64url
  document. The label names the playground
  (the full preflight page with its editor), not a timetable view, which doesn't exist. Valid requests travel in
  canonical form (no prose), invalid ones as written, so the playground marks their errors; one label for both. The
  base must be `https` with a host, no user info and no `#` (the fragment is the document); a query stays. A link
  over `MaxFiddleLink` (32 KiB) gives way to a notice. Base64url without padding, as the page writes and reads it
  (checked with the page's own `fromHash` logic). Without the flag, `comment.md` stays byte-identical to the core
  example's `schedule:check`.
- **On GitHub, a binary can't upload workflow artifacts:** the action runs `preview render`, then
  `actions/upload-artifact`, then `preview post` with the links.
- **Licence: FSL-1.1-ALv2**, like the core; the binary includes the core anyway.
- **Same conventions as the core:** mise tasks only in CI, actions pinned to SHAs, mise itself pinned in every
  `jdx/mise-action` step, runner labels pinned to explicit images (`ubuntu-26.04`, `macos-26`, `windows-2025`),
  versions at least two weeks old, the strict `.golangci.yml`, conventional commits.

## Open

1. The composite action at the repo root (`preview render` → `actions/upload-artifact` → `preview post` with the
   `artifact-url`s); it needs the release pipeline (item 2) or a build from the action's checkout.
2. The release pipeline (goreleaser, signing, attestations, smoke tests) like the core's; needs the GPG secrets in a
   `release` environment.
3. The core's `examples/consumer` switches to this binary.
4. GitLab: `preview post --forge gitlab` (uploads API, notes), the CI/CD component in a GitLab project.

## Layout

- `cmd/canned-schedules-companion`: the binary, flag parsing, input reading, atomic writes, exit codes.
- `internal/preview`: step 1's local part (`Render`, `Body`, `ParseLink`, `ParseFiddleBase`, `FiddleLink`), pure.
- `internal/forge/github`: the only code that talks to GitHub.
- `testdata/`: request inputs (`office-hours.md` is the core's GitLab issue template).
