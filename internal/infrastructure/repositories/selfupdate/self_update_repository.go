package selfupdate

import "github.com/rios0rios0/cliforge/selfupdate"

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) Execute(version string, dryRun, force bool) error {
	cmd := selfupdate.NewSelfUpdateCommand("rios0rios0", "autoupdate", "autoupdate", version)
	return cmd.Execute(dryRun, force)
}
