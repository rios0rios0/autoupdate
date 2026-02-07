package terraform //nolint:testpackage // tests unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/domain"
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
		assert.Equal(t, "terraform-deps-upgrade/mod-net-v2.0.0", name)
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
		assert.Equal(t, "terraform-deps-upgrade/batch-3-modules", name)
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
		assert.Equal(t, "chore(deps): upgrade mod-net from v1.0.0 to v2.0.0", msg)
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
			"chore(deps): upgrade 2 Terraform module dependencies",
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
		assert.Equal(t, "chore(deps): Upgrade mod-storage to v3.1.0", title)
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
			"chore(deps): Upgrade 3 Terraform module dependencies",
			title,
		)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include markdown table with all upgrades", func(t *testing.T) {
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
		assert.Contains(t, desc, "| mod-a | v1.0.0 | v2.0.0 | /a.tf |")
		assert.Contains(t, desc, "| mod-b | v0.5.0 | v1.0.0 | /b.tf |")
		assert.Contains(t, desc, "autoupdate")
	})
}
