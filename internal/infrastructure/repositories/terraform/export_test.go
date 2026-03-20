package terraform

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ExtractChangelogVersions is exported for testing.
func ExtractChangelogVersions(content string) map[string]bool {
	return extractChangelogVersions(content)
}

// FindLatestChangelogVersion is exported for testing.
func FindLatestChangelogVersion(
	ctx context.Context,
	provider repositories.ProviderRepository,
	depRepo *entities.Repository,
	tags []string,
) string {
	return findLatestChangelogVersion(ctx, provider, depRepo, tags)
}
