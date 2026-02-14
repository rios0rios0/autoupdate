//go:build integration || unit || test

package commanddoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
)

// StubLocalCommand is a stub implementation of commands.Local.
type StubLocalCommand struct {
	ExecuteCallCount int
	ExecuteErr       error
	LastOpts         commands.LocalOptions
}

var _ commands.Local = (*StubLocalCommand)(nil)

func (s *StubLocalCommand) Execute(
	_ context.Context,
	opts commands.LocalOptions,
) error {
	s.ExecuteCallCount++
	s.LastOpts = opts
	return s.ExecuteErr
}
