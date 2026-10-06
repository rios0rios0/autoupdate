package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
)

func TestDependencyChanges(t *testing.T) {
	t.Parallel()

	t.Run("should state runtimes and GitHub Actions under their own subjects", func(t *testing.T) {
		t.Parallel()

		// given
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("golang", "1.26.0", "1.27.1"),
			pipeline.NewUpgradeTask("action:actions/checkout", "v4", "v5"),
		}

		// when
		changes := pipeline.DependencyChanges(upgrades)

		// then
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectPipelineRuntime, Name: "golang", From: "1.26.0", To: "1.27.1"},
			{Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "v5"},
		}, changes)
	})
}
