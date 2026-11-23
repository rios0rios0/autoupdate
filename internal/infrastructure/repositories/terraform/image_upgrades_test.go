package terraform_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

const (
	leafFile     = "environments/platform/root.hcl"
	registryHost = "registry.example.com"
)

// consumerRepo is the repository whose image pins are being upgraded.
func consumerRepo() entities.Repository {
	return entities.Repository{Organization: "example", Project: "infra", Name: "live"}
}

// operatorRepo builds the image the operator pins name.
func operatorRepo() entities.Repository {
	return entities.Repository{Organization: "example", Project: "apps", Name: "queue-operator"}
}

// apiRepo builds the image the api pins name.
func apiRepo() entities.Repository {
	return entities.Repository{Organization: "example", Project: "apps", Name: "api"}
}

// imageDep is an image pin of leafFile.
func imageDep(image, version string) terraform.DepWithContent {
	return terraform.NewDepWithContent(
		entities.Dependency{
			Name:       "container_image",
			Source:     image,
			CurrentVer: version,
			FilePath:   leafFile,
		},
		`container_image = "`+image+`:`+version+`"`,
		terraform.DepKindImage,
	)
}

// registryRule covers leafFile with the given registry.
func registryRule(host string) entities.ImageRegistryRule {
	return entities.ImageRegistryRule{
		Registry:     host,
		Repositories: []string{"example/infra/live"},
		Paths:        []string{"environments/*/root.hcl"},
	}
}

// heldCandidates returns the tags each held upgrade left out, in order.
func heldCandidates(held []terraform.HeldUpgrade) []string {
	candidates := make([]string, 0, len(held))
	for _, h := range held {
		candidates = append(candidates, terraform.HeldCandidate(h))
	}
	return candidates
}

func TestImageCandidates(t *testing.T) {
	t.Parallel()

	t.Run("should order tags mixing three and four parts as numbers, not strings", func(t *testing.T) {
		t.Parallel()

		// given -- the order a provider sorts these into once one of them is not semver
		tags := []string{"0.26.0.5", "0.26.0.4", "0.0.4", "0.0.3", "0.0.10"}

		// when
		candidates := terraform.ImageCandidates("0.0.4", tags)

		// then
		assert.Equal(t, []string{"0.26.0.5", "0.26.0.4", "0.0.10"}, candidates)
	})

	t.Run("should never offer a pre-release, a suffixed tag or a word", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"1.3.0-rc.1", "1.2.1-alpine", "latest", "1.2.1"}

		// when
		candidates := terraform.ImageCandidates("1.2.0", tags)

		// then
		assert.Equal(t, []string{"1.2.1"}, candidates)
	})

	t.Run("should offer nothing for a pin that is not a plain version", func(t *testing.T) {
		t.Parallel()

		// when
		candidates := terraform.ImageCandidates("1.2.0-alpine", []string{"1.3.0"})

		// then
		assert.Empty(t, candidates)
	})
}

func TestSameVersionLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		current   string
		candidate string
		same      bool
	}{
		{"should keep a patch on the line", "0.0.4", "0.0.5", true},
		{"should keep a minor of a 0.x image on the line", "0.7.0", "0.8.0", true},
		{"should ignore a leading v", "v1.2.3", "1.3.0", true},
		{"should treat a change in the number of parts as another line", "0.0.4", "0.26.0.4", false},
		{"should treat a new major as another line", "1.9.0", "2.0.0", false},
		{"should treat a more precise tag as another line", "1.4", "1.4.1", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// when
			same := terraform.SameVersionLine(testCase.current, testCase.candidate)

			// then
			assert.Equal(t, testCase.same, same)
		})
	}
}

