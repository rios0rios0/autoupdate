package commands

import "github.com/rios0rios0/cliforge/selfupdate"

type SelfUpdateCommand struct{}

func NewSelfUpdateCommand() *SelfUpdateCommand {
	return &SelfUpdateCommand{}
}

func (c *SelfUpdateCommand) Execute(dryRun, force bool) error {
	cmd := selfupdate.NewSelfUpdateCommand("rios0rios0", "autoupdate", "autoupdate", AutoupdateVersion)
	return cmd.Execute(dryRun, force)
}
