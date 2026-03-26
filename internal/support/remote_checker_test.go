//go:build unit

package support_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	langGolang "github.com/rios0rios0/langforge/pkg/infrastructure/languages/golang"
)

func TestDetectRemote(t *testing.T) {
	t.Parallel()

	t.Run("should return true when detector finds matching files", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"go.mod": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		found, err := support.DetectRemote(t.Context(), &langGolang.Detector{}, provider, repo)

		// then
		assert.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("should return false when no matching files found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		found, err := support.DetectRemote(t.Context(), &langGolang.Detector{}, provider, repo)

		// then
		assert.NoError(t, err)
		assert.False(t, found)
	})
}
