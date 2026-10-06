package dockerfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
)

func TestDependencyChanges(t *testing.T) {
	t.Parallel()

	t.Run("should state every image upgrade as a Docker base image", func(t *testing.T) {
		t.Parallel()

		// given
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("python", "3.12-slim", "3.13-slim"),
			dockerfile.NewUpgradeTask("golang", "1.26-alpine", "1.27-alpine"),
		}

		// when
		changes := dockerfile.DependencyChanges(upgrades)

		// then
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectDockerBaseImage, Name: "python", From: "3.12-slim", To: "3.13-slim"},
			{Subject: entities.SubjectDockerBaseImage, Name: "golang", From: "1.26-alpine", To: "1.27-alpine"},
		}, changes)
	})
}
