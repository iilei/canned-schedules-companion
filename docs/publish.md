# publish fetch, prepare and post

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
