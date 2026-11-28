# Tokens and permissions

What each step needs from the forge, no more. The tables come from tests against the forges (described under each),
not from their documentation, which doesn't say for every endpoint.

## On GitHub

The actions use the workflow token (`github.token`). Grant these on the job; the workflow's top level can stay at
`contents: read`.

| Step          | Action                  | Job permissions                                                      |
| ------------- | ----------------------- | -------------------------------------------------------------------- |
| issue preview | `action.yml`            | `contents: read`, `issues: write`                                    |
| playground    | `playground/action.yml` | `contents: read` (the deploy job: `pages: write`, `id-token: write`) |
| review        | `review/action.yml`     | `contents: read`, `pull-requests: write`                             |
| promotion     | `promote/action.yml`    | `contents: write`, `pull-requests: write`, `issues: write`           |
| publishing    | `publish/action.yml`    | `contents: read`, `pull-requests: write`                             |

- **A pull request's comments and labels need `pull-requests: write`.** They go through the issues API, but
  `issues: write` alone gets 403 when the workflow token comments on or labels a pull request; `pull-requests: write`
  allows both. The other way round, `pull-requests: write` doesn't cover issues. So the issue preview needs
  `issues: write`, the review and publishing `pull-requests: write`, and promotion both (it opens the pull request and
  takes the label off the issue).
- **Promotion** also needs Settings > Actions > General > "Allow GitHub Actions to create and approve pull requests".
- **Reading** (the issue, the pull request, the commit's pull requests, files and branches) worked with
  `contents: read` alone in the public test repository; in a private one, each write permission above includes the
  read it stands for.

Checked on 2026-11-28 with a workflow on the demo repository that called every endpoint the companion uses, once per
permission set, with bodies GitHub refuses: 403 means the permission is missing, 422 that it is there.

## On GitLab

The components read a **project access token** with the `api` scope from a CI/CD variable, `CANNED_SCHEDULES_TOKEN`
unless the input `token-variable` names another. `CI_JOB_TOKEN` can read an issue but can't write notes, labels or
uploads.

| Step          | Component                     | Role, at least |
| ------------- | ----------------------------- | -------------- |
| issue preview | `templates/issue-preview.yml` | Planner        |
| playground    | `templates/playground.yml`    | no token       |
| review        | `templates/review.yml`        | Developer      |
| promotion     | `templates/promote.yml`       | Developer      |
| publishing    | `templates/publish.yml`       | Planner        |

- **Guest** can write notes and upload files, but can't set labels or see confidential issues, and gets 403 for merge
  requests in a private project.
- **Planner and Reporter** label issues and write notes on issues and merge requests, but get 403 when they label or
  rename a merge request, push a branch or open a merge request. So the review and promotion need **Developer**.
  Developer can't push to a protected branch: protect the default branch, so the promotion's token can only open
  merge requests.
- **`read_api`** is enough for dry runs, and for publishing with `note: false`.
- **One token or several:** a single Developer token in `CANNED_SCHEDULES_TOKEN` runs every step. To keep the issue
  preview, which reads anyone's issue text, and publishing, whose post-fetch script runs with the job's variables, on
  less, give them a Planner token of their own:

  ```yaml
  include:
    - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/<tag>/templates/issue-preview.yml
      integrity: sha256-…
      inputs:
        token-variable: CANNED_SCHEDULES_PLANNER_TOKEN
  ```

  Each token is its own bot user, and a bot only updates its own notes: give a step a new token and it writes a new
  note next to the old one (see [rotation](gitlab.md#gitlab-cicd-component)).

Checked on GitLab CE 19.4.1 on 2026-11-28 with one project access token per role (Guest, Planner, Reporter,
Developer) against every call the components make, in a private project.
