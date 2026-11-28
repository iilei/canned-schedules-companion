# canned-schedules-companion

The lifecycle around [canned-schedules](https://github.com/iilei/canned-schedules) in a schedule catalog: feedback on
a proposed schedule, a playground for the catalog, a review of every merge request, promotion of an approved issue,
and publishing what a merge request merged, tied to its review by digest. Housekeeping is planned.

## Design

- **One binary, local first.** Every step has a local part that reads and writes files only (no network, no tokens)
  and a thin forge part that posts, opens or uploads. The local part runs the same on a laptop, in any CI and with
  any forge; GitHub and GitLab get small wrappers that install the binary and run it.
- **Commands are `<step> <verb>`.** The verb says whether it writes to a forge:

  | Step          | Local (files only) | Forge                           |
  | ------------- | ------------------ | ------------------------------- |
  | issue preview | `preview render`   | `preview post`                  |
  | playground    | `playground build` |                                 |
  | review        | `review render`    | `review post`                   |
  | promotion     | `promote prepare`  | `promote open`                  |
  | publishing    | `publish prepare`  | `publish fetch`, `publish post` |
  | housekeeping  | `housekeep check`  | `housekeep retire` (planned)    |

  Forge verbs that write support `--dry-run`, and all take the forge explicitly (`--forge github` or
  `--forge gitlab`).
- **Built on the canned-schedules Go library** (`github.com/iilei/canned-schedules`), pinned in `go.mod`. Its
  results are the CLI's results, byte for byte.

:::{toctree}
:caption: Commands
:hidden:

preview
playground
review
promote
publish
:::

:::{toctree}
:caption: In CI
:hidden:

gitlab
github
permissions
publishing
gitlab-flow/README
:::

:::{toctree}
:caption: Recipes
:hidden:

json
png
development
:::