func TestPlanUpgradesForImages(t *testing.T) {
	t.Parallel()

	t.Run("should hold a jump to another version line and say so", func(t *testing.T) {
		t.Parallel()

		// given -- an image that switched to upstream versions with a build number appended
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{operatorRepo()}).
			WithTags([]string{"0.26.0.5", "0.26.0.4", "0.0.4", "0.0.3"}).
			BuildSpy()
		updater := terraform.NewUpdaterRepositoryForTest(&repositorydoubles.StubImageRegistryRepository{})

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("queue-operator", "0.0.4")},
			entities.UpdateOptions{AllowMajorUpdates: true})

		// then
		assert.Empty(t, upgrades)
		require.Len(t, held, 1)
		assert.Equal(t, "0.26.0.5", terraform.HeldCandidate(held[0]))
		assert.Contains(t, terraform.HeldDescription(held[0]), "another version line")
	})

	t.Run("should pass over a tag the registry lacks and report it", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{operatorRepo()}).
			WithTags([]string{"0.26.0.5", "0.26.0.4", "0.0.4"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			registryHost + "/queue-operator": {"0.0.4", "0.26.0.1", "0.26.0.4"},
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("queue-operator", "0.0.4")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)}})

		// then
		assert.Empty(t, upgrades)
		assert.Equal(t, []string{"0.26.0.5", "0.26.0.4"}, heldCandidates(held))
		assert.Contains(t, terraform.HeldDescription(held[0]), "does not hold it")
		assert.Contains(t, terraform.HeldDescription(held[1]), "another version line")
	})

	t.Run("should move to the newest tag the registry holds on the pin's line", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.3.0", "1.2.0", "1.1.0"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			registryHost + "/team/api": {"1.1.0", "1.2.0"},
		}}
		rule := registryRule(registryHost)
		rule.Namespace = "team"
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{rule}})

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "1.2.0", terraform.UpgradeTaskNewVersion(upgrades[0]))
		assert.Equal(t, []string{"1.3.0"}, heldCandidates(held))
	})

	t.Run("should leave the pin alone when the registry cannot be asked", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.2.0", "1.1.0"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Errs: map[string]error{
			registryHost + "/api": errors.New("fixture outage"),
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)}})

		// then -- an outage is not an answer, so nothing below it is pinned either
		assert.Empty(t, upgrades)
		require.Len(t, held, 1)
		assert.Contains(t, terraform.HeldDescription(held[0]), "could not be asked")
	})

	t.Run("should leave the pin alone when no registry client exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.2.0", "1.1.0"}).
			BuildSpy()
		updater := terraform.NewUpdaterRepositoryForTest(nil)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)}})

		// then
		assert.Empty(t, upgrades)
		assert.Len(t, held, 1)
	})

	t.Run("should require every registry the file pulls from to hold the tag", func(t *testing.T) {
		t.Parallel()

		// given -- one file pinning the images of two environments, each with its registry
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.2.0", "1.1.0"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			"dev.example.com/api":  {"1.1.0", "1.2.0"},
			"prod.example.com/api": {"1.1.0"},
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{
				registryRule("dev.example.com"), registryRule("prod.example.com"),
			}})

		// then
		assert.Empty(t, upgrades)
		require.Len(t, held, 1)
		assert.Contains(t, terraform.HeldDescription(held[0]), "prod.example.com")
	})

	t.Run("should not ask a registry about a file no rule covers", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.2.0", "1.1.0"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{}
		rule := registryRule(registryHost)
		rule.Paths = []string{"environments/prod/**"}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{rule}})

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "1.2.0", terraform.UpgradeTaskNewVersion(upgrades[0]))
		assert.Empty(t, held)
		assert.Empty(t, images.Lookups)
	})

	t.Run("should ask a registry once for an image pinned in several places", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo()}).
			WithTags([]string{"1.2.0", "1.1.0"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			registryHost + "/api": {"1.1.0", "1.2.0"},
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		upgrades, _ := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("api", "1.1.0"), imageDep("api", "1.0.0")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)}})

		// then
		assert.Len(t, upgrades, 2)
		assert.Equal(t, []string{registryHost + "/api"}, images.Lookups)
		assert.Len(t, provider.DiscoveredOrgs, 1, "the organization is listed once per scan")
	})
}

func TestPlanUpgradesResolvesTheImageRepository(t *testing.T) {
	t.Parallel()

	chart := entities.Repository{Organization: "example", Project: "charts", Name: "cache-operator"}
	image := entities.Repository{Organization: "example", Project: "apps", Name: "cache-operator"}
	fork := entities.Repository{Organization: "example", Project: "forks", Name: "cache-operator"}

	t.Run("should version an image by the repository that builds it, not a same-named chart", func(t *testing.T) {
		t.Parallel()

		// given -- the chart is listed first, as the provider happens to list it
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{chart, image}).
			WithTagsByRepository(map[string][]string{
				entities.RepoKey(chart): {"0.3.0", "0.2.16"},
				entities.RepoKey(image): {"0.0.4", "0.0.3"},
			}).
			WithExistingFilesByRepository(map[string]map[string]bool{
				entities.RepoKey(chart): {"Chart.yaml": true},
				entities.RepoKey(image): {"Dockerfile": true},
			}).
			BuildSpy()
		updater := terraform.NewUpdaterRepositoryForTest(nil)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("cache-operator", "0.0.3")},
			entities.UpdateOptions{AllowMajorUpdates: true})

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "0.0.4", terraform.UpgradeTaskNewVersion(upgrades[0]))
		assert.Empty(t, held)
	})

	t.Run("should leave a pin alone when only a chart is named like the image", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{chart}).
			WithTags([]string{"0.3.0", "0.2.16"}).
			WithExistingFilesByRepository(map[string]map[string]bool{
				entities.RepoKey(chart): {"Chart.yaml": true},
			}).
			BuildSpy()
		updater := terraform.NewUpdaterRepositoryForTest(nil)

		// when
		upgrades, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("cache-operator", "0.0.3")},
			entities.UpdateOptions{AllowMajorUpdates: true})

		// then
		assert.Empty(t, upgrades)
		assert.Empty(t, held)
	})

	t.Run("should leave a pin alone when several repositories build the image", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{image, fork}).
			WithTags([]string{"0.0.5", "0.0.4"}).
			WithExistingFilesByRepository(map[string]map[string]bool{
				entities.RepoKey(image): {"Dockerfile": true},
				entities.RepoKey(fork):  {"Containerfile": true},
			}).
			BuildSpy()
		updater := terraform.NewUpdaterRepositoryForTest(nil)

		// when
		upgrades, _ := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("cache-operator", "0.0.4")},
			entities.UpdateOptions{AllowMajorUpdates: true})

		// then -- a guess between them would version the pin by whichever is listed first
		assert.Empty(t, upgrades)
	})

	t.Run("should keep taking a module's first same-named repository", func(t *testing.T) {
		t.Parallel()

		// given -- modules name their repository by URL and are left exactly as they were
		module := entities.Repository{Organization: "example", Project: "modules", Name: "network"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{module}).
			WithTags([]string{"v2.0.0", "v1.0.0"}).
			WithExistingFilesByRepository(map[string]map[string]bool{
				entities.RepoKey(module): {"Chart.yaml": true},
			}).
			BuildSpy()
		dep := terraform.NewDepWithContent(entities.Dependency{
			Name:       "network",
			Source:     "git::https://example.com/modules/network",
			CurrentVer: "v1.0.0",
			FilePath:   "main.tf",
		}, "", terraform.DepKindModule)
		updater := terraform.NewUpdaterRepositoryForTest(nil)

		// when
		upgrades, _ := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{dep}, entities.UpdateOptions{AllowMajorUpdates: true})

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "v2.0.0", terraform.UpgradeTaskNewVersion(upgrades[0]))
	})
}

