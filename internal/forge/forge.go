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
	// Delete lists further marked comments by the author, removed as duplicates.
	Delete []int64
	// Update is the comment updated (0 for a create).
	Update int64
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
	// Issue is the issue number as the forge shows it.
	Issue int
	// DryRun reads but never writes.
	DryRun bool
}
