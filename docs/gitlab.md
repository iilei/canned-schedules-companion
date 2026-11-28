# On GitLab

## GitLab CI/CD component

`templates/issue-preview.yml` checks an issue that proposes a schedule and keeps one note on it up to date, with the week shown
inline. GitLab can't start a pipeline from an issue event by itself, so a webhook calls a pipeline trigger.

1. **Create a project access token** in the project that holds the issues, with the `api` scope and the Planner role
   (Guest can't set labels; [Tokens and permissions](permissions.md) lists every step's role).
2. **Store it** as the CI/CD variable `CANNED_SCHEDULES_TOKEN`, masked (the input `token-variable` names another). Before it expires, **rotate** it and store the
   new value. Rotation keeps the token's bot user, so the bot goes on updating its notes. A newly created token is a
   new user: it can't edit the old bot's notes, so each issue would get a second note.
3. **Create a pipeline trigger token** under Settings > CI/CD > Pipeline trigger tokens.
4. **Add a webhook** under Settings > Webhooks, with **Issues events** (and **Confidential issues events** if you use
   them) and this URL:

   ```text
   https://gitlab.example.com/api/v4/projects/<project ID>/ref/<default branch>/trigger/pipeline?token=<trigger token>
   ```

5. **Include the template** in the project's `.gitlab-ci.yml`, directly from this repository's release tag:

   ```yaml
   include:
     - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.10/templates/issue-preview.yml
       integrity: sha256-kDbyBrleez66PbBOwatNj4CCBC+WRnSTXQJ1XABjkYM=
   ```

   GitLab rejects the file if it doesn't match `integrity`. Inputs go under `inputs:` of the same entry.

The job runs when an issue is opened or reopened and when its description changes, and checks it when the description
holds a schedule directive (`[!SCHEDULE`, any case) or the issue has the label. It reads the text from the API, not from
the event. It adds the label to an issue with a valid schedule and removes it otherwise; label changes alone don't start
it, so its own label doesn't either. A proposal with errors gets its note, and the job then fails. The
job keeps its files as artifacts under `canned-schedule-preview/`, for further jobs of your own. They include the proposal
as GitLab stores it, named after its origin, for example `acme-catalog-issue-12.md` or `acme-catalog-task-13.md`.

[The GitLab flow in screenshots](gitlab-flow/README.md) shows each step, from the new issue to the updated note.

| Input                  | Meaning                                                                       | Default                  |
| ---------------------- | ----------------------------------------------------------------------------- | ------------------------ |
| `stage`                | the job's stage                                                               | `test`                   |
| `version`              | the release to install; its signature and checksum are verified               | this file's              |
| `image`                | the job's image; one with the binary, `curl` and `jq` skips the download      | `alpine:3.22`            |
| `label`                | added when the schedule is valid, removed when not; empty leaves labels alone | `Schedule`               |
| `api-url`              | the REST API as a job reaches it                                              | `$CI_API_V4_URL`         |
| `token-variable`       | the CI/CD variable with the project access token                              | `CANNED_SCHEDULES_TOKEN` |
| `args`                 | evaluation flags, one argument per line, as in `canned-schedules.args`        |                          |
| `fiddle-link-base-uri` | a preflight page you host, linked from the note                               | no link                  |
| `expire-in`            | how long the job's files are kept                                             | `30 days`                |
| `dry-run`              | says what would change, but writes nothing                                    | `false`                  |

## GitLab Pages playground

`templates/playground.yml` publishes a [self-hosted playground](playground.md#playground-build) for the catalog on GitLab Pages, on
every pipeline of the default branch, so `site.json` lists the catalog's schedules as they are after each merge. It
installs the companion, downloads the core's site zip (by default the version the companion is built with, so the
playground checks as the issue preview does), verifies both with GPG against the release key, runs
`playground build` and publishes `public/`. It needs no token and no webhook.

```yaml
include:
  - remote: https://raw.githubusercontent.com/iilei/canned-schedules-companion/refs/tags/v0.3.10/templates/playground.yml
    integrity: sha256-h/hKdJ6MacBMXL40HqPm7aQT2IdnIRAMRgn9LPB3Mrg=
    inputs:
      name: Acme schedules
      fork: true
```

GitLab Pages must be enabled on the instance and the project. The job is named `pages`, so a project can't have a
`pages` job of its own next to it. The page's proposal targets point at `server-url` (default `$CI_SERVER_URL`), which
must be https.

| Input          | Meaning                                                                        | Default                 |
| -------------- | ------------------------------------------------------------------------------ | ----------------------- |
| `stage`        | the job's stage                                                                | `deploy`                |
| `version`      | the companion release to install                                               | this file's             |
| `site-version` | the canned-schedules release whose page to publish                             | the companion's core    |
| `image`        | the job's image; one with the binary, `curl` and `gnupg` skips the downloads   | `alpine:3.22`           |
| `server-url`   | the project's web address as readers reach it (https)                          | `$CI_SERVER_URL`        |
| `branch`       | the branch proposals target                                                    | `$CI_DEFAULT_BRANCH`    |
| `catalog-dir`  | the catalog's folder                                                           | `schedules`             |
| `args-file`    | the flags your CI checks with; a missing file means none                       | `canned-schedules.args` |
| `name`         | shown in the page's header                                                     |                         |
| `fork`         | link readers who aren't members to the fork page                               | `false`                 |
| `expire-in`    | how long the job's files are kept                                              | `30 days`               |

`site.json` is as public as the Pages site; for a private catalog, set the Pages site's visibility to project members
(Settings > General > Visibility > Pages).
