package support_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// checkoutRepository is the repository the checkout in these tests belongs to.
func checkoutRepository() entities.Repository {
	return entities.Repository{Name: "infra-repo", Organization: "example-org"}
}

// writeCheckout lays out files (checkout-relative path to content) under a fresh root.
func writeCheckout(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o700))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	return root
}

func TestCheckoutProviderReadsTheCheckout(t *testing.T) {
	t.Parallel()

	t.Run("should read the checkout's own files from disk, not from the API", func(t *testing.T) {
		t.Parallel()

		// given -- an API whose copy of the file differs from the checkout's
		root := writeCheckout(t, map[string]string{"stacks/network/main.tf": "local copy"})
		api := repositorydoubles.SpyProviderWithFile("stacks/network/main.tf", "remote copy")
		provider := support.NewCheckoutProvider(root, checkoutRepository(), api, "")

		// when
		content, err := provider.GetFileContent(t.Context(), checkoutRepository(), "stacks/network/main.tf")
		rooted, rootedErr := provider.GetFileContent(t.Context(), checkoutRepository(), "/stacks/network/main.tf")

		// then
		require.NoError(t, err)
		require.NoError(t, rootedErr)
		assert.Equal(t, "local copy", content)
		assert.Equal(t, "local copy", rooted)
		assert.Empty(t, api.FetchedFilePaths)
	})

	t.Run("should report regular files only, the way the forges answer", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeCheckout(t, map[string]string{"Dockerfile": "FROM example-image:1.0.0\n", "docker/x": ""})
		provider := support.NewCheckoutProvider(root, checkoutRepository(), nil, "")

		// when
		file := provider.HasFile(t.Context(), checkoutRepository(), "Dockerfile")
		directory := provider.HasFile(t.Context(), checkoutRepository(), "docker")
		missing := provider.HasFile(t.Context(), checkoutRepository(), "go.mod")

		// then
		assert.True(t, file)
		assert.False(t, directory)
		assert.False(t, missing)
	})

	t.Run("should list the files below the root whose path ends with the pattern", func(t *testing.T) {
		t.Parallel()

		// given -- one module at the root, one nested, and copies in trees the walker skips
		root := writeCheckout(t, map[string]string{
			"main.tf":                          "",
			"modules/network/main.tf":          "",
			"modules/network/README.md":        "",
			".terraform/modules/cache/x.tf":    "",
			"node_modules/example/vendored.tf": "",
		})
		provider := support.NewCheckoutProvider(root, checkoutRepository(), nil, "")

		// when
		files, err := provider.ListFiles(t.Context(), checkoutRepository(), ".tf")

		// then
		require.NoError(t, err)
		paths := make([]string, 0, len(files))
		for _, file := range files {
			paths = append(paths, file.Path)
		}
		assert.ElementsMatch(t, []string{"main.tf", "modules/network/main.tf"}, paths)
	})

	t.Run("should match a pattern holding a separator against the whole path", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeCheckout(t, map[string]string{"tests/terratest/go.mod": "", "go.mod": ""})
		provider := support.NewCheckoutProvider(root, checkoutRepository(), nil, "")

		// when
		files, err := provider.ListFiles(t.Context(), checkoutRepository(), "terratest/go.mod")

		// then
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "tests/terratest/go.mod", files[0].Path)
	})

	t.Run("should refuse a path that leaves the checkout", func(t *testing.T) {
		t.Parallel()

		// given
		parent := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("outside"), 0o600))
		root := filepath.Join(parent, "checkout")
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(root, 0o700))
		require.NoError(t, os.Symlink(filepath.Join(parent, "secret.txt"), filepath.Join(root, "link.txt")))
		provider := support.NewCheckoutProvider(root, checkoutRepository(), nil, "")

		// when
		_, dotDotErr := provider.GetFileContent(t.Context(), checkoutRepository(), "../secret.txt")
		_, linkErr := provider.GetFileContent(t.Context(), checkoutRepository(), "link.txt")

		// then -- ".." is cleaned away at the root, and the link is refused by os.Root
		require.Error(t, dotDotErr)
		require.Error(t, linkErr)
	})
}

