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
}
