package terraform //nolint:testpackage // tests unexported functions

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/domain"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestTerraformUpdater_Name(t *testing.T) {
	t.Parallel()

	t.Run("should return terraform", func(t *testing.T) {
		t.Parallel()

		// given
		u := New()

		// when
		name := u.Name()

		// then
		assert.Equal(t, "terraform", name)
	})
}

func TestIsGitModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected bool
	}{
		{
			name:     "should detect git:: prefix",
			source:   "git::https://dev.azure.com/org/project/_git/repo?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should detect git@ SSH prefix",
			source:   "git@ssh.dev.azure.com:v3/org/project/repo",
			expected: true,
		},
		{
			name:     "should detect github.com URL",
			source:   "https://github.com/org/terraform-module-vpc?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should detect gitlab.com URL",
			source:   "https://gitlab.com/group/module?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should detect bitbucket.org URL",
			source:   "https://bitbucket.org/org/module?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should detect dev.azure.com URL",
			source:   "https://dev.azure.com/org/project/_git/module?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should detect _git path segment",
			source:   "https://my-server.com/org/project/_git/module?ref=v1.0.0",
			expected: true,
		},
		{
			name:     "should reject Terraform registry source",
			source:   "hashicorp/consul/aws",
			expected: false,
		},
		{
			name:     "should reject local path source",
			source:   "../modules/networking",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			source := tt.source

			// when
			result := isGitModule(source)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "should extract version from ?ref= parameter",
			source:   "git::https://dev.azure.com/org/project/_git/repo?ref=v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "should extract version without v prefix",
			source:   "git::https://dev.azure.com/org/project/_git/repo?ref=1.0.0",
			expected: "1.0.0",
		},
		{
			name:     "should extract version with additional params",
			source:   "git::https://example.com/repo?ref=v2.0.0&depth=1",
			expected: "v2.0.0",
		},
		{
			name:     "should return empty for no ref parameter",
			source:   "git::https://dev.azure.com/org/project/_git/repo",
			expected: "",
		},
		{
			name:     "should return empty for local path",
			source:   "../modules/networking",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			source := tt.source

			// when
			result := extractVersion(source)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveVersionFromSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "should remove ?ref= from source",
			source:   "git::https://dev.azure.com/org/project/_git/repo?ref=v1.0.0",
			expected: "git::https://dev.azure.com/org/project/_git/repo",
		},
		{
			name:     "should return unchanged when no ref",
			source:   "git::https://dev.azure.com/org/project/_git/repo",
			expected: "git::https://dev.azure.com/org/project/_git/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			source := tt.source

			// when
			result := removeVersionFromSource(source)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "should extract last path segment from URL",
			source:   "git::https://dev.azure.com/org/project/_git/terraform-module-networking",
			expected: "terraform-module-networking",
		},
		{
			name:     "should handle simple name",
			source:   "my-module",
			expected: "my-module",
		},
		{
			name:     "should handle path with single segment",
			source:   "repo",
			expected: "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			source := tt.source

			// when
			result := extractRepoName(source)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    string
		newVersion string
		expected   bool
	}{
		{
			name:       "should detect newer patch version",
			current:    "v1.0.0",
			newVersion: "v1.0.1",
			expected:   true,
		},
		{
			name:       "should detect newer minor version",
			current:    "v1.0.0",
			newVersion: "v1.1.0",
			expected:   true,
		},
		{
			name:       "should detect newer major version",
			current:    "v1.0.0",
			newVersion: "v2.0.0",
			expected:   true,
		},
		{
			name:       "should reject same version",
			current:    "v1.0.0",
			newVersion: "v1.0.0",
			expected:   false,
		},
		{
			name:       "should reject older version",
			current:    "v2.0.0",
			newVersion: "v1.0.0",
			expected:   false,
		},
		{
			name:       "should handle versions without v prefix",
			current:    "1.0.0",
			newVersion: "1.1.0",
			expected:   true,
		},
		{
			name:       "should handle mixed v prefix",
			current:    "v1.0.0",
			newVersion: "1.1.0",
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			current := tt.current
			newVer := tt.newVersion

			// when
			result := isNewerVersion(current, newVer)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanTerraformFile(t *testing.T) {
	t.Parallel()

	t.Run("should parse HCL module block with git source", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
module "networking" {
  source = "git::https://dev.azure.com/org/project/_git/terraform-module-networking?ref=v1.2.3"

  variable1 = "value1"
}
`
		// when
		deps := scanTerraformFile(content, "main.tf")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "networking", deps[0].Name)
		assert.Equal(t, "v1.2.3", deps[0].CurrentVer)
		assert.Contains(t, deps[0].Source, "terraform-module-networking")
		assert.Equal(t, "main.tf", deps[0].FilePath)
	})

	t.Run("should parse multiple module blocks", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
module "networking" {
  source = "git::https://dev.azure.com/org/proj/_git/mod-net?ref=v1.0.0"
}

module "storage" {
  source = "git::https://dev.azure.com/org/proj/_git/mod-storage?ref=v2.1.0"
}
`
		// when
		deps := scanTerraformFile(content, "infra.tf")

		// then
		require.Len(t, deps, 2)
		assert.Equal(t, "networking", deps[0].Name)
		assert.Equal(t, "v1.0.0", deps[0].CurrentVer)
		assert.Equal(t, "storage", deps[1].Name)
		assert.Equal(t, "v2.1.0", deps[1].CurrentVer)
	})

	t.Run("should skip non-git module sources", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
module "consul" {
  source = "hashicorp/consul/aws"
  version = "0.1.0"
}
`
		// when
		deps := scanTerraformFile(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should skip modules without ref parameter", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
module "unversioned" {
  source = "git::https://dev.azure.com/org/proj/_git/mod-no-ref"
}
`
		// when
		deps := scanTerraformFile(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should return empty for file with no modules", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
resource "aws_instance" "example" {
  ami           = "ami-12345"
  instance_type = "t2.micro"
}
`
		// when
		deps := scanTerraformFile(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should parse GitHub module source", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
module "vpc" {
  source = "github.com/org/terraform-vpc?ref=v3.0.0"
}
`
		// when
		deps := scanTerraformFile(content, "vpc.tf")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "vpc", deps[0].Name)
		assert.Equal(t, "v3.0.0", deps[0].CurrentVer)
	})
}

func TestApplyVersionUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should replace version in source using string match", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "net" {
  source = "git::https://dev.azure.com/org/proj/_git/mod-net?ref=v1.0.0"
}`
		dep := domain.Dependency{
			Name:       "net",
			Source:     "git::https://dev.azure.com/org/proj/_git/mod-net",
			CurrentVer: "v1.0.0",
		}

		// when
		result := applyVersionUpgrade(content, dep, "v2.0.0")

		// then
		assert.Contains(t, result, "ref=v2.0.0")
		assert.NotContains(t, result, "ref=v1.0.0")
	})

	t.Run("should handle multiple modules and only replace the correct one", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "a" {
  source = "git::https://example.com/mod-a?ref=v1.0.0"
}

module "b" {
  source = "git::https://example.com/mod-b?ref=v1.0.0"
}`
		dep := domain.Dependency{
			Name:       "a",
			Source:     "git::https://example.com/mod-a",
			CurrentVer: "v1.0.0",
		}

		// when
		result := applyVersionUpgrade(content, dep, "v2.0.0")

		// then
		assert.Contains(t, result, "mod-a?ref=v2.0.0")
		assert.Contains(t, result, "mod-b?ref=v1.0.0") // b should remain unchanged
	})
}

func TestBuildSourceWithVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		version  string
		expected string
	}{
		{
			name:     "should add ref parameter to clean source",
			source:   "git::https://example.com/repo",
			version:  "v1.0.0",
			expected: "git::https://example.com/repo?ref=v1.0.0",
		},
		{
			name:     "should replace existing ref parameter",
			source:   "git::https://example.com/repo?ref=v0.9.0",
			version:  "v1.0.0",
			expected: "git::https://example.com/repo?ref=v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			source := tt.source
			version := tt.version

			// when
			result := buildSourceWithVersion(source, version)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateBranchName(t *testing.T) {
	t.Parallel()

	t.Run("should generate single-module branch name", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-net"},
				newVersion: "v2.0.0",
			},
		}

		// when
		name := generateBranchName(tasks)

		// then
		assert.Equal(t, "chore/upgrade-mod-net-v2.0.0", name)
	})

	t.Run("should generate batch branch name for multiple modules", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{dep: domain.Dependency{Source: "a"}, newVersion: "v1"},
			{dep: domain.Dependency{Source: "b"}, newVersion: "v2"},
			{dep: domain.Dependency{Source: "c"}, newVersion: "v3"},
		}

		// when
		name := generateBranchName(tasks)

		// then
		assert.Equal(t, "chore/upgrade-3-dependencies", name)
	})
}

