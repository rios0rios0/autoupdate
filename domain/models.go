package domain

// Repository represents a Git repository on any hosting provider.
type Repository struct {
	ID            string
	Name          string
	Organization  string
	Project       string // Used by Azure DevOps; empty for GitHub/GitLab
	DefaultBranch string
	RemoteURL     string
	SSHURL        string
	ProviderName  string
}

// File represents a file entry within a repository.
type File struct {
	Path     string
	ObjectID string
	IsDir    bool
}

// Dependency represents a versioned dependency found in a repository.
type Dependency struct {
	Name       string // Dependency name or module label
	Source     string // Source URL/path (without version ref)
	CurrentVer string // Currently pinned version
	LatestVer  string // Latest available version
	FilePath   string // File where this dependency was found
	Line       int    // Line number in the file
}

// FileChange represents a file modification to be included in a commit.
type FileChange struct {
	Path       string
	Content    string
	ChangeType string // "add", "edit", "delete"
}

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

// UpdateOptions holds runtime options passed to updaters.
type UpdateOptions struct {
	DryRun       bool
	Verbose      bool
	TargetBranch string
	AutoComplete bool
}
