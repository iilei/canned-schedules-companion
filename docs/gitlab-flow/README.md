# The GitLab flow in screenshots

What contributors and reviewers see in a project that
[includes the template](../gitlab.md#gitlab-cicd-component). The screenshots show companion `v0.1.3` on a local GitLab.
Runner ids and token prefixes are blurred in the job log.

## 1. Open an issue

The contributor fills in a schedule. The `/label ~Schedule` line at the end of the description adds the label that starts the
check. GitLab lists issues with a similar title above the description.

![The new issue form with a schedule in the description](01-issue-form.png)

## 2. The issue exists

The issue has the `Schedule` label, and the webhook has started a pipeline.

![The created issue with the Schedule label](02-issue-created.png)

## 3. The check answers on the issue

The bot's note has the report, the week as text and the week as a picture with its cron expressions.

![The bot's note with no errors, the text preview and the week](03-bot-note.png)

## 4. The pipeline

One job, started by the trigger.

![The passed pipeline with its canned-schedule-preview job](04-pipeline.png)

## 5. The job

The log names the note it created or updated.

![The job log of the passed job](05-job-log.png)

## 6. The files

The job keeps the report, the schedule's outputs and the request as submitted, named after where it came from.

![The job's artifacts](06-artifacts.png)

## 7. An edit updates the same note

Changing the description checks the schedule again. Here `18:00` became `25:00`: the note now shows the error with its position
and an example, and there is still only one note.

![The same note, now reporting an invalid time](07-note-with-error.png)

## 8. A request with errors fails the pipeline

The note reaches the issue first, and the job then fails.

![The failed pipeline](08-pipeline-failed.png)