func TestGeneratePRDescriptionWithHeld(t *testing.T) {
	t.Parallel()

	t.Run("should list the newer tags it leaves out and why", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{operatorRepo()}).
			WithTags([]string{"0.26.0.5", "0.26.0.4", "0.0.4"}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			registryHost + "/queue-operator": {"0.0.4", "0.26.0.4"},
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)
		_, held := terraform.PlanUpgrades(t.Context(), updater, provider, consumerRepo(),
			[]terraform.DepWithContent{imageDep("queue-operator", "0.0.4")},
			entities.UpdateOptions{ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)}})

		// when
		description := terraform.GeneratePRDescriptionWithHeld(nil, held)

		// then
		assert.Contains(t, description, "### Not applied")
		assert.Contains(t, description,
			"| `queue-operator` | "+leafFile+" | `0.0.4` | `0.26.0.5` | `"+registryHost+"` does not hold it |")
		assert.Contains(t, description, "| `0.26.0.4` | it is on another version line than `0.0.4`")
	})

	t.Run("should add nothing when no newer tag was left out", func(t *testing.T) {
		t.Parallel()

		// when
		description := terraform.GeneratePRDescriptionWithHeld(nil, nil)

		// then
		assert.NotContains(t, description, "Not applied")
	})
}

func TestApplyUpdatesHoldsImagePins(t *testing.T) {
	t.Parallel()

	t.Run("should write the upgrade the registry allows and leave the held pin in place", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		leaf := filepath.Join(repoDir, filepath.FromSlash(leafFile))
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(leaf), 0o700))
		require.NoError(t, os.WriteFile(leaf, []byte(`locals {
  api_image       = "api:1.1.0"
  queue_operator_image = "queue-operator:0.0.4"
}
`), 0o600))

		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{apiRepo(), operatorRepo()}).
			WithTagsByRepository(map[string][]string{
				entities.RepoKey(apiRepo()):      {"1.3.0", "1.2.0", "1.1.0"},
				entities.RepoKey(operatorRepo()): {"0.26.0.5", "0.0.4"},
			}).
			BuildSpy()
		images := &repositorydoubles.StubImageRegistryRepository{Tags: map[string][]string{
			registryHost + "/api":            {"1.1.0", "1.2.0"},
			registryHost + "/queue-operator": {"0.0.4", "0.26.0.5"},
		}}
		updater := terraform.NewUpdaterRepositoryForTest(images)

		// when
		result, err := updater.ApplyUpdates(t.Context(), repoDir, provider, consumerRepo(), entities.UpdateOptions{
			ImageRegistries: []entities.ImageRegistryRule{registryRule(registryHost)},
		})

		// then
		require.NoError(t, err)
		written, readErr := os.ReadFile(leaf)
		require.NoError(t, readErr)
		assert.Contains(t, string(written), `api_image       = "api:1.2.0"`)
		assert.Contains(t, string(written), `queue_operator_image = "queue-operator:0.0.4"`)
		assert.Contains(t, result.PRDescription, "| `api` | "+leafFile+" | `1.1.0` | `1.3.0` |")
		assert.Contains(t, result.PRDescription, "| `queue-operator` | "+leafFile+" | `0.0.4` | `0.26.0.5` |")
	})
}
