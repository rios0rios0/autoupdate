package support

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ErrNoProviderAPI reports a call that needs the provider API on a CheckoutProvider built
// without one, which is what detection runs on: it reads files, and nothing else.
var ErrNoProviderAPI = errors.New("no provider API to answer for this checkout")

// CheckoutProvider is the provider `autoupdate .` hands the updaters it runs on the
// operator's checkout, the way `autoupdate run` hands them its clone and the provider API.
//
// Two things set it apart from the API it wraps:
//
//   - The checkout's own files are read from disk. Local mode upgrades the checkout as it
//     is -- a branch the remote has never seen, commits that were never pushed -- so an
//     updater detecting its ecosystem, or reading a manifest, has to see that tree and not
//     the default branch on the remote. Every other repository (the module repositories
//     Terraform resolves tags from, for instance) is still read through the API.
//   - AuthToken answers git's token. An updater hands AuthToken to git -- the Go updater
//     writes it into its script's credential rewrites -- and in local mode git only ever
//     receives the token the operator gave git (`--token` or the forge's environment
//     variable), never one the gh or az CLI issued for the API. The API's token is still
//     what every API call carries, and APIToken names it, so redaction covers both.
//
// Built without an API, it answers the file reads alone, which is all detection needs, and
// every API call fails with ErrNoProviderAPI.
type CheckoutProvider struct {
	root     string
	repo     entities.Repository
	api      repositories.ProviderRepository
	gitToken string
}

var (
	_ repositories.ProviderRepository = (*CheckoutProvider)(nil)
	_ repositories.APITokenProvider   = (*CheckoutProvider)(nil)
)

// NewCheckoutProvider returns the provider for the checkout at root, which is repo's. api
// may be nil, for a provider that only reads the checkout; gitToken is the token git
// receives, and may be empty.
func NewCheckoutProvider(
	root string,
	repo entities.Repository,
	api repositories.ProviderRepository,
	gitToken string,
) *CheckoutProvider {
	return &CheckoutProvider{root: root, repo: repo, api: api, gitToken: gitToken}
}

// ProviderTokens returns every token the provider holds, which is what text it may have
// handled is redacted with: its AuthToken, and the token its API calls carry when that is
// a different one (repositories.APITokenProvider).
func ProviderTokens(provider repositories.ProviderRepository) []string {
	tokens := []string{provider.AuthToken()}
	if holder, ok := provider.(repositories.APITokenProvider); ok {
		tokens = append(tokens, holder.APIToken())
	}
	return tokens
}

// Name returns the wrapped provider's name, or the empty string without one.
func (p *CheckoutProvider) Name() string {
	if p.api == nil {
		return ""
	}
	return p.api.Name()
}

// MatchesURL defers to the wrapped provider.
func (p *CheckoutProvider) MatchesURL(url string) bool {
	return p.api != nil && p.api.MatchesURL(url)
}

// AuthToken returns git's token, never the API's: an updater hands this one to git.
func (p *CheckoutProvider) AuthToken() string {
	return p.gitToken
}

// APIToken returns the token the API calls carry, for redaction.
func (p *CheckoutProvider) APIToken() string {
	if p.api == nil {
		return ""
	}
	return p.api.AuthToken()
}

// CloneURL defers to the wrapped provider.
func (p *CheckoutProvider) CloneURL(repo entities.Repository) string {
	if p.api == nil {
		return ""
	}
	return p.api.CloneURL(repo)
}

// SSHCloneURL defers to the wrapped provider.
func (p *CheckoutProvider) SSHCloneURL(repo entities.Repository, sshAlias string) string {
	if p.api == nil {
		return ""
	}
	return p.api.SSHCloneURL(repo, sshAlias)
}

// DiscoverRepositories defers to the wrapped provider.
func (p *CheckoutProvider) DiscoverRepositories(ctx context.Context, org string) ([]entities.Repository, error) {
	if p.api == nil {
		return nil, ErrNoProviderAPI
	}
	return p.api.DiscoverRepositories(ctx, org)
}

// CreatePullRequest defers to the wrapped provider.
func (p *CheckoutProvider) CreatePullRequest(
	ctx context.Context, repo entities.Repository, input entities.PullRequestInput,
) (*entities.PullRequest, error) {
	if p.api == nil {
		return nil, ErrNoProviderAPI
	}
	return p.api.CreatePullRequest(ctx, repo, input)
}

// PullRequestExists defers to the wrapped provider.
func (p *CheckoutProvider) PullRequestExists(
	ctx context.Context, repo entities.Repository, sourceBranch string,
) (bool, error) {
	if p.api == nil {
		return false, ErrNoProviderAPI
	}
	return p.api.PullRequestExists(ctx, repo, sourceBranch)
}

