package entities

// UpdateOptions holds runtime options passed to updaters.
type UpdateOptions struct {
	DryRun       bool
	Verbose      bool
	TargetBranch string
	AutoComplete bool
	// AllowMajorUpdates lets an upgrade cross a major version boundary. Resolved
	// from the settings by MajorUpdatesAllowed, which defaults it to true, so a
	// zero-valued UpdateOptions is the *restrictive* case -- construct it through
	// the run command rather than by hand where the distinction matters.
	AllowMajorUpdates bool
	// ToolingDir is where the scripts an updater runs keep their package managers'
	// caches, downloads and temporary files. A batch run gives each repository its
	// own and removes it with the repository, so nothing a repository needed
	// outlives it. Empty leaves the scripts on the process environment.
	ToolingDir string
}
