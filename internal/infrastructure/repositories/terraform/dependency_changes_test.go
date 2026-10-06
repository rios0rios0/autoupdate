package terraform_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
)

func TestDependencyChanges(t *testing.T) {
	t.Parallel()

	t.Run("should state modules and container images under their own subjects", func(t *testing.T) {
		t.Parallel()

		// given
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{
				Source: "git::https://github.com/org/terraform-modules//vpc", CurrentVer: "1.0.0",
			}, "1.1.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{
				Source: "redis", CurrentVer: "7.2-alpine",
			}, "7.4-alpine", "", terraform.DepKindImage),
		}

		// when
		changes := terraform.DependencyChanges(upgrades)

		// then
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectTerraformModule, Name: "vpc", From: "1.0.0", To: "1.1.0"},
			{Subject: entities.SubjectContainerImage, Name: "redis", From: "7.2-alpine", To: "7.4-alpine"},
		}, changes)
	})
}