// ClosePullRequest defers to the wrapped provider.
func (p *CheckoutProvider) ClosePullRequest(
	ctx context.Context, repo entities.Repository, sourceBranch string,
) (bool, error) {
	if p.api == nil {
		return false, ErrNoProviderAPI
	}
	return p.api.ClosePullRequest(ctx, repo, sourceBranch)
}

// GetTags defers to the wrapped provider. The checkout's tags are read there too: a tag
// is a release, and a release is what the remote publishes.
func (p *CheckoutProvider) GetTags(ctx context.Context, repo entities.Repository) ([]string, error) {
	if p.api == nil {
		return nil, ErrNoProviderAPI
	}
	return p.api.GetTags(ctx, repo)
}

// CreateBranchWithChanges defers to the wrapped provider.
func (p *CheckoutProvider) CreateBranchWithChanges(
	ctx context.Context, repo entities.Repository, input entities.BranchInput,
) error {
	if p.api == nil {
		return ErrNoProviderAPI
	}
	return p.api.CreateBranchWithChanges(ctx, repo, input)
}

// GetFileContent reads the checkout's own file from disk, and any other repository's
// through the wrapped provider.
func (p *CheckoutProvider) GetFileContent(
	ctx context.Context, repo entities.Repository, filePath string,
) (string, error) {
	if !p.ownsRepository(repo) {
		if p.api == nil {
			return "", ErrNoProviderAPI
		}
		return p.api.GetFileContent(ctx, repo, filePath)
	}

	root, err := os.OpenRoot(p.root)
	if err != nil {
		return "", fmt.Errorf("failed to open the checkout at %s: %w", p.root, err)
	}
	defer func() { _ = root.Close() }()

	data, err := root.ReadFile(checkoutRelativePath(filePath))
	if err != nil {
		return "", fmt.Errorf("failed to read %s from the checkout: %w", filePath, err)
	}
	return string(data), nil
}

// HasFile reports whether the checkout holds a regular file at filePath -- as the forges
// answer it, by whether the file's content can be read -- and asks the wrapped provider
// about any other repository.
func (p *CheckoutProvider) HasFile(ctx context.Context, repo entities.Repository, filePath string) bool {
	if !p.ownsRepository(repo) {
		return p.api != nil && p.api.HasFile(ctx, repo, filePath)
	}

	root, err := os.OpenRoot(p.root)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()

	info, err := root.Stat(checkoutRelativePath(filePath))
	return err == nil && info.Mode().IsRegular()
}

// ListFiles lists the checkout's files whose path ends with pattern, the way the forges'
// listings filter, walking the whole tree the shared walker enters; any other repository
// is listed by the wrapped provider. Paths are relative to the checkout, slash-separated.
func (p *CheckoutProvider) ListFiles(
	ctx context.Context, repo entities.Repository, pattern string,
) ([]entities.File, error) {
	if !p.ownsRepository(repo) {
		if p.api == nil {
			return nil, ErrNoProviderAPI
		}
		return p.api.ListFiles(ctx, repo, pattern)
	}

	// A pattern without a separator is a suffix of the file's name, which the walker can
	// test without building every path; one with a separator needs the path.
	matchName := func(string) bool { return true }
	if !strings.Contains(pattern, "/") {
		matchName = func(name string) bool { return strings.HasSuffix(name, pattern) }
	}

	paths, err := WalkFilesByPredicate(p.root, matchName)
	if err != nil {
		// An unreadable subtree ends the walk with what it found so far, and that is kept:
		// discarding it would hide files that are there and readable, which is how the
		// Go module discovery treats the same walk.
		logger.Warnf("Scan of %s ended early (%v); continuing with the %d file(s) found",
			p.root, err, len(paths))
	}

	files := make([]entities.File, 0, len(paths))
	for _, relPath := range paths {
		if strings.HasSuffix(relPath, pattern) {
			files = append(files, entities.File{Path: relPath})
		}
	}
	return files, nil
}

// ownsRepository reports whether repo is the checkout's own repository.
func (p *CheckoutProvider) ownsRepository(repo entities.Repository) bool {
	return entities.RepoKey(repo) == entities.RepoKey(p.repo)
}

// checkoutRelativePath turns a path an updater names -- repo-relative, or rooted the way
// Azure DevOps roots its item paths ("/go.mod") -- into one relative to the checkout.
// [os.Root] then refuses anything that would leave it.
func checkoutRelativePath(filePath string) string {
	return strings.TrimPrefix(path.Clean("/"+filePath), "/")
}
