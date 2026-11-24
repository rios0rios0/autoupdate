package repositories

import "context"

// ImageRegistryRepository reads what a container registry holds.
type ImageRegistryRepository interface {
	// ListTags returns every tag registry holds for repository: `team/api`, or `api` for an
	// image kept at the registry's root. A registry with no repository by that name answers
	// an empty list and no error.
	//
	// An error means the registry could not be asked -- unreachable, refusing the
	// credentials, answering something that is not a tag list -- and a caller must never
	// read it as "the tag is not there": that would turn an outage into a decision.
	ListTags(ctx context.Context, registry, repository string) ([]string, error)
}
