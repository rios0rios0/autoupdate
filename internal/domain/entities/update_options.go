package entities

// UpdateOptions holds runtime options passed to updaters.
type UpdateOptions struct {
	DryRun       bool
	Verbose      bool
	TargetBranch string
	AutoComplete bool
}