func TestGenerateCommitMessage(t *testing.T) {
	t.Parallel()

	t.Run("should generate single-module commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{
				dep: domain.Dependency{
					Source:     "git::https://example.com/mod-net",
					CurrentVer: "v1.0.0",
				},
				newVersion: "v2.0.0",
			},
		}

		// when
		msg := generateCommitMessage(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded `mod-net` from `v1.0.0` to `v2.0.0`", msg)
	})

	t.Run("should generate batch commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{dep: domain.Dependency{Source: "a"}, newVersion: "v1"},
			{dep: domain.Dependency{Source: "b"}, newVersion: "v2"},
		}

		// when
		msg := generateCommitMessage(tasks)

		// then
		assert.Equal(
			t,
			"chore(deps): upgraded 2 Terraform dependencies",
			msg,
		)
	})
}

func TestGeneratePRTitle(t *testing.T) {
	t.Parallel()

	t.Run("should generate single-module PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-storage"},
				newVersion: "v3.1.0",
			},
		}

		// when
		title := generatePRTitle(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded `mod-storage` to `v3.1.0`", title)
	})

	t.Run("should generate batch PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{dep: domain.Dependency{Source: "a"}, newVersion: "v1"},
			{dep: domain.Dependency{Source: "b"}, newVersion: "v2"},
			{dep: domain.Dependency{Source: "c"}, newVersion: "v3"},
		}

		// when
		title := generatePRTitle(tasks)

		// then
		assert.Equal(
			t,
			"chore(deps): upgraded 3 Terraform dependencies",
			title,
		)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include markdown table with all upgrades when at threshold", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{
				dep: domain.Dependency{
					Source:     "git::https://example.com/mod-a",
					CurrentVer: "v1.0.0",
					FilePath:   "/a.tf",
				},
				newVersion: "v2.0.0",
			},
			{
				dep: domain.Dependency{
					Source:     "git::https://example.com/mod-b",
					CurrentVer: "v0.5.0",
					FilePath:   "/b.tf",
				},
				newVersion: "v1.0.0",
			},
		}

		// when
		desc := generatePRDescription(tasks)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "| mod-a | module | v1.0.0 | v2.0.0 | /a.tf |")
		assert.Contains(t, desc, "| mod-b | module | v0.5.0 | v1.0.0 | /b.tf |")
		assert.Contains(t, desc, "autoupdate")
	})

	t.Run("should summarize when more than 5 upgrades", func(t *testing.T) {
		t.Parallel()

		// given — 7 tasks: 4 modules + 3 images
		tasks := []upgradeTask{
			{dep: domain.Dependency{Source: "mod-a", CurrentVer: "v1.0.0"}, newVersion: "v2.0.0", kind: depKindModule},
			{dep: domain.Dependency{Source: "mod-b", CurrentVer: "v1.0.0"}, newVersion: "v2.0.0", kind: depKindModule},
			{dep: domain.Dependency{Source: "mod-c", CurrentVer: "v1.0.0"}, newVersion: "v2.0.0", kind: depKindModule},
			{dep: domain.Dependency{Source: "mod-d", CurrentVer: "v1.0.0"}, newVersion: "v2.0.0", kind: depKindModule},
			{dep: domain.Dependency{Source: "img-a", CurrentVer: "0.1.0"}, newVersion: "0.2.0", kind: depKindImage},
			{dep: domain.Dependency{Source: "img-b", CurrentVer: "0.1.0"}, newVersion: "0.2.0", kind: depKindImage},
			{dep: domain.Dependency{Source: "img-c", CurrentVer: "0.1.0"}, newVersion: "0.2.0", kind: depKindImage},
		}

		// when
		desc := generatePRDescription(tasks)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "**7** Terraform dependencies")
		assert.Contains(t, desc, "**4** module upgrades")
		assert.Contains(t, desc, "**3** container image upgrades")
		assert.NotContains(t, desc, "| Name |")
		assert.Contains(t, desc, "autoupdate")
	})

	t.Run("should summarize with only modules when no images present", func(t *testing.T) {
		t.Parallel()

		// given — 6 module-only tasks
		tasks := make([]upgradeTask, 6)
		for i := range tasks {
			tasks[i] = upgradeTask{
				dep:        domain.Dependency{Source: fmt.Sprintf("mod-%d", i), CurrentVer: "v1.0.0"},
				newVersion: "v2.0.0",
				kind:       depKindModule,
			}
		}

		// when
		desc := generatePRDescription(tasks)

		// then
		assert.Contains(t, desc, "**6** Terraform dependencies")
		assert.Contains(t, desc, "**6** module upgrades")
		assert.NotContains(t, desc, "container image")
	})
}

func TestAppendChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should append changelog entry when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		upgrades := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-net", CurrentVer: "v1.0.0"},
				newVersion: "v2.0.0",
			},
		}
		existing := []domain.FileChange{{Path: "main.tf", Content: "...", ChangeType: "edit"}}

		// when
		result := appendChangelogEntry(ctx, provider, repo, upgrades, existing)

		// then
		require.Len(t, result, 2)
		assert.Equal(t, "main.tf", result[0].Path)
		assert.Equal(t, "CHANGELOG.md", result[1].Path)
		assert.Contains(t, result[1].Content, "### Changed")
		assert.Contains(t, result[1].Content, "- changed the Terraform module mod-net from v1.0.0 to v2.0.0")
	})

	t.Run("should return unchanged file changes when CHANGELOG.md is absent", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		upgrades := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-net", CurrentVer: "v1.0.0"},
				newVersion: "v2.0.0",
			},
		}
		existing := []domain.FileChange{{Path: "main.tf", Content: "...", ChangeType: "edit"}}

		// when
		result := appendChangelogEntry(ctx, provider, repo, upgrades, existing)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "main.tf", result[0].Path)
	})

	t.Run("should generate one entry per upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		upgrades := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-net", CurrentVer: "v1.0.0"},
				newVersion: "v2.0.0",
			},
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-compute", CurrentVer: "v3.0.0"},
				newVersion: "v4.0.0",
			},
		}

		// when
		result := appendChangelogEntry(ctx, provider, repo, upgrades, nil)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "CHANGELOG.md", result[0].Path)
		assert.Contains(t, result[0].Content, "- changed the Terraform module mod-net from v1.0.0 to v2.0.0")
		assert.Contains(t, result[0].Content, "- changed the Terraform module mod-compute from v3.0.0 to v4.0.0")
	})

	t.Run("should return unchanged when CHANGELOG.md has no Unreleased section", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		upgrades := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "git::https://example.com/mod-net", CurrentVer: "v1.0.0"},
				newVersion: "v2.0.0",
			},
		}
		existing := []domain.FileChange{{Path: "main.tf", Content: "...", ChangeType: "edit"}}

		// when
		result := appendChangelogEntry(ctx, provider, repo, upgrades, existing)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "main.tf", result[0].Path)
	})

	t.Run("should generate image-specific changelog entries for image deps", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		upgrades := []upgradeTask{
			{
				dep:        domain.Dependency{Source: "relayer-http", CurrentVer: "0.7.0"},
				newVersion: "0.8.0",
				kind:       depKindImage,
			},
		}

		// when
		result := appendChangelogEntry(ctx, provider, repo, upgrades, nil)

		// then
		require.Len(t, result, 1)
		assert.Contains(t, result[0].Content, "- changed the container image relayer-http from 0.7.0 to 0.8.0")
	})
}

