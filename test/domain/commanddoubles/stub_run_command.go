//go:build integration || unit || test

package commanddoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// StubRunCommand is a stub implementation of commands.Run.
type StubRunCommand struct {
	ExecuteCallCount int
	ExecuteErr       error
	LastSettings     *entities.Settings
	LastOpts         commands.RunOptions
}

var _ commands.Run = (*StubRunCommand)(nil)

func (s *StubRunCommand) Execute(
	_ context.Context,
	settings *entities.Settings,
	opts commands.RunOptions,
) error {
	s.ExecuteCallCount++
	s.LastSettings = settings
	s.LastOpts = opts
	return s.ExecuteErr
}
