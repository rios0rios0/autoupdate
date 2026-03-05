//go:build integration || unit || test

package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// SpyProviderRepositoryBuilder helps create test SpyProviderRepository instances with a fluent interface.
type SpyProviderRepositoryBuilder struct {
	*testkit.BaseBuilder
	providerName    string
	token           string
	repositories    []entities.Repository
	discoverErr     error
	fileContents    map[string]string
	fileContentErr  error
	files           []entities.File
	listFileErr     error
	tags            []string
	getTagsErr      error
	existingFiles   map[string]bool
	createBranchErr error
	createdPR       *entities.PullRequest
	createPRErr     error
	prExistsResult  bool
	prExistsErr     error
}

// NewSpyProviderRepositoryBuilder creates a new spy provider repository builder with sensible defaults.
func NewSpyProviderRepositoryBuilder() *SpyProviderRepositoryBuilder {
	return &SpyProviderRepositoryBuilder{
		BaseBuilder:  testkit.NewBaseBuilder(),
		providerName: "github",
		token:        "test-token",
	}
}

// WithProviderName sets the provider name.
func (b *SpyProviderRepositoryBuilder) WithProviderName(name string) *SpyProviderRepositoryBuilder {
	b.providerName = name
	return b
}

// WithToken sets the authentication token.
func (b *SpyProviderRepositoryBuilder) WithToken(token string) *SpyProviderRepositoryBuilder {
	b.token = token
	return b
}

// WithRepositories sets the repositories to return from DiscoverRepositories.
func (b *SpyProviderRepositoryBuilder) WithRepositories(repos []entities.Repository) *SpyProviderRepositoryBuilder {
	b.repositories = repos
	return b
}

// WithDiscoverErr sets the error to return from DiscoverRepositories.
func (b *SpyProviderRepositoryBuilder) WithDiscoverErr(err error) *SpyProviderRepositoryBuilder {
	b.discoverErr = err
	return b
}

// WithFileContents sets the file contents map.
func (b *SpyProviderRepositoryBuilder) WithFileContents(contents map[string]string) *SpyProviderRepositoryBuilder {
	b.fileContents = contents
	return b
}

// WithFileContentErr sets the error to return from GetFileContent.
func (b *SpyProviderRepositoryBuilder) WithFileContentErr(err error) *SpyProviderRepositoryBuilder {
	b.fileContentErr = err
	return b
}

// WithFiles sets the files to return from ListFiles.
func (b *SpyProviderRepositoryBuilder) WithFiles(files []entities.File) *SpyProviderRepositoryBuilder {
	b.files = files
	return b
}

// WithListFileErr sets the error to return from ListFiles.
func (b *SpyProviderRepositoryBuilder) WithListFileErr(err error) *SpyProviderRepositoryBuilder {
	b.listFileErr = err
	return b
}

// WithTags sets the tags to return from GetTags.
func (b *SpyProviderRepositoryBuilder) WithTags(tags []string) *SpyProviderRepositoryBuilder {
	b.tags = tags
	return b
}

// WithGetTagsErr sets the error to return from GetTags.
func (b *SpyProviderRepositoryBuilder) WithGetTagsErr(err error) *SpyProviderRepositoryBuilder {
	b.getTagsErr = err
	return b
}

// WithExistingFiles sets the existing files map for HasFile.
func (b *SpyProviderRepositoryBuilder) WithExistingFiles(files map[string]bool) *SpyProviderRepositoryBuilder {
	b.existingFiles = files
	return b
}

// WithCreateBranchErr sets the error to return from CreateBranchWithChanges.
func (b *SpyProviderRepositoryBuilder) WithCreateBranchErr(err error) *SpyProviderRepositoryBuilder {
	b.createBranchErr = err
	return b
}

// WithCreatedPR sets the pull request to return from CreatePullRequest.
func (b *SpyProviderRepositoryBuilder) WithCreatedPR(pr *entities.PullRequest) *SpyProviderRepositoryBuilder {
	b.createdPR = pr
	return b
}

