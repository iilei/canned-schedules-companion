# playground build

```sh
canned-schedules-companion playground build --site-zip canned-schedules-site_<version>.zip \
  --forge github|gitlab --project OWNER/REPO --branch main [flags] public
```

Sets up a **self-hosted playground** for a catalog: it unpacks the core's preflight page (the zip every
[canned-schedules release](https://github.com/iilei/canned-schedules/releases) attaches) into the directory and writes
its `site.json`. Published on GitHub Pages or GitLab Pages, the page checks every schedule with the catalog's flags,
and a valid schedule goes straight to the forge: **Propose new schedule** opens the new-file page with the file named
after the schedule's title, **Propose change** copies the schedule and opens the edit page of a schedule the catalog has (neither forge
prefills an existing file), **Open as issue** a new
issue (see "Your own playground" in the core's README).

The command is local: it reads files and writes files, nothing else. Download the zip first and verify it (the GPG
signature on `checksums.txt`, then its SHA-256), and publish the directory afterwards; the
[GitHub Action](github.md#github-pages-playground) and the [GitLab component](gitlab.md#gitlab-pages-playground) do both.

| Flag                  | Meaning                                                                                     |
| --------------------- | ------------------------------------------------------------------------------------------- |
| `--site-zip file`     | the core's `canned-schedules-site_<version>.zip`, downloaded and verified (required)        |
| `--forge name`        | where proposals go: `github` or `gitlab` (required, never guessed)                          |
| `--project path`      | `OWNER/REPO`, on GitLab `GROUP/PROJECT` with any subgroups (required)                       |
| `--branch name`       | the branch proposals target, usually the default branch (required)                          |
| `--server-url URL`    | the forge's web address (default: github.com or gitlab.com); https, http only for localhost |
| `--catalog-dir dir`   | the catalog's folder in the repository (default `schedules`)                                |
| `--repo-root dir`     | the checked-out repository, where the catalog is read (default `.`)                         |
| `--no-catalog`        | the catalog has no schedules yet                                                            |
| `--args-file file`    | the flags your CI checks with, as `canned-schedules.args`; the page applies them, locked    |
| `--name text`         | shown in the page's header                                                                  |
| `--fork`              | link readers who aren't members to the project's fork page (GitLab needs a fork for them)   |

`catalog.names` comes from the `*.canned-schedule.md` files in the catalog folder, so the page knows whether a schedule
is new or a change: **run it on every merge to the default branch.** `site.json` is checked with the core before it is
written; the output directory must be new or empty, so no file of an earlier page stays behind.

`site.json` is as public as the Pages site. It holds no secrets, but it lists the catalog's schedules: for a private
catalog, restrict the Pages site (GitLab Pages can be limited to project members; on GitHub it depends on the plan).
Protect the default branch, too: members with push rights could otherwise commit straight to it from the forge's
pages.
