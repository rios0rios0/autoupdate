package entities

// BranchInput contains the data needed to create a branch with file changes.
type BranchInput struct {
	BranchName    string
	BaseBranch    string
	Changes       []FileChange
	CommitMessage string
}

// PullRequestInput contains the data needed to create a pull request.
type PullRequestInput struct {
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	AutoComplete bool
}

// PullRequest represents a pull/merge request returned by a provider.
type PullRequest struct {
	ID     int
	Title  string
	URL    string
	Status string
}