// WithCreatePRErr sets the error to return from CreatePullRequest.
func (b *SpyProviderRepositoryBuilder) WithCreatePRErr(err error) *SpyProviderRepositoryBuilder {
	b.createPRErr = err
	return b
}

// WithPRExistsResult sets the result to return from PullRequestExists.
func (b *SpyProviderRepositoryBuilder) WithPRExistsResult(exists bool) *SpyProviderRepositoryBuilder {
	b.prExistsResult = exists
	return b
}

// WithPRExistsErr sets the error to return from PullRequestExists.
func (b *SpyProviderRepositoryBuilder) WithPRExistsErr(err error) *SpyProviderRepositoryBuilder {
	b.prExistsErr = err
	return b
}

// Build creates the spy (satisfies testkit.Builder interface).
func (b *SpyProviderRepositoryBuilder) Build() interface{} {
	return b.BuildSpy()
}

// BuildSpy creates the SpyProviderRepository with a concrete return type.
func (b *SpyProviderRepositoryBuilder) BuildSpy() *SpyProviderRepository {
	return &SpyProviderRepository{
		ProviderName:   b.providerName,
		Token:          b.token,
		Repositories:   b.repositories,
		DiscoverErr:    b.discoverErr,
		FileContents:   b.fileContents,
		FileContentErr: b.fileContentErr,
		Files:          b.files,
		ListFileErr:    b.listFileErr,
		Tags:           b.tags,
		GetTagsErr:     b.getTagsErr,
		ExistingFiles:  b.existingFiles,
		CreateBranchErr: b.createBranchErr,
		CreatedPR:      b.createdPR,
		CreatePRErr:    b.createPRErr,
		PRExistsResult: b.prExistsResult,
		PRExistsErr:    b.prExistsErr,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *SpyProviderRepositoryBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.providerName = "github"
	b.token = "test-token"
	b.repositories = nil
	b.discoverErr = nil
	b.fileContents = nil
	b.fileContentErr = nil
	b.files = nil
	b.listFileErr = nil
	b.tags = nil
	b.getTagsErr = nil
	b.existingFiles = nil
	b.createBranchErr = nil
	b.createdPR = nil
	b.createPRErr = nil
	b.prExistsResult = false
	b.prExistsErr = nil
	return b
}

// Clone creates a deep copy of the SpyProviderRepositoryBuilder.
func (b *SpyProviderRepositoryBuilder) Clone() testkit.Builder {
	clone := &SpyProviderRepositoryBuilder{
		BaseBuilder:     b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		providerName:    b.providerName,
		token:           b.token,
		discoverErr:     b.discoverErr,
		fileContentErr:  b.fileContentErr,
		listFileErr:     b.listFileErr,
		getTagsErr:      b.getTagsErr,
		createBranchErr: b.createBranchErr,
		createdPR:       b.createdPR,
		createPRErr:     b.createPRErr,
		prExistsResult:  b.prExistsResult,
		prExistsErr:     b.prExistsErr,
	}

	if b.repositories != nil {
		clone.repositories = make([]entities.Repository, len(b.repositories))
		copy(clone.repositories, b.repositories)
	}
	if b.fileContents != nil {
		clone.fileContents = make(map[string]string, len(b.fileContents))
		for k, v := range b.fileContents {
			clone.fileContents[k] = v
		}
	}
	if b.files != nil {
		clone.files = make([]entities.File, len(b.files))
		copy(clone.files, b.files)
	}
	if b.tags != nil {
		clone.tags = make([]string, len(b.tags))
		copy(clone.tags, b.tags)
	}
	if b.existingFiles != nil {
		clone.existingFiles = make(map[string]bool, len(b.existingFiles))
		for k, v := range b.existingFiles {
			clone.existingFiles[k] = v
		}
	}

	return clone
}
