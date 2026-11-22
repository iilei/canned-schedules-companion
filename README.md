# canned-schedules-companion

[![docs](https://img.shields.io/badge/docs-iilei.github.io-0969da)](https://iilei.github.io/canned-schedules-companion/)

The lifecycle around [canned-schedules](https://github.com/iilei/canned-schedules) in a schedule catalog: feedback on
a proposed schedule, a playground for the catalog, a review of every merge request, promotion of an approved issue,
and publishing what a merge request merged, tied to its review by digest. Housekeeping is planned.

One binary, local first: every step has a local part that only reads and writes files, and a thin forge part that
posts, opens or uploads. GitHub Actions and GitLab CI/CD components install the binary and run it.

| Step          | Local (files only) | Forge                           | Docs                                                                             |
| ------------- | ------------------ | ------------------------------- | -------------------------------------------------------------------------------- |
| issue preview | `preview render`   | `preview post`                  | [preview](https://iilei.github.io/canned-schedules-companion/preview.html)       |
| playground    | `playground build` |                                 | [playground](https://iilei.github.io/canned-schedules-companion/playground.html) |
| review        | `review render`    | `review post`                   | [review](https://iilei.github.io/canned-schedules-companion/review.html)         |
| promotion     | `promote prepare`  | `promote open`                  | [promotion](https://iilei.github.io/canned-schedules-companion/promote.html)     |
| publishing    | `publish prepare`  | `publish fetch`, `publish post` | [publishing](https://iilei.github.io/canned-schedules-companion/publish.html)    |

## Documentation

The documentation lives at **[iilei.github.io/canned-schedules-companion](https://iilei.github.io/canned-schedules-companion/)**, built from [`docs/`](docs):

- **In CI:** [GitLab](https://iilei.github.io/canned-schedules-companion/gitlab.html), [GitHub](https://iilei.github.io/canned-schedules-companion/github.html), [publishing from CI](https://iilei.github.io/canned-schedules-companion/publishing.html) with
  upload scripts for Artifactory and S3, and [the GitLab flow in screenshots](https://iilei.github.io/canned-schedules-companion/gitlab-flow/README.html).
- **Recipes:** [using the JSON](https://iilei.github.io/canned-schedules-companion/json.html), [the week as a PNG](https://iilei.github.io/canned-schedules-companion/png.html), [development](https://iilei.github.io/canned-schedules-companion/development.html).
- **Setting up a whole catalog:** [How to host your own schedule catalog](https://iilei.github.io/canned-schedules/self-hosted-catalog.html)
  in the canned-schedules docs.

## License

[FSL-1.1-ALv2](LICENSE.md), like canned-schedules: any use except a competing commercial product or service; each
release becomes Apache-2.0 two years after it is published. The binary includes canned-schedules, under the same
licence.