func TestCheckoutProviderDefersToTheAPI(t *testing.T) {
	t.Parallel()

	t.Run("should read any other repository through the API", func(t *testing.T) {
		t.Parallel()

		// given -- a module repository Terraform resolves its versions from
		root := writeCheckout(t, map[string]string{"CHANGELOG.md": "local"})
		api := repositorydoubles.SpyProviderWithFile("CHANGELOG.md", "## [1.2.0]")
		provider := support.NewCheckoutProvider(root, checkoutRepository(), api, "")
		moduleRepository := entities.Repository{Name: "network-module", Organization: "example-org"}

		// when
		content, err := provider.GetFileContent(t.Context(), moduleRepository, "CHANGELOG.md")

		// then
		require.NoError(t, err)
		assert.Equal(t, "## [1.2.0]", content)
		assert.Equal(t, []string{"CHANGELOG.md"}, api.FetchedFilePaths)
	})

	t.Run("should fail every API call when built without an API", func(t *testing.T) {
		t.Parallel()

		// given -- the provider detection runs on, before the run has authenticated
		provider := support.NewCheckoutProvider(t.TempDir(), checkoutRepository(), nil, "")
		other := entities.Repository{Name: "network-module", Organization: "example-org"}

		// when
		_, discoverErr := provider.DiscoverRepositories(t.Context(), "example-org")
		_, tagsErr := provider.GetTags(t.Context(), other)
		_, contentErr := provider.GetFileContent(t.Context(), other, "CHANGELOG.md")
		_, prErr := provider.CreatePullRequest(t.Context(), other, entities.PullRequestInput{})
		branchErr := provider.CreateBranchWithChanges(t.Context(), other, entities.BranchInput{})

		// then
		require.ErrorIs(t, discoverErr, support.ErrNoProviderAPI)
		require.ErrorIs(t, tagsErr, support.ErrNoProviderAPI)
		require.ErrorIs(t, contentErr, support.ErrNoProviderAPI)
		require.ErrorIs(t, prErr, support.ErrNoProviderAPI)
		require.ErrorIs(t, branchErr, support.ErrNoProviderAPI)
		assert.False(t, provider.HasFile(t.Context(), other, "CHANGELOG.md"))
		assert.Empty(t, provider.Name())
	})
}

func TestCheckoutProviderTokens(t *testing.T) {
	t.Parallel()

	t.Run("should answer git's token from AuthToken, never the API's", func(t *testing.T) {
		t.Parallel()

		// given -- an API token the gh or az CLI issued, and the token the operator gave git
		api := &repositorydoubles.SpyProviderRepository{ProviderName: "github", Token: "fixture-api-token-placeholder"}
		provider := support.NewCheckoutProvider(t.TempDir(), checkoutRepository(), api, "fixture-git-token-placeholder")

		// when
		authToken := provider.AuthToken()
		apiToken := provider.APIToken()

		// then
		assert.Equal(t, "fixture-git-token-placeholder", authToken)
		assert.Equal(t, "fixture-api-token-placeholder", apiToken)
		assert.Equal(t, "github", provider.Name())
	})

	t.Run("should answer no token for git when the operator gave git none", func(t *testing.T) {
		t.Parallel()

		// given -- an SSH origin and only a CLI sign-in
		api := &repositorydoubles.SpyProviderRepository{ProviderName: "github", Token: "fixture-api-token-placeholder"}
		provider := support.NewCheckoutProvider(t.TempDir(), checkoutRepository(), api, "")

		// when
		authToken := provider.AuthToken()

		// then
		assert.Empty(t, authToken)
	})

	t.Run("should name both tokens for redaction", func(t *testing.T) {
		t.Parallel()

		// given
		api := &repositorydoubles.SpyProviderRepository{ProviderName: "github", Token: "fixture-api-token-placeholder"}
		checkout := support.NewCheckoutProvider(t.TempDir(), checkoutRepository(), api, "fixture-git-token-placeholder")

		// when
		checkoutTokens := support.ProviderTokens(checkout)
		apiTokens := support.ProviderTokens(api)

		// then
		assert.Equal(t, []string{"fixture-git-token-placeholder", "fixture-api-token-placeholder"}, checkoutTokens)
		assert.Equal(t, []string{"fixture-api-token-placeholder"}, apiTokens)
	})
}
