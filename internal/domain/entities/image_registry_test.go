package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestImageRegistryRuleCovers(t *testing.T) {
	t.Parallel()

	infra := entities.Repository{Organization: "example", Project: "platform", Name: "live-infra"}

	cases := []struct {
		name     string
		rule     entities.ImageRegistryRule
		repo     entities.Repository
		filePath string
		covers   bool
	}{
		{
			name:     "should cover every file of every repository when the rule names neither",
			rule:     entities.ImageRegistryRule{Registry: "registry.example.com"},
			repo:     infra,
			filePath: "environments/prod/terragrunt.hcl",
			covers:   true,
		},
		{
			name: "should cover a file at any depth below a ** segment",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Paths: []string{"environments/**/prod/**"},
			},
			repo:     infra,
			filePath: "environments/02_kubernetes/prod/0001/terragrunt.hcl",
			covers:   true,
		},
		{
			name: "should let a ** segment match no directory at all",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Paths: []string{"environments/**/root.hcl"},
			},
			repo:     infra,
			filePath: "environments/root.hcl",
			covers:   true,
		},
		{
			name: "should not let * cross a directory",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Paths: []string{"environments/*.hcl"},
			},
			repo:     infra,
			filePath: "environments/prod/terragrunt.hcl",
			covers:   false,
		},
		{
			name: "should match a path the provider lists with a leading slash",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Paths: []string{"environments/*/root.hcl"},
			},
			repo:     infra,
			filePath: "/environments/02_kubernetes/root.hcl",
			covers:   true,
		},
		{
			name: "should match a path walked with backslashes",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Paths: []string{"environments/*/root.hcl"},
			},
			repo:     infra,
			filePath: `environments\02_kubernetes\root.hcl`,
			covers:   true,
		},
		{
			name: "should not cover a repository its repositories patterns leave out",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Repositories: []string{"example/platform/other-infra"},
			},
			repo:     infra,
			filePath: "environments/prod/terragrunt.hcl",
			covers:   false,
		},
		{
			name: "should cover a repository named by its right-anchored key",
			rule: entities.ImageRegistryRule{
				Registry: "registry.example.com", Repositories: []string{"platform/live-*"},
			},
			repo:     infra,
			filePath: "environments/prod/terragrunt.hcl",
			covers:   true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// when
			covers := testCase.rule.Covers(testCase.repo, testCase.filePath)

			// then
			assert.Equal(t, testCase.covers, covers)
		})
	}
}

func TestImageRegistriesFor(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "example", Name: "live-infra"}

	t.Run("should return every rule covering the file, in configuration order", func(t *testing.T) {
		t.Parallel()

		// given
		rules := []entities.ImageRegistryRule{
			{Registry: "prod.example.com", Paths: []string{"environments/root.hcl", "environments/prod/**"}},
			{Registry: "dev.example.com", Paths: []string{"environments/root.hcl", "environments/dev/**"}},
		}

		// when
		covering := entities.ImageRegistriesFor(rules, repo, "environments/root.hcl")

		// then
		require.Len(t, covering, 2)
		assert.Equal(t, "prod.example.com", covering[0].Registry)
		assert.Equal(t, "dev.example.com", covering[1].Registry)
	})

	t.Run("should name a registry and namespace once", func(t *testing.T) {
		t.Parallel()

		// given
		rules := []entities.ImageRegistryRule{
			{Registry: "registry.example.com", Paths: []string{"environments/**"}},
			{Registry: "Registry.Example.com", Paths: []string{"environments/prod/**"}},
		}

		// when
		covering := entities.ImageRegistriesFor(rules, repo, "environments/prod/terragrunt.hcl")

		// then
		assert.Len(t, covering, 1)
	})

	t.Run("should return nothing when no rule covers the file", func(t *testing.T) {
		t.Parallel()

		// given
		rules := []entities.ImageRegistryRule{
			{Registry: "registry.example.com", Paths: []string{"environments/prod/**"}},
		}

		// when
		covering := entities.ImageRegistriesFor(rules, repo, "modules/api/main.hcl")

		// then
		assert.Empty(t, covering)
	})
}

