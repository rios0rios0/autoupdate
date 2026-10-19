package pipeline

import (
	"context"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
)

// githubGitURLFormat is the git endpoint of a GitHub repository, filled with its owner
// and name. A variable only so a test can point it at local repositories.
var githubGitURLFormat = "https://github.com/%s/%s.git" //nolint:gochecknoglobals // test override

// actionTagLister lists the tags of the GitHub repository an action is published from.
type actionTagLister func(ctx context.Context, owner, repo string) ([]string, error)

// actionTagsFor returns where the tags of the GitHub Actions a workflow uses are read
// from.
//
// An action is published on GitHub whatever hosts the workflow that uses it, so only a
// GitHub provider can be asked for its tags. Any other provider looked the reference up
// as one of its own repositories -- Azure DevOps reads `actions/checkout` as project
// `actions`, repository `checkout`, and answers 401 -- so no action in a workflow kept
// anywhere else was ever updated. Those tags are read from GitHub's git endpoint
// instead, which needs no token and none of the REST API's rate limit for anonymous
// callers.
func actionTagsFor(provider repositories.ProviderRepository) actionTagLister {
	if provider != nil && provider.Name() == support.ProviderGitHub {
		return func(ctx context.Context, owner, repo string) ([]string, error) {
			return provider.GetTags(ctx, entities.Repository{Organization: owner, Name: repo})
		}
	}
	return listPublicGitHubTags
}

// listPublicGitHubTags lists the tags of a public GitHub repository.
func listPublicGitHubTags(ctx context.Context, owner, repo string) ([]string, error) {
	return listRemoteTags(ctx, fmt.Sprintf(githubGitURLFormat, owner, repo))
}

// listRemoteTags lists the names of the tags a git remote advertises, without cloning
// it. The peeled entries of annotated tags are left out: they name the same tags.
func listRemoteTags(ctx context.Context, url string) ([]string, error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := remote.ListContext(ctx, &git.ListOptions{PeelingOption: git.IgnorePeeled})
	if err != nil {
		return nil, fmt.Errorf("failed to list the tags of %s: %w", url, err)
	}

	tags := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Name().IsTag() {
			tags = append(tags, ref.Name().Short())
		}
	}
	return tags, nil
}
