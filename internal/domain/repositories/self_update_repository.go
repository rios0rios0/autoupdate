package repositories

// SelfUpdateRepository abstracts the mechanism for updating the autoupdate binary itself.
type SelfUpdateRepository interface {
	Execute(version string, dryRun, force bool) error
}
