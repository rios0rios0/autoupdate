package repositories

import (
	gitforgeEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
)

// ProviderRepository is an alias for gitforge's FileAccessProvider.
// It abstracts a Git hosting service (GitHub, GitLab, Azure DevOps, etc.)
// providing file access, repository discovery, and PR management.
type ProviderRepository = gitforgeEntities.FileAccessProvider
