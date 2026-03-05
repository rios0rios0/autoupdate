package entities

import (
	changelogEntities "github.com/rios0rios0/gitforge/pkg/changelog/domain/entities"
)

// InsertChangelogEntry delegates to gitforge's changelog module.
var InsertChangelogEntry = changelogEntities.InsertChangelogEntry
