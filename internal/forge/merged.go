package forge

import (
	"errors"
	"time"
)

// ErrNotMerged means no merged merge request has the commit as its result: a direct push, or a merge request that
// isn't merged yet.
var ErrNotMerged = errors.New("no merged merge request (pull request) has this commit as its result")

// Merged is a merged merge request (pull request) as publishing records it, from the forge's own records, never git's.
type Merged struct {
	CreatedAt time.Time
	MergedAt  time.Time
	// Host is the forge's web host, from the merge request's URL.
	Host string
	// Project is GROUP/PROJECT on GitLab, OWNER/REPO on GitHub.
	Project      string
	URL          string
	Title        string
	Author       string
	MergedBy     string
	TargetBranch string
	// MergeCommit is the commit the merge produced, for reference only.
	MergeCommit string
	Number      int
}
