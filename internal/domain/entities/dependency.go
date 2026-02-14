package entities

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
