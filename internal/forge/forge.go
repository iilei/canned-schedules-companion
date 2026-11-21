// Package forge holds what the forge clients share: one marked comment per issue, kept up to date.
package forge

// Actions that a client takes on the marked comment.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
)

// Plan is what a client did, or would do with DryRun.
type Plan struct {
	// Action is ActionCreate or ActionUpdate.
	Action string
	// URL is the comment's page; empty for a create in a dry run.
	URL string
	// Existing is the body of the comment to update, as the forge returned it (empty for a create).
	Existing string
	// Delete lists further marked comments by the author, removed as duplicates.
	Delete []int64
	// Update is the comment updated (0 for a create).
	Update int64
}

// MergeRequest is what a step reads from a merge request (a pull request on GitHub).
type MergeRequest struct {
	// Title as the forge shows it.
	Title string
	// SHA is the head commit the merge request points at now.
	SHA string
	// Labels are the label names.
	Labels []string
}

// Issue is what a step reads from an issue: its text and its labels.
type Issue struct {
	// Body is the description as the forge stores it.
	Body string
	// Labels are the label names.
	Labels []string
}

// Upsert describes one comment to keep up to date.
type Upsert struct {
	// Repo is the project: OWNER/REPO on GitHub, GROUP/PROJECT or the numeric ID on GitLab.
	Repo string
	// Marker starts the comment; only comments that start with it count.
	Marker string
	// Author is the login whose comments count, so a pasted marker by someone else is never touched.
	Author string
	// Body is the full comment, marker included.
	Body string
	// Issue is the issue number as the forge shows it, or the merge request's with MergeRequest.
	Issue int
	// MergeRequest makes Issue a merge request (GitLab notes live under /merge_requests/; GitHub's pull requests take
	// comments through the issues API anyway).
	MergeRequest bool
	// DryRun reads but never writes.
	DryRun bool
}