func TestScanHCLFile(t *testing.T) {
	t.Parallel()

	t.Run("should parse container image references from Terragrunt HCL", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
inputs = {
  relayer_http_image = "relayer-http:0.7.0"
  tracker_http_image = "tracker-http:2.2.1"
}
`
		// when
		deps := scanHCLFile(content, "terragrunt.hcl")

		// then
		require.Len(t, deps, 2)
		assert.Equal(t, "relayer_http_image", deps[0].Name)
		assert.Equal(t, "relayer-http", deps[0].Source)
		assert.Equal(t, "0.7.0", deps[0].CurrentVer)
		assert.Equal(t, "terragrunt.hcl", deps[0].FilePath)
		assert.Equal(t, "tracker_http_image", deps[1].Name)
		assert.Equal(t, "tracker-http", deps[1].Source)
		assert.Equal(t, "2.2.1", deps[1].CurrentVer)
	})

	t.Run("should skip images with latest tag", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
inputs = {
  tracker_http_image = "tracker-http:latest"
  sight_http_image   = "sight-http:0.9.1"
}
`
		// when
		deps := scanHCLFile(content, "terragrunt.hcl")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "sight_http_image", deps[0].Name)
		assert.Equal(t, "sight-http", deps[0].Source)
		assert.Equal(t, "0.9.1", deps[0].CurrentVer)
	})

	t.Run("should handle multiple image references in real-world Terragrunt file", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  container_registry_url = dependency.shared_common.outputs.container_registry_server

  sight_http_image      = "sight-http:0.9.1"
  sight_detector_image  = "sight-detector:0.4.2"
  sight_validator_image = "sight-validator:0.7.1"
}
`
		// when
		deps := scanHCLFile(content, "environments/09_security_alerts/prod/terragrunt.hcl")

		// then
		require.Len(t, deps, 3)
		assert.Equal(t, "sight-http", deps[0].Source)
		assert.Equal(t, "0.9.1", deps[0].CurrentVer)
		assert.Equal(t, "sight-detector", deps[1].Source)
		assert.Equal(t, "0.4.2", deps[1].CurrentVer)
		assert.Equal(t, "sight-validator", deps[2].Source)
		assert.Equal(t, "0.7.1", deps[2].CurrentVer)
	})

	t.Run("should return empty for HCL file with no image references", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
terraform {
  source = "${get_path_to_repo_root()}//stacks/01_shared"
}

inputs = {
  environment = "dev"
  location    = "eastus"
}
`
		// when
		deps := scanHCLFile(content, "root.hcl")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should skip non-semver image tags", func(t *testing.T) {
		t.Parallel()

		// given
		content := `
inputs = {
  app_image = "my-app:dev-build-123"
}
`
		// when
		deps := scanHCLFile(content, "terragrunt.hcl")

		// then
		assert.Empty(t, deps)
	})
}

