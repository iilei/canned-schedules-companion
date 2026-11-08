# canned-schedules-companion

The lifecycle around [canned-schedules](https://github.com/iilei/canned-schedules): it turns a schedule request (an
issue written from the template) into feedback on that issue, and later moves approved requests to the catalog
("shelf"), publishes them and keeps them tidy.

**Work in progress.** Step 1, issue feedback, has its local part: `preview render`.

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

  Only `preview render` exists so far. Forge verbs will support `--dry-run` and take the forge explicitly
  (`--forge github|gitlab`).
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

Flags with the canned-schedules CLI's names and meaning: `--sample-interval`, `--require-base true|false`, `--base`,
`--error-unless-alert-pattern`, `--set-config-at` / `--set-config-value` (repeatable pairs) and
`--preview-downsample N`.

Exit codes: 0 valid, 1 the request has errors (the files are still written), 2 unusable command line or input.

## Development

```sh
mise run check   # format check, vet, lint, Markdown lint, tests (race detector), build
```

## License

[FSL-1.1-ALv2](LICENSE.md), like canned-schedules: any use except a competing commercial product or service; each
release becomes Apache-2.0 two years after it is published. The binary includes canned-schedules, under the same
licence.
