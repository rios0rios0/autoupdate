package commands

import "github.com/rios0rios0/autoupdate/internal/domain/repositories"

type SelfUpdateCommand struct {
	repository repositories.SelfUpdateRepository
}

func NewSelfUpdateCommand(repository repositories.SelfUpdateRepository) *SelfUpdateCommand {
	return &SelfUpdateCommand{repository: repository}
}

func (c *SelfUpdateCommand) Execute(dryRun, force bool) error {
	return c.repository.Execute(AutoupdateVersion, dryRun, force)
}