func TestIsSemverLike(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{name: "should accept standard semver", version: "1.2.3", expected: true},
		{name: "should accept semver with v prefix", version: "v1.2.3", expected: true},
		{name: "should accept major-only version", version: "1", expected: true},
		{name: "should reject latest", version: "latest", expected: false},
		{name: "should reject build tag", version: "dev-build-123", expected: false},
		{name: "should accept zero version", version: "0.0.1", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			version := tt.version

			// when
			result := isSemverLike(version)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyImageVersionUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should replace image version using string match", func(t *testing.T) {
		t.Parallel()

		// given
		content := `inputs = {
  relayer_http_image = "relayer-http:0.7.0"
}`
		dep := domain.Dependency{
			Name:       "relayer_http_image",
			Source:     "relayer-http",
			CurrentVer: "0.7.0",
		}

		// when
		result := applyImageVersionUpgrade(content, dep, "0.8.0")

		// then
		assert.Contains(t, result, "relayer-http:0.8.0")
		assert.NotContains(t, result, "relayer-http:0.7.0")
	})

	t.Run("should only replace the correct image when multiple exist", func(t *testing.T) {
		t.Parallel()

		// given
		content := `inputs = {
  sight_http_image      = "sight-http:0.9.1"
  sight_detector_image  = "sight-detector:0.4.2"
  sight_validator_image = "sight-validator:0.7.1"
}`
		dep := domain.Dependency{
			Name:       "sight_detector_image",
			Source:     "sight-detector",
			CurrentVer: "0.4.2",
		}

		// when
		result := applyImageVersionUpgrade(content, dep, "0.5.0")

		// then
		assert.Contains(t, result, "sight-detector:0.5.0")
		assert.NotContains(t, result, "sight-detector:0.4.2")
		assert.Contains(t, result, "sight-http:0.9.1")      // unchanged
		assert.Contains(t, result, "sight-validator:0.7.1") // unchanged
	})
}

func TestCountByKind(t *testing.T) {
	t.Parallel()

	t.Run("should return correct counts for mixed modules and images", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{kind: depKindModule},
			{kind: depKindModule},
			{kind: depKindImage},
			{kind: depKindModule},
			{kind: depKindImage},
		}

		// when
		moduleCount, imageCount := countByKind(tasks)

		// then
		assert.Equal(t, 3, moduleCount)
		assert.Equal(t, 2, imageCount)
	})

	t.Run("should return zero image count when all modules", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{kind: depKindModule},
			{kind: depKindModule},
		}

		// when
		moduleCount, imageCount := countByKind(tasks)

		// then
		assert.Equal(t, 2, moduleCount)
		assert.Equal(t, 0, imageCount)
	})

	t.Run("should return zero module count when all images", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{kind: depKindImage},
			{kind: depKindImage},
			{kind: depKindImage},
		}

		// when
		moduleCount, imageCount := countByKind(tasks)

		// then
		assert.Equal(t, 0, moduleCount)
		assert.Equal(t, 3, imageCount)
	})

	t.Run("should return zeros for empty slice", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{}

		// when
		moduleCount, imageCount := countByKind(tasks)

		// then
		assert.Equal(t, 0, moduleCount)
		assert.Equal(t, 0, imageCount)
	})
}

func TestGeneratePRDescriptionWithMixedDeps(t *testing.T) {
	t.Parallel()

	t.Run("should include Type column distinguishing modules and images", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []upgradeTask{
			{
				dep: domain.Dependency{
					Source:     "git::https://example.com/mod-net",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
				},
				newVersion: "v2.0.0",
				kind:       depKindModule,
			},
			{
				dep: domain.Dependency{
					Source:     "relayer-http",
					CurrentVer: "0.7.0",
					FilePath:   "environments/prod/terragrunt.hcl",
				},
				newVersion: "0.8.0",
				kind:       depKindImage,
			},
		}

		// when
		desc := generatePRDescription(tasks)

		// then
		assert.Contains(t, desc, "| Name | Type |")
		assert.Contains(t, desc, "| mod-net | module | v1.0.0 | v2.0.0 | main.tf |")
		assert.Contains(t, desc, "| relayer-http | image | 0.7.0 | 0.8.0 | environments/prod/terragrunt.hcl |")
	})
}