func TestImageRegistryRuleImageRepository(t *testing.T) {
	t.Parallel()

	t.Run("should look an image up under the rule's namespace", func(t *testing.T) {
		t.Parallel()

		// given
		rule := entities.ImageRegistryRule{Registry: "registry.example.com", Namespace: "/team/"}

		// when
		repository := rule.ImageRepository("api")

		// then
		assert.Equal(t, "team/api", repository)
	})

	t.Run("should look an image up at the registry root without a namespace", func(t *testing.T) {
		t.Parallel()

		// given
		rule := entities.ImageRegistryRule{Registry: "registry.example.com"}

		// when
		repository := rule.ImageRepository("api")

		// then
		assert.Equal(t, "api", repository)
	})
}

func TestValidateImageRegistries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		rule  entities.ImageRegistryRule
		valid bool
	}{
		{"should accept a host", entities.ImageRegistryRule{Registry: "registry.example.com"}, true},
		{"should accept a host with a port", entities.ImageRegistryRule{Registry: "localhost:5000"}, true},
		{
			"should accept a namespace and ** paths",
			entities.ImageRegistryRule{
				Registry: "registry.example.com", Namespace: "team/apps", Paths: []string{"envs/**/prod/*.hcl"},
			},
			true,
		},
		{"should refuse an empty registry", entities.ImageRegistryRule{}, false},
		{"should refuse a scheme", entities.ImageRegistryRule{Registry: "https://registry.example.com"}, false},
		{"should refuse a path", entities.ImageRegistryRule{Registry: "registry.example.com/team"}, false},
		{
			"should refuse a namespace a registry cannot name",
			entities.ImageRegistryRule{Registry: "registry.example.com", Namespace: "Team"},
			false,
		},
		{
			"should refuse a repositories pattern path.Match cannot read",
			entities.ImageRegistryRule{Registry: "registry.example.com", Repositories: []string{"org/["}},
			false,
		},
		{
			"should refuse a paths pattern path.Match cannot read",
			entities.ImageRegistryRule{Registry: "registry.example.com", Paths: []string{"environments/[/x"}},
			false,
		},
		{
			"should refuse an empty paths pattern",
			entities.ImageRegistryRule{Registry: "registry.example.com", Paths: []string{" "}},
			false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// when
			err := entities.ValidateImageRegistries([]entities.ImageRegistryRule{testCase.rule})

			// then
			if testCase.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, entities.ErrInvalidImageRegistry)
			assert.Contains(t, err.Error(), "image_registries[0]")
		})
	}
}

func TestImageRegistriesLayering(t *testing.T) {
	t.Parallel()

	t.Run("should read image_registries from the operator's configuration", func(t *testing.T) {
		t.Parallel()

		// given
		layer := operatorLayer(`
image_registries:
  - registry: 'registry.example.com'
    namespace: 'team'
    repositories: ['example/live-infra']
    paths: ['environments/prod/**']
`)

		// when
		result, err := entities.ApplyLayer(&entities.Settings{}, layer)

		// then
		require.NoError(t, err)
		require.Len(t, result.ImageRegistries, 1)
		assert.Equal(t, entities.ImageRegistryRule{
			Registry:     "registry.example.com",
			Namespace:    "team",
			Repositories: []string{"example/live-infra"},
			Paths:        []string{"environments/prod/**"},
		}, result.ImageRegistries[0])
	})

	t.Run("should ignore image_registries from a repository's configuration", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{
			ImageRegistries: []entities.ImageRegistryRule{{Registry: "registry.example.com"}},
		}
		layer := restrictedLayer(`
image_registries:
  - registry: 'elsewhere.example.net'
`)

		// when
		result, err := entities.ApplyLayer(base, layer)

		// then -- the check sends the operator's registry credentials, so a repository must
		// not be able to name where to, nor to switch it off for itself
		require.NoError(t, err)
		assert.Equal(t, base.ImageRegistries, result.ImageRegistries)
	})

	t.Run("should refuse settings whose rule cannot be used", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			ImageRegistries: []entities.ImageRegistryRule{{Registry: "https://registry.example.com"}},
		}

		// when
		err := entities.ValidateSettings(settings, false)

		// then
		require.ErrorIs(t, err, entities.ErrInvalidImageRegistry)
	})

	t.Run("should answer no rules for no settings", func(t *testing.T) {
		t.Parallel()

		// when
		rules := entities.ConfiguredImageRegistries(nil)

		// then
		assert.Nil(t, rules)
	})
}
