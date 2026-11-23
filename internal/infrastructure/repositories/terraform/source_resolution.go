package terraform

import (
	"context"
	"slices"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

const (
	// helmChartFile marks a Helm chart's repository.
	helmChartFile = "Chart.yaml"
)

// imageBuildFiles mark a repository that builds a container image.
func imageBuildFiles() []string {
	return []string{"Dockerfile", "Containerfile"}
}

// sourceKey identifies a dependency source for one scan. An image and a module are resolved
// differently, so the same name is resolved once for each kind.
type sourceKey struct {
	source string
	kind   depKind
}

// sourceResolver finds the repository whose tags version a dependency. The organization is
// listed once per scan, on first use, rather than once per dependency.
type sourceResolver struct {
	provider repositories.ProviderRepository
	org      string
	listed   bool
	repos    []entities.Repository
}

// newSourceResolver returns a resolver over the organization repo belongs to.
func newSourceResolver(provider repositories.ProviderRepository, repo entities.Repository) *sourceResolver {
	return &sourceResolver{provider: provider, org: repo.Organization, listed: false, repos: nil}
}

// resolve returns the tags of the repository a source names, and that repository; nothing
// when no repository can be told to be it.
func (r *sourceResolver) resolve(ctx context.Context, key sourceKey) ([]string, *entities.Repository) {
	name := extractRepoName(key.source)
	if name == "" {
		return nil, nil
	}

	named := r.named(ctx, name)
	var depRepo *entities.Repository
	if key.kind == depKindImage {
		depRepo = imageSourceRepository(ctx, r.provider, name, named)
	} else if len(named) > 0 {
		depRepo = &named[0]
	}
	if depRepo == nil {
		return nil, nil
	}

	tags, err := r.provider.GetTags(ctx, *depRepo)
	if err != nil {
		return nil, nil
	}
	return tags, depRepo
}

// named returns the organization's repositories called name, in the order the provider
// lists them.
func (r *sourceResolver) named(ctx context.Context, name string) []entities.Repository {
	if !r.listed {
		r.listed = true
		repos, err := r.provider.DiscoverRepositories(ctx, r.org)
		if err != nil {
			logger.Warnf("[terraform] Failed to list the repositories of %s: %v", r.org, err)
		}
		r.repos = repos
	}

	var named []entities.Repository
	for _, repo := range r.repos {
		if repo.Name == name {
			named = append(named, repo)
		}
	}
	return named
}

// imageSourceRepository picks, among the repositories named like an image, the one that
// builds it.
//
// A Helm chart is usually named after the image it deploys, and its tags version the chart:
// reading them as the image's pinned a chart release as an image tag that was never built.
// So a repository holding a `Chart.yaml` at its root is never an image's source. When more
// than one repository is left, the one that builds an image -- a `Dockerfile` or
// `Containerfile` at its root -- is taken; when that does not settle it either, the pin is
// left alone rather than versioned by a guess.
func imageSourceRepository(
	ctx context.Context,
	provider repositories.ProviderRepository,
	image string,
	named []entities.Repository,
) *entities.Repository {
	candidates := slices.DeleteFunc(slices.Clone(named), func(repo entities.Repository) bool {
		return provider.HasFile(ctx, repo, helmChartFile)
	})

	switch len(candidates) {
	case 0:
		if len(named) > 0 {
			logger.Infof("[terraform] Every repository named %q is a Helm chart, whose tags do not version "+
				"the image; leaving its pins alone", image)
		}
		return nil
	case 1:
		return &candidates[0]
	}

	builders := slices.DeleteFunc(candidates, func(repo entities.Repository) bool {
		return !buildsImage(ctx, provider, repo)
	})
	if len(builders) == 1 {
		return &builders[0]
	}

	logger.Warnf("[terraform] %d repositories are named %q and no single one of them builds the image; "+
		"leaving its pins alone", len(named), image)
	return nil
}

// buildsImage reports whether repo holds an image build file at its root.
func buildsImage(ctx context.Context, provider repositories.ProviderRepository, repo entities.Repository) bool {
	for _, file := range imageBuildFiles() {
		if provider.HasFile(ctx, repo, file) {
			return true
		}
	}
	return false
}
